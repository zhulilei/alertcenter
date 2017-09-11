package alertcenter

import (
	"fmt"
	"testing"
	"time"

	"github.com/qiniu/log.v1"
	"github.com/qiniu/xlog.v1"
	"github.com/stretchr/testify/assert"
	"labix.org/v2/mgo/bson"
	pmgo "pili.qiniu.com/mgo"
)

type FakeDutyMgr struct {
}

func (f *FakeDutyMgr) GetCurrent(xl *xlog.Logger) ([]Staff, error) {
	return []Staff{{Name: "1", Phones: []string{"18650317419"}}}, nil
}
func (f *FakeDutyMgr) CreateStaff(arg *Staff) error                              { return nil }
func (f *FakeDutyMgr) UpdateStaff(id bson.ObjectId, arg *UpdateStaffArg) error   { return nil }
func (f *FakeDutyMgr) RemoveStaff(id bson.ObjectId) error                        { return nil }
func (f *FakeDutyMgr) GetStaff(id bson.ObjectId) (ret Staff, err error)          { return }
func (f *FakeDutyMgr) ListStaffs([]bson.ObjectId) (ret []Staff, err error)       { return }
func (f *FakeDutyMgr) CreateRoster(arg *Roster) error                            { return nil }
func (f *FakeDutyMgr) UpdateRoster(id bson.ObjectId, arg *UpdateRosterArg) error { return nil }
func (f *FakeDutyMgr) RemoveRoster(id bson.ObjectId) error                       { return nil }
func (f *FakeDutyMgr) GetRoster(id bson.ObjectId) (ret Roster, err error)        { return }
func (f *FakeDutyMgr) ListRosters() (ret []Roster, err error)                    { return }

func Init(ast *assert.Assertions) (dutymgr *DutyMgr) {
	cfg := DutyCfg{
		StaffMgoOpt: pmgo.Option{
			MgoAddr:     "127.0.0.1",
			MgoDB:       "test",
			MgoColl:     "staff_test",
			MgoMode:     "strong",
			MgoPoolSize: 1,
		},
		RosterMgoOpt: pmgo.Option{
			MgoAddr:     "127.0.0.1",
			MgoDB:       "test",
			MgoColl:     "roster_test",
			MgoMode:     "strong",
			MgoPoolSize: 1,
		},
	}
	dutymgr, err := NewDutyMgr(cfg)
	ast.NoError(err)

	// =================================
	// Create Roster

	staffsM := make(map[int]bson.ObjectId)
	for i := 1; i < 5; i++ {
		v := fmt.Sprintf("%d", i)
		s := &Staff{Name: v, Phones: []string{v}}
		err := dutymgr.CreateStaff(s)
		ast.NoError(err)
	}
	allStaffs, err := dutymgr.ListStaffs(nil)
	ast.NoError(err)
	for i := range allStaffs {
		staffsM[i+1] = allStaffs[i].Id
	}

	// =================================
	// Create Roster1
	// now() is not within Roster1

	r := &Roster{
		Name:     "test",
		Begin:    time.Now().Add(-time.Hour * 24 * 15),
		End:      time.Now().Add(-time.Hour * 24 * 10),
		Unit:     UnitDay,
		StartIdx: 1,
		Priority: 1,
		Staffs: [][]bson.ObjectId{
			{
				staffsM[1],
			},
			{
				staffsM[2],
			},
			{
				staffsM[3],
			},
			{
				staffsM[4],
			},
		},
	}
	err = dutymgr.CreateRoster(r)
	if !ast.NoError(err) {
		return
	}

	// Create Roster2
	// now() is not within Roster2
	r.Id = ""
	r.Begin = time.Now().Add(-time.Hour * 24 * 11)
	r.End = time.Unix(1<<50, 0)
	err = dutymgr.CreateRoster(r)
	if !ast.NoError(err) {
		return
	}

	return
}

func TestGetStaffs(t *testing.T) {
	log.Println("TestGetStaffs Begin")
	defer log.Println("TestGetStaffs End")
	xl := xlog.NewDummy()
	ast := assert.New(t)
	dutymgr := Init(ast)
	defer func() {
		dutymgr.staffMgo.Coll().DropCollection()
		dutymgr.rosterMgo.Coll().DropCollection()
	}()

	// =========================
	// first case
	staffs, err := dutymgr.GetCurrent(xl)
	if !ast.NoError(err) {
		return
	}
	if !ast.Equal(1, len(staffs)) {
		return
	}
	ast.Equal("4", staffs[0].Name)

	// =========================
	// Get Roster2
	rosters, err := dutymgr.ListRosters()
	if !ast.NoError(err) {
		return
	}
	if !ast.Equal(2, len(rosters)) {
		return
	}
	if !ast.Equal("4", staffs[0].Name) {
		return
	}

	// =========================
	// modify Roster2 to test cases
	roster := rosters[1]
	updateArg := &UpdateRosterArg{
		Name:     roster.Name,
		Priority: roster.Priority,
		Staffs:   roster.Staffs,
	}

	cases := []struct {
		inStartIdx int
		inUnit     Unit
		InBegin    time.Time
		inEnd      time.Time
		wantLen    int
		wantName   string
	}{
		{2, UnitDay, time.Now().Add(-11 * day), time.Now().Add(11 * day), 1, "1"},
		{3, UnitDay, time.Now().Add(-11 * day), time.Now().Add(11 * day), 1, "2"},
		{4, UnitDay, time.Now().Add(-11 * day), time.Now().Add(11 * day), 1, "3"},
		{1, UnitWeek, time.Now().Add(-1 * week), time.Now().Add(10 * week), 1, "2"},
		{2, UnitWeek, time.Now().Add(-2 * week), time.Now().Add(10 * week), 1, "4"},
	}
	for _, c := range cases {
		updateArg.StartIdx = c.inStartIdx
		updateArg.Unit = c.inUnit
		updateArg.Begin = c.InBegin
		updateArg.End = c.inEnd
		dutymgr.UpdateRoster(roster.Id, updateArg)
		staffs, err = dutymgr.GetCurrent(xl)
		if !ast.NoError(err) {
			return
		}
		if !ast.Equal(c.wantLen, len(staffs)) {
			return
		}
		if !ast.Equal(c.wantName, staffs[0].Name) {
			return
		}
	}
}

package alertcenter

import (
	"errors"
	"net/http"
	"time"

	"github.com/qiniu/http/httputil.v1"
	"github.com/qiniu/log.v1"
	"github.com/qiniu/xlog.v1"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
	pmgo "pili.qiniu.com/mgo"
)

const (
	day  = time.Hour * 24
	week = time.Hour * 24 * 7
)

var (
	ErrDuplicatedStaff = httputil.NewError(http.StatusConflict, "duplicated staff")
	ErrStaffNotFound   = httputil.NewError(http.StatusNotFound, "staff not found")

	ErrDuplicatedRoster = httputil.NewError(http.StatusConflict, "duplicated roster")
	ErrRosterNotFound   = httputil.NewError(http.StatusNotFound, "roster not found")
)

type DutyCfg struct {
	StaffMgoOpt  pmgo.Option `json:"staff_mgo_opt"`
	RosterMgoOpt pmgo.Option `json:"roster_mgo_opt"`
}

type DutyManager interface {
	GetCurrent(xl *xlog.Logger) ([]Staff, error)

	CreateStaff(arg *Staff) error
	UpdateStaff(id bson.ObjectId, arg *UpdateStaffArg) error
	RemoveStaff(id bson.ObjectId) error
	GetStaff(id bson.ObjectId) (Staff, error)
	ListStaffs(ids []bson.ObjectId) ([]Staff, error)

	CreateRoster(arg *Roster) error
	UpdateRoster(id bson.ObjectId, arg *UpdateRosterArg) error
	RemoveRoster(id bson.ObjectId) error
	GetRoster(id bson.ObjectId) (Roster, error)
	ListRosters() ([]Roster, error)
}

type DutyMgr struct {
	staffMgo  pmgo.Mongo
	rosterMgo pmgo.Mongo
}

func NewDutyMgr(cfg DutyCfg) (dutyMgr *DutyMgr, err error) {
	staffMgo, err := pmgo.New(cfg.StaffMgoOpt)
	if err != nil {
		log.Panic("duty: NewDutyMgr pmgo.New(cfg.RosterMgoOpt) err:", err)
	}
	rosterMgo, err := pmgo.New(cfg.RosterMgoOpt)
	if err != nil {
		log.Panic("duty: NewDutyMgr pmgo.New(cfg.RosterMgoOpt) err:", err)
	}

	dutyMgr = &DutyMgr{
		staffMgo:  staffMgo,
		rosterMgo: rosterMgo,
	}
	return
}

func getIdxByUnit(staffCnt, startIdx int, unit time.Duration, now, begin time.Time) int {
	unitElapsed := int(now.Sub(begin) / unit)
	return (unitElapsed%staffCnt + startIdx - 1) % staffCnt
}

func (d *DutyMgr) GetCurrent(xl *xlog.Logger) (staffs []Staff, err error) {
	rosters, err := d.ListRosters()
	if err != nil {
		return
	}

	for _, r := range rosters {
		now := time.Now()
		if r.Begin.After(now) || r.End.Before(now) {
			continue
		}
		switch r.Unit {
		case UnitDay:
			ids := r.Staffs[getIdxByUnit(len(r.Staffs), r.StartIdx, day, now, r.Begin)]
			staffs, err = d.ListStaffs(ids)
			return
		case UnitWeek:
			staffs, err = d.ListStaffs(r.Staffs[getIdxByUnit(len(r.Staffs), r.StartIdx, week, now, r.Begin)])
			return
		default:
			err = errors.New("Unknown Unit")
			return
		}
	}
	return
}

// ================================================
// Staff
type Staff struct {
	Id       bson.ObjectId `bson:"_id" json:"id"`
	Name     string        `bson:"name" json:"name"`
	Phones   []string      `bson:"phones" json:"phones"`
	UpdateAt time.Time     `bson:"updateAt" json:"updateAt"`
}

func (s *Staff) Check() error {
	if s.Name == "" {
		return httputil.NewError(400, "empth Name")
	}
	if len(s.Phones) == 0 {
		return httputil.NewError(400, "empty Phones")
	}
	return nil
}

func (d *DutyMgr) CreateStaff(arg *Staff) error {
	if arg.Id == "" {
		arg.Id = bson.NewObjectId()
	}
	arg.UpdateAt = time.Now()
	err := d.staffMgo.Coll().Insert(arg)
	if mgo.IsDup(err) {
		err = ErrDuplicatedStaff
	}
	return err
}

type UpdateStaffArg struct {
	Name     string    `bson:"name" json:"name"`
	Phones   []string  `bson:"phones" json:"phones"`
	UpdateAt time.Time `bson:"updateAt" json:"updateAt"`
}

func (d *DutyMgr) UpdateStaff(id bson.ObjectId, arg *UpdateStaffArg) (err error) {
	arg.UpdateAt = time.Now()
	err = d.staffMgo.Coll().UpdateId(id, M{"$set": arg})
	if err == mgo.ErrNotFound {
		err = ErrStaffNotFound
	}
	return
}

func (d *DutyMgr) RemoveStaff(id bson.ObjectId) (err error) {
	err = d.staffMgo.Coll().RemoveId(id)
	if err == mgo.ErrNotFound {
		err = ErrStaffNotFound
	}
	return
}

func (d *DutyMgr) GetStaff(id bson.ObjectId) (ret Staff, err error) {
	err = d.staffMgo.Coll().FindId(id).One(&ret)
	if err == mgo.ErrNotFound {
		err = ErrStaffNotFound
	}
	return
}

func (d *DutyMgr) ListStaffs(ids []bson.ObjectId) (ret []Staff, err error) {
	if ids == nil || len(ids) == 0 {
		err = d.staffMgo.Coll().Find(M{}).All(&ret)
	} else {
		err = d.staffMgo.Coll().Find(M{"_id": M{"$in": ids}}).All(&ret)
	}
	return
}

// ================================================
// Roster

type Unit string

const (
	UnitDay  Unit = "Day"
	UnitWeek Unit = "Week"
)

func (u Unit) Check() bool {
	switch u {
	case UnitDay:
		return true
	case UnitWeek:
		return true
	default:
		return false
	}
}

type Roster struct {
	Id       bson.ObjectId     `bson:"_id" json:"id"`
	Name     string            `bson:"name" json:"name"`
	Staffs   [][]bson.ObjectId `bson:"staffs" json:"staffs"`
	Begin    time.Time         `bson:"begin" json:"begin"`
	End      time.Time         `bson:"end" json:"end"`
	Unit     Unit              `bson:"unit" json:"unit"`
	StartIdx int               `bson:"startIdx" json:"startIdx"`
	Priority int               `bson:"priority" json:"priority"` // The smaller the number the higher the priority
	UpdateAt time.Time         `bson:"updateAt" json:"updateAt"`
}

func (s *Roster) Check() error {
	if s.Name == "" {
		return httputil.NewError(400, "empty Name")
	}
	if len(s.Staffs) == 0 {
		return httputil.NewError(400, "empty Staffs")
	}
	if s.Begin.After(s.End) {
		return httputil.NewError(400, "Begin After End")
	}
	if s.StartIdx < 0 || s.StartIdx > len(s.Staffs) {
		return httputil.NewError(400, "wrong startIdx")
	}
	if s.Priority < 0 {
		return httputil.NewError(400, "wrong priority")
	}
	if !s.Unit.Check() {
		return httputil.NewError(400, "wrong Unit")
	}
	for _, ss := range s.Staffs {
		if len(ss) == 0 {
			return httputil.NewError(400, "empty Staffs")
		}
	}
	return nil
}

func (s *Roster) Normalize() {
	s.Begin = s.Begin.Local()
	s.End = s.End.Local()
	s.Begin = time.Date(s.Begin.Year(), s.Begin.Month(), s.Begin.Day(), 0, 0, 0, 0, time.Local)
	s.End = s.End.Add(day)
	s.End = time.Date(s.End.Year(), s.End.Month(), s.End.Day(), 0, 0, 0, 0, time.Local)
}

func (d *DutyMgr) CreateRoster(arg *Roster) error {
	if arg.Id == "" {
		arg.Id = bson.NewObjectId()
	}
	cnt, err := d.rosterMgo.Coll().Find(M{}).Count()
	if err != nil {
		return httputil.NewError(http.StatusInternalServerError, err.Error())
	}
	arg.Normalize()
	arg.UpdateAt = time.Now()
	arg.Priority = cnt + 1
	err = d.rosterMgo.Coll().Insert(arg)
	if mgo.IsDup(err) {
		err = ErrDuplicatedRoster
	}
	return err
}

type UpdateRosterArg struct {
	Name     string            `bson:"name" json:"name"`
	Staffs   [][]bson.ObjectId `bson:"staffs" json:"staffs"`
	Begin    time.Time         `bson:"begin" json:"begin"`
	End      time.Time         `bson:"end" json:"end"`
	Unit     Unit              `bson:"unit" json:"unit"`
	StartIdx int               `bson:"startIdx" json:"startidx"`
	Priority int               `bson:"priority" json:"priority"`
	UpdateAt time.Time         `bson:"updateAt" json:"-"`
}

func (s *UpdateRosterArg) Check() error {
	if s.Begin.After(s.End) {
		return httputil.NewError(400, "empty Names")
	}
	if s.StartIdx < 0 || s.StartIdx > len(s.Staffs) {
		return httputil.NewError(400, "wrong startIdx")
	}
	if s.Priority < 0 {
		return httputil.NewError(400, "wrong priority")
	}
	if s.Unit.Check() {
		return httputil.NewError(400, "Unknown Unit")
	}
	return nil
}

func (d *DutyMgr) UpdateRoster(id bson.ObjectId, arg *UpdateRosterArg) (err error) {
	arg.UpdateAt = time.Now()
	err = d.rosterMgo.Coll().UpdateId(id, M{"$set": arg})
	if err == mgo.ErrNotFound {
		err = ErrRosterNotFound
	}
	return
}

func (d *DutyMgr) RemoveRoster(id bson.ObjectId) (err error) {
	err = d.rosterMgo.Coll().RemoveId(id)
	if err == mgo.ErrNotFound {
		err = ErrRosterNotFound
	}
	return
}

func (d *DutyMgr) GetRoster(id bson.ObjectId) (ret Roster, err error) {
	err = d.rosterMgo.Coll().FindId(id).One(&ret)
	if err == mgo.ErrNotFound {
		err = ErrRosterNotFound
	}
	return
}

func (d *DutyMgr) ListRosters() (ret []Roster, err error) {
	err = d.rosterMgo.Coll().Find(M{}).Sort("priority").All(&ret)
	return
}

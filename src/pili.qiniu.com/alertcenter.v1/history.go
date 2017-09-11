package alertcenter

import (
	"net/http"
	"time"

	"github.com/qiniu/http/httputil.v1"
	"github.com/qiniu/log.v1"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"

	pmgo "pili.qiniu.com/mgo"
)

var (
	ErrAlertHistoryNotFound   = httputil.NewError(http.StatusNotFound, "alertHistory not found")
	ErrDuplicatedAlertHistory = httputil.NewError(http.StatusConflict, "duplicated alertHistory")
)

type HistoryCfg struct {
	MgoOpt pmgo.Option `json:"mgo_opt"`
}

type HistoryMgr struct {
	*HistoryCfg
	mgo             pmgo.Mongo
	alertProfileMgr *AlertProfileMgr
}

func NewHistoryMgr(cfg HistoryCfg, alertProfileMgr *AlertProfileMgr) *HistoryMgr {
	mgo, err := pmgo.New(cfg.MgoOpt)
	if err != nil {
		log.Panic("history: NewHistory pmgo.New(cfg.MgoOpt) err:", err)
	}
	// TODO mgo.Coll().EnsureIndex(index)
	return &HistoryMgr{&cfg, mgo, alertProfileMgr}
}

func (hm *HistoryMgr) Create(alert *Alert) (err error) {
	if alert.Id == "" {
		alert.Id = bson.NewObjectId()
	}
	err = hm.mgo.Coll().Insert(alert)
	if err != nil {
		if mgo.IsDup(err) {
			err = ErrDuplicatedAlertHistory
		}
		return
	}
	now := time.Now()

	// try to insert into alertProfile(may already exist)
	go func() {
		err = hm.alertProfileMgr.Create(
			&AlertProfile{
				Alertname:  alert.Alertname,
				LatestTime: now,
			},
		)
		if err == ErrDuplicatedAlertProfile {
			err = hm.alertProfileMgr.UpdateLatestTime(alert.Alertname)
			if err != nil {
				log.Errorf("[HistoryMgr.Create] ==> hm.alertProfileMgr.UpdateLatestTime alertname: %v, err: %v", alert.Alertname, err)
				return
			}
			return
		}
		if err != nil {
			log.Errorf("[HistoryMgr.Create] ==> hm.alertProfileMgr.Create alertname: %v, err: %v", alert.Alertname, err)
			return
		}
		return
	}()
	return
}

type AlertHistoryUpdateArgs struct {
	Status AlertStatus `json:"status" bson:"status"`
	EndsAt time.Time   `json:"endsAt" bson:"endsAt"`
}

func (hm *HistoryMgr) Update(id bson.ObjectId, args *AlertHistoryUpdateArgs) (err error) {
	if args.Status == AlertResolved && args.EndsAt.IsZero() {
		args.EndsAt = time.Now()
	}
	err = hm.mgo.Coll().UpdateId(id, M{"$set": args})
	if err == mgo.ErrNotFound {
		err = ErrAlertHistoryNotFound
	}
	return
}

func (hm *HistoryMgr) UpdateStatus(id bson.ObjectId, status AlertStatus) (err error) {
	err = hm.mgo.Coll().UpdateId(id, M{"$set": M{"status": status}})
	if err == mgo.ErrNotFound {
		err = ErrAlertHistoryNotFound
	}
	return
}

func (hm *HistoryMgr) Ack(id bson.ObjectId, ack Ack) (err error) {
	err = hm.mgo.Coll().UpdateId(id, M{
		"$set":      M{"status": AlertAcked},
		"$addToSet": M{"acks": M{"username": ack.Username, "time": ack.Time, "comment": ack.Comment}},
	})

	if err == mgo.ErrNotFound {
		err = ErrAlertHistoryNotFound
	}
	return
}

func (hm *HistoryMgr) Rename(oldName, newName string) (ids []interface{}, err error) {
	iter := hm.mgo.Coll().Find(M{"alertname": oldName}).Sort("_id").Iter()

	var alert Alert
	for iter.Next(&alert) {
		alert.Alertname = newName
		alert.Key = alert.CalKey()
		ret, err1 := hm.mgo.Coll().UpsertId(alert.Id, M{"alertname": newName, "key": alert.Key})
		if err1 != nil {
			err = err1
			return
		}
		ids = append(ids, ret.UpsertedId)
	}
	return
}

func (hm *HistoryMgr) List(args *AlertsHistoryQuery) (ret []Alert, marker string, err error) {
	if args.Limit <= 0 || args.Limit > 1000 {
		args.Limit = 1000
	}

	now := time.Now()
	q := M{}

	timeFilter := M{}
	if args.Begin != "" {
		begin, ok := TimeOf(args.Begin)
		if !ok {
			err = httputil.NewError(400, "invalid begin time str")
			return
		}
		if begin.After(now) {
			begin = now.Add(-time.Hour)
		}
		timeFilter["$gte"] = begin
	}
	if args.End != "" {
		end, ok := TimeOf(args.End)
		if !ok {
			err = httputil.NewError(400, "invalid end time str")
			return
		}
		if end.After(now) {
			end = now
		}
		timeFilter["$lt"] = end
	}
	if len(timeFilter) != 0 {
		q["startsAt"] = timeFilter
	}

	if args.Alertname != "" {
		q["alertname"] = args.Alertname
	} else if args.Key != "" {
		q["key"] = args.Key
	}

	if args.Marker != "" {
		q["_id"] = M{"$lt": bson.ObjectIdHex(args.Marker)}
	}

	err = hm.mgo.Coll().Find(q).Sort("-_id").Limit(args.Limit).All(&ret)
	if err != nil {
		return
	}
	if len(ret) > 0 {
		marker = ret[len(ret)-1].Id.Hex()
	}
	return
}

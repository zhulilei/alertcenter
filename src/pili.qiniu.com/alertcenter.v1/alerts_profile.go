package alertcenter

import (
	"net/http"
	"sync"
	"time"

	"github.com/qiniu/http/httputil.v1"
	"github.com/qiniu/log.v1"
	"github.com/qiniu/rpc.v1"
	"github.com/qiniu/xlog.v1"
	"labix.org/v2/mgo"
	"qbox.us/cc/configversion"

	pmgo "pili.qiniu.com/mgo"
)

var (
	ErrAlertProfileNotFound   = httputil.NewError(http.StatusNotFound, "alertProfile not found")
	ErrDuplicatedAlertProfile = httputil.NewError(http.StatusNotFound, "duplicated alertProfile")
)

type AlertProfileCfg struct {
	MgoOpt       pmgo.Option `json:"mgo_opt"`
	AutoReloadMS int         `json:"auto_reload_ms"`
	ReloadColl   pmgo.Mongo  `json:"-"`
}

type AlertProfile struct {
	Alertname   string    `json:"alertname" bson:"_id"`
	Description string    `json:"description" bson:"description"`
	Tags        []string  `json:"tags" bson:"tags"`
	NeedOncall  bool      `json:"needOncall" bson:"needOncall"`
	Notifiers   []string  `json:"notifiers" bson:"notifiers"`
	IsNew       bool      `json:"isNew" bson:"isNew"`
	CreateAt    time.Time `json:"createAt" bson:"createAt"`
	LatestTime  time.Time `json:"latestTime" bson:"latestTime"`
	UpdateAt    time.Time `json:"updateAt" bson:"updateAt"`
}

func (a *AlertProfile) Check() (err error) {
	if a.Alertname == "" {
		return httputil.NewError(400, "empty alertname")
	}
	return
}

type AlertProfileMgr struct {
	*AlertProfileCfg
	mgo pmgo.Mongo

	cache      map[string]AlertProfile
	cacheMutex sync.RWMutex

	advance func() error
}

func NewAlertProfileMgr(cfg AlertProfileCfg) (apm *AlertProfileMgr) {
	mgo, err := pmgo.New(cfg.MgoOpt)
	if err != nil {
		log.Panic("NewAlertProfileMgr pmgo.New(cfg.MgoOpt) err:", err)
	}

	apm = &AlertProfileMgr{AlertProfileCfg: &cfg, mgo: mgo}
	apm.advance, err = configversion.StartReloading(
		&configversion.ReloadingConfig{
			Id:       "alert_profile",
			ReloadMs: cfg.AutoReloadMS,
			C:        cfg.ReloadColl.Coll(),
		},
		apm.reload)
	if err != nil {
		log.Errorf("configversion.StartReloading cfg:%v err:%s\n, ", cfg, err)
		return
	}
	return
}

func (apm *AlertProfileMgr) reload(l rpc.Logger) (err error) {
	xl := xlog.NewWith(l)
	aps, err := apm.List()
	if err != nil {
		xl.Error("apm.List() error:", err)
		return
	}
	cache := make(map[string]AlertProfile)
	for _, ap := range aps {
		cache[ap.Alertname] = ap
	}
	apm.cacheMutex.Lock()
	apm.cache = cache
	apm.cacheMutex.Unlock()

	return
}

func (apm *AlertProfileMgr) GetByCache(alertname string) (ret AlertProfile, ok bool) {
	apm.cacheMutex.RLock()
	defer apm.cacheMutex.RUnlock()
	ret, ok = apm.cache[alertname]
	return
}

func (apm *AlertProfileMgr) Create(ap *AlertProfile) (err error) {
	now := time.Now()
	err = ap.Check()
	if err != nil {
		return
	}
	if ap.UpdateAt.IsZero() {
		ap.UpdateAt = now
	}
	ap.CreateAt = now
	ap.IsNew = true
	err = apm.mgo.Coll().Insert(ap)
	if err == nil {
		err = apm.advance()
		return
	}
	if mgo.IsDup(err) {
		err = ErrDuplicatedAlertProfile
	}
	return
}

type AlertProfileUpdateArgs struct {
	Alertname   string    `json:"alertname" bson:"-"`
	Description string    `json:"description" bson:"description"`
	Tags        []string  `json:"tags" bson:"tags"`
	NeedOncall  bool      `json:"needOncall" bson:"needOncall"`
	IsNew       bool      `json:"isNew" bson:"isNew"`
	Notifiers   []string  `json:"notifiers" bson:"notifiers"`
	UpdateAt    time.Time `bson:"updateAt" json:"-"`
}

func (apm *AlertProfileMgr) Update(args *AlertProfileUpdateArgs) (err error) {
	args.UpdateAt = time.Now()
	err = apm.mgo.Coll().UpdateId(args.Alertname, M{"$set": args})
	if err == nil {
		err = apm.advance()
		return
	}
	if err == mgo.ErrNotFound {
		err = ErrAlertProfileNotFound
	}
	return
}

func (apm *AlertProfileMgr) UpdateLatestTime(alertname string) (err error) {
	err = apm.mgo.Coll().UpdateId(alertname, M{"$set": M{"latestTime": time.Now()}})
	if err == nil {
		err = apm.advance()
		return
	}
	if err == mgo.ErrNotFound {
		err = ErrAlertProfileNotFound
	}
	return
}

func (apm *AlertProfileMgr) Rename(oldname, newName string) (err error) {
	var ap AlertProfile
	change := mgo.Change{Remove: true}
	_, err = apm.mgo.Coll().FindId(oldname).Apply(change, &ap)
	if err != nil {
		return
	}
	err = apm.advance()
	if err != nil {
		return
	}
	ap.Alertname = newName
	_, err = apm.mgo.Coll().UpsertId(newName, ap)
	if err != nil {
		return
	}
	err = apm.advance()
	return
}

func (apm *AlertProfileMgr) AppendTags(alertnames []string, tags []string) (err error) {
	_, err = apm.mgo.Coll().UpdateAll(M{"_id": M{"$in": alertnames}}, M{"$addToSet": M{"tags": M{"$each": tags}}})
	if err == nil {
		err = apm.advance()
		return
	}
	return
}

func (apm *AlertProfileMgr) DeleteTags(alertnames []string, tags []string) (err error) {
	_, err = apm.mgo.Coll().UpdateAll(M{"_id": M{"$in": alertnames}}, M{"$pull": M{"tags": M{"$in": tags}}})
	if err == nil {
		err = apm.advance()
		return
	}
	return
}

func (apm *AlertProfileMgr) Delete(alertname string) (err error) {
	err = apm.mgo.Coll().RemoveId(alertname)
	if err == nil {
		err = apm.advance()
		return
	}
	if err == mgo.ErrNotFound {
		err = ErrAlertProfileNotFound
	}
	return
}

func (apm *AlertProfileMgr) Get(alertname string) (ret AlertProfile, err error) {
	err = apm.mgo.Coll().FindId(alertname).One(&ret)
	if err == mgo.ErrNotFound {
		err = ErrAlertProfileNotFound
	}
	return
}

func (apm *AlertProfileMgr) List() (ret []AlertProfile, err error) {
	err = apm.mgo.Coll().Find(M{}).All(&ret)
	if err != nil {
		return
	}
	return
}

package alertcenter

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/qiniu/errors"
	"github.com/qiniu/http/httputil.v1"
	"github.com/qiniu/http/rpcutil.v1"
	"github.com/qiniu/log.v1"
	"github.com/qiniu/xlog.v1"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"

	"pili.qiniu.com/alertcenter.v1/analyzer"
	pmgo "pili.qiniu.com/mgo"
)

var (
	ErrDBFatal               = httputil.NewError(500, "server fatal: db error")
	ErrAnalyzerNotFound      = httputil.NewError(404, "Analyzer not found")
	ErrAnalyzeResultNotFound = httputil.NewError(404, "Analyze result not found")
	ErrInvalidObjectId       = httputil.NewError(400, "invalid object id")
)

const (
	tySgForward = "sgForward"
)

type Service struct {
	*Config
	notifiers       Notifier
	actions         Action
	alertActiveMgr  *AlertActiveMgr
	caller          *Caller
	dutyMgr         DutyManager
	historyMgr      *HistoryMgr
	alertProfileMgr *AlertProfileMgr
	sendC           chan Message
	analyzers       map[string]Analyzer
}

func (cfg *Config) Check() {
	if cfg.MsgBacklog == 0 {
		cfg.MsgBacklog = DefaultMsgBacklog
	}
}

func NewService(cfg *Config) *Service {
	cfg.Check()

	sendC := make(chan Message, cfg.MsgBacklog)
	sendF := func(msg Message) {
		sendC <- msg
	}

	reloadMgo, err := pmgo.New(cfg.ReloadMgoOpt)
	if err != nil {
		log.Panic("pmgo.New(cfg.ReloadMgoOpt) err:", err)
	}
	cfg.AlertProfileCfg.ReloadColl = reloadMgo

	alertProfileMgr := NewAlertProfileMgr(cfg.AlertProfileCfg)
	historyMgr := NewHistoryMgr(cfg.HistoryCfg, alertProfileMgr)

	// Actions
	alertActiveMgr := NewAlertActiveMgr(sendF, cfg.AlertActiveCfg, historyMgr)
	actions := NewActions(alertActiveMgr)

	// DutyMgr
	dutyMgr, err := NewDutyMgr(cfg.DutyCfg)
	if err != nil {
		xlog.Panicln("New DutyMgr error:", err)
	}

	// Notifiers
	ns := NewNotifiers(cfg.NotifiersCfg, alertProfileMgr)

	// Caller
	caller := NewCaller(cfg.CallerCfg, dutyMgr, sendF)
	ns.Append(&caller)

	// Analyzer
	analyzers := make(map[string]Analyzer)
	for _, j := range cfg.AnalyzerCfgs {
		switch j.Type {
		case tySgForward:
			analyzers[j.Type] = analyzer.NewSgForward(j)
		default:
			log.Panic("unknown type:", j.Type)
		}
	}

	s := &Service{
		Config:          cfg,
		alertActiveMgr:  alertActiveMgr,
		actions:         actions,
		dutyMgr:         dutyMgr,
		caller:          &caller,
		analyzers:       analyzers,
		notifiers:       ns,
		sendC:           sendC,
		historyMgr:      historyMgr,
		alertProfileMgr: alertProfileMgr,
	}
	go s.Send()
	return s
}

func (s *Service) Send() {
	for {
		select {
		case msg := <-s.sendC:
			s.notifiers.Notify(msg)
		}
	}
}

type cmdArgs struct {
	CmdArgs []string
}

// =================== Alerts ===================

type PostAlertsPushArgs struct {
	Alerts []AlertForDefault `json:"alerts"`
	From   string            `json:"from"`
}

// 新增告警
func (s *Service) PostAlerts(args *PostAlertsPushArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("PostAlerts Begin, Args: %v", args)
	defer xl.Debugf("PostAlerts End")

	as := make([]*Alert, 0, len(args.Alerts))
	for _, alert := range args.Alerts {
		a := NewAlert(&alert)
		as = append(as, a)
	}
	as = s.actions.Do(xl, as)
	if len(as) == 0 {
		return
	}
	sort.Sort(ByStartsAt(as)) // 按StartedAt排序

	var wg sync.WaitGroup
	for _, a := range as {
		if a.Status == AlertResolved {
			continue
		}
		s.runAnalyzer(xl, a, &wg)
	}
	wg.Wait()

	msg := NewMessage(xl, as...)
	m, _ := json.Marshal(msg)
	xl.Debugf("%s", string(m))

	s.sendC <- msg
	return
}

type PostPrometheusAlertsPushArgs struct {
	Receiver string         `json:"receiver"`
	Status   string         `json:"status"`
	Alerts   []AlertForProm `json:"alerts"`

	GroupLabels       KV     `json:"groupLabels"`
	CommonLabels      KV     `json:"commonLabels"`
	CommonAnnotations KV     `json:"commonAnnotations"`
	ExternalURL       string `json:"externalURL"`
	// The protocol version.
	Version  string `json:"version"`
	GroupKey uint64 `json:"groupKey"`
}

func (s *Service) runAnalyzer(xl *xlog.Logger, a *Alert, wg *sync.WaitGroup) {
	mutex := sync.Mutex{}
	for _, analyzer := range s.analyzers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if analyzer.ShouldRun(a.Alertname) {
				err := analyzer.Run(a.Alertname, a.Id.Hex())
				if err == nil {
					mutex.Lock()
					a.AnalyzerTypes = append(a.AnalyzerTypes, analyzer.GetType())
					mutex.Unlock()
				}
			}
		}()
	}
}

// 新增告警（针对 Prometheus）
func (s *Service) PostPrometheusAlerts(args *PostPrometheusAlertsPushArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("PostPrometheusAlerts Begin, Args: %v", args)
	defer xl.Debugf("PostPrometheusAlerts End")

	as := make([]*Alert, 0, len(args.Alerts))
	for _, alert := range args.Alerts {
		a := NewAlert(&alert)
		as = append(as, a)
	}
	as = s.actions.Do(xl, as)
	if len(as) == 0 {
		return
	}
	sort.Sort(ByStartsAt(as)) // 按StartedAt排序

	var wg sync.WaitGroup
	for _, a := range as {
		if a.Status == AlertResolved {
			continue
		}
		s.runAnalyzer(xl, a, &wg)
	}
	wg.Wait()

	msg := NewMessage(xl, as...)
	m, _ := json.Marshal(msg)
	xl.Debugf("%s", string(m))

	s.sendC <- msg
	return
}

func (s *Service) GetActiveAlerts(env *rpcutil.Env) (ret []AlertActive, err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debug("GetActiveAlerts Begin")
	defer xl.Debugf("GetActiveAlerts End")

	as, err := s.alertActiveMgr.List()
	if err != nil {
		xl.Errorf("[alertActiveMgr.List] err: %v", err)
	}
	for _, v := range as {
		ret = append(ret, *v)
	}
	return
}

// 获取当前正在触发的告警
func (s *Service) GetAlertsActive(env *rpcutil.Env) (ret map[string]interface{}, err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debug("GetAlertsActive Begin")
	defer xl.Debugf("GetAlertsActive End")

	ret = make(map[string]interface{})

	var aas []AlertActive
	as, err := s.alertActiveMgr.List()
	if err != nil {
		xl.Errorf("[alertActiveMgr.List] err: %v", err)
	}
	for _, v := range as {
		aas = append(aas, *v)
	}
	ret["alerts"] = aas
	return
}

type AlertsHistoryQuery struct {
	Alertname string `json:"alertname"`
	Key       string `json:"key"`
	Tags      string `json:"tags"`
	Begin     string `json:"begin"`
	End       string `json:"end"`
	Limit     int    `json:"limit"`
	Marker    string `json:"marker"`
}

// 获取告警历史
func (s *Service) GetAlertsHistory(args *AlertsHistoryQuery, env *rpcutil.Env) (ret map[string]interface{}, err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("GetAlertsHistory Begin, Args: %v", args)
	defer xl.Debugf("GetAlertsHistory End")

	alerts, rmarker, err := s.historyMgr.List(args)
	if err != nil {
		xl.Errorf("[HistoryMgr.List] args: %v, err: %v", args, err)
		return
	}

	ret = bson.M{
		"items":  alerts,
		"marker": rmarker,
	}
	return
}

type AlertsAckArgs struct {
	Ids        []string `json:"id"`        // TODO
	Alertnames []string `json:"alertname"` // TODO
	Comment    string   `json:"comment"`
	Username   string   `json:"username"`
}

// 将告警设置为 Ack
func (s *Service) PostAlertsAck(args *AlertsAckArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("PostAlertsAck Begin, Args: %v", args)
	defer xl.Debugf("PostAlertsAck End")

	err = s.alertActiveMgr.Ack(args)
	if err != nil {
		xl.Errorf("[AlertProfileMgr.Ack] AlertsAckArgs: %v, err: %v", args, err)
	}
	return
}

// 创建 AlertProfile
func (s *Service) PostAlertsProfiles(args *AlertProfile, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("PostAlertsProfiles Begin, Args: %v", args)
	defer xl.Debugf("PostAlertsProfiles End")

	err = s.alertProfileMgr.Create(args)
	if err != nil {
		xl.Errorf("[AlertProfileMgr.Create] alertProfile: %v, err: %v", args, err)
	}
	return
}

// 获取所有 AlertProfile
func (s *Service) GetAlertsProfiles(env *rpcutil.Env) (ret []AlertProfile, err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("GetAlertsProfiles Begin")
	defer xl.Debugf("GetAlertsProfiles End")

	ret, err = s.alertProfileMgr.List()
	if err != nil {
		xl.Errorf("[AlertProfileMgr.List] error: %v", err)
	}
	return
}

type AlertProfileArgs struct {
	Alertname string `json:"alertname"`
}

// 获取某一个 AlertProfile
func (s *Service) GetAlertsProfile(args *AlertProfileArgs, env *rpcutil.Env) (ret AlertProfile, err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("GetAlertsProfile Begin")
	defer xl.Debugf("GetAlertsProfile End")

	ret, err = s.alertProfileMgr.Get(args.Alertname)
	if err != nil {
		xl.Errorf("[AlertProfileMgr.Get] AlertProfileArgs: %v, err: %v", args, err)
	}
	return
}

// 修改 AlertProfile
func (s *Service) PostAlertsProfilesUpdate(args *AlertProfileUpdateArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("PostAlertsProfilesUpdate Begin, Args: %v", args)
	defer xl.Debugf("PostAlertsProfilesUpdate End")

	err = s.alertProfileMgr.Update(args)
	if err != nil {
		xl.Errorf("[AlertProfileMgr.Update] AlertProfileUpdateArgs: %v, err: %v", args, err)
	}
	return
}

type AlertsProfilesTagsArgs struct {
	Type       string   `json:"type" bson:"type"`
	Alertnames []string `json:"alertname" bson:"alertname"` // TODO
	Tags       []string `json:"tags" bson:"tags"`
}

// 给告警打标签
func (s *Service) PostAlertsProfilesTags(args *AlertsProfilesTagsArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("PostAlertsProfilesTags Begin, Args: %v", args)
	defer xl.Debugf("PostAlertsProfilesTags End")

	switch args.Type {
	case "append":
		err = s.alertProfileMgr.AppendTags(args.Alertnames, args.Tags)
	case "delete":
		err = s.alertProfileMgr.DeleteTags(args.Alertnames, args.Tags)
	}

	if err != nil {
		xl.Errorf("[AlertProfileMgr.Append | Delete] AlertsProfilesTagsArgs: %v, err: %v", args, err)
	}
	return
}

type AlertProfileRenameArgs struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// 修改告警名称
// 高危操作
func (s *Service) PostAlertsProfilesRename(args *AlertProfileRenameArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("PostAlertsProfilesRename Begin, Args: %v", args)
	defer xl.Debugf("PostAlertsProfilesRename End")

	ret, err := s.historyMgr.Rename(args.Old, args.New)
	if err != nil {
		xl.Errorf("[HistoryMgr.Rename] success ids: %v, error: %v", ret, err)
	}
	s.alertProfileMgr.Rename(args.Old, args.New)
	if err != nil {
		xl.Errorf("[AlertProfileMgr.Rename] AlertProfileRenameArgs: %v, err: %v", args, err)
	}
	return
}

// 修改告警名称
// 高危操作
func (s *Service) DeleteAlertsProfiles(args *AlertProfileArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("DeleteAlertsProfiles Begin, Args: %v", args)
	defer xl.Debugf("DeleteAlertsProfiles End")

	err = s.alertProfileMgr.Delete(args.Alertname)
	if err != nil {
		xl.Errorf("[AlertProfileMgr.Delete] AlertProfileArgs: %v, err: %v", args, err)
	}

	return
}

// 获取所有的 notifiers
func (s *Service) GetAlertsNotifiers(args *AlertProfileArgs, env *rpcutil.Env) (ret []string, err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("DeleteAlertsProfiles Begin, Args: %v", args)
	defer xl.Debugf("DeleteAlertsProfiles End")

	if ns, ok := s.notifiers.(Notifiers); ok {
		ret = ns.GetNames()
	} else {
		err = httputil.NewError(http.StatusInternalServerError, "s.notifiers is not type of Notifiers")
	}
	return
}

func (s *Service) DeleteAlerts_(args *cmdArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debugf("DeleteAlerts_ Begin, Args: %v", args)
	defer xl.Debugf("DeleteAlerts_ End")

	err = s.alertActiveMgr.Delete(args.CmdArgs[0])
	if err != nil {
		xl.Errorf("[AlertActiveMgr.Delete] cmdArgs: %v, err: %v", args, err)
	}
	return
}

// =================== Caller ===================

type CallerParamsRet struct {
	StartAt      int       `json:"startAt"`
	EndAt        int       `json:"endAt"`
	CloseEndTime time.Time `json:"closeEndTime"`
}

func (s *Service) GetCallerParams(env *rpcutil.Env) (ret CallerParamsRet, err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debug("GetCallerParams Begin")
	defer xl.Debug("GetCallerParams End")

	ret = CallerParamsRet{
		StartAt:      s.caller.StartAt,
		EndAt:        s.caller.EndAt,
		CloseEndTime: s.caller.CloseEndTime,
	}
	return
}

type CallerCloseArgs struct {
	Seconds int `json:"seconds"` // 要关闭打电话功能几秒钟
}

func (s *Service) PostCallerTempclose(args *CallerCloseArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debug("PostCallerClose Begin")
	defer xl.Debug("PostCallerClose End")

	if args.Seconds <= 0 {
		return httputil.NewError(400, "Seconds parameter error")
	}
	s.caller.TempClose(xl, args.Seconds)
	return
}

func (s *Service) DeleteCallerTempclose(env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debug("DeleteCallerTempclose Begin")
	defer xl.Debug("DeleteCallerTempclose End")

	s.caller.UnsetTempClose(xl)
	return
}

type CallerSilenceArgs struct {
	StartAt string `json:"startAt"`
	EndAt   string `json:"endAt"`
}

func (s *Service) PostCallerSilence(args *CallerSilenceArgs, env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debug("PostCallerSilence Begin")
	defer xl.Debug("PostCallerSilence End")

	if args.StartAt == "" || args.EndAt == "" {
		err = httputil.NewError(400, "startAt or startAt cann't be empty")
		return
	}

	sa, err := time.Parse(timeFmt, args.StartAt)
	if err != nil {
		err = httputil.NewError(400, "startAt fmt error")
		return
	}
	ea, err := time.Parse(timeFmt, args.EndAt)
	if err != nil {
		err = httputil.NewError(400, "endAt fmt error")
		return
	}
	if sa.After(ea) {
		err = httputil.NewError(400, "startAt after endAt")
		return
	}

	err = s.caller.Silence(xl, sa.Hour()*60*60+sa.Minute()*60, ea.Hour()*60*60+ea.Minute()*60)
	return
}

func (s *Service) DeleteCallerSilence(env *rpcutil.Env) (err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debug("DeleteCallerSilence Begin")
	defer xl.Debug("DeleteCallerSilence End")

	s.caller.UnsetSilence(xl)
	return
}

// =================== Analyzer ===================

type ResultArgs struct {
	Type    string `json:"type"`
	AlertId string `json:"alertId"`
}

func (s *Service) GetAnalyzerResult(args *ResultArgs, env *rpcutil.Env) (ret interface{}, err error) {
	xl := xlog.New(env.W, env.Req)
	xl.Debug("GetAnalyzerResult_ Begin")
	defer xl.Debug("GetAnalyzerResult_ End")

	if a, ok := s.analyzers[args.Type]; ok {
		ret, err = a.GetResults(args.AlertId)
		if err != nil {
			if err == mgo.ErrNotFound {
				err = ErrAnalyzeResultNotFound
				return
			}
			err = errors.Info(ErrDBFatal, "Analyzers.GetResults", args.AlertId, args.Type).Detail(err)
			return
		}
	} else {
		err = ErrAnalyzerNotFound
	}

	return
}

/*
GET /duty/currnt
[
	{
	  "id": "hex id",
	  "name": "",
	  "phones": ["11111111111"],
	  "updateAt": ""
	},
	...
]
*/
func (s *Service) GetDutyCurrent(env *rpcutil.Env) (interface{}, error) {
	xl := xlog.New(env.W, env.Req)
	return s.dutyMgr.GetCurrent(xl)
}

// =================== staff ===================
//
//  staff 是执勤人员，包含名字和电话, 优先级高的 schedule 包含的时间范围优先于优先级低的
//
//
/*
POST /duty/staffs
{
  "name": "", // 必填
  "phones": ["11111111111"] // 必填
}
*/

func (s *Service) PostDutyStaffs(arg *Staff) (err error) {
	err = arg.Check()
	if err != nil {
		return
	}
	return s.dutyMgr.CreateStaff(arg)
}

// DELETE /duty/staff/:id
func (s *Service) DeleteDutyStaffs_(arg *cmdArgs) error {
	id := arg.CmdArgs[0]
	if !bson.IsObjectIdHex(id) {
		return ErrInvalidObjectId
	}
	return s.dutyMgr.RemoveStaff(bson.ObjectIdHex(id))
}

/*
POST /duty/staff/:id
{
  "name": "", // 必填
  "phones": ["11111111111"] // 必填
}
*/
type updateStaffArg struct {
	CmdArgs []string
	UpdateStaffArg
}

func (s *Service) PostDutyStaffs_(arg *updateStaffArg) (err error) {
	id := arg.CmdArgs[0]
	if !bson.IsObjectIdHex(id) {
		return ErrInvalidObjectId
	}
	return s.dutyMgr.UpdateStaff(bson.ObjectIdHex(id), &arg.UpdateStaffArg)
}

/*
GET /duty/staff/:id

{
  "id": "hex id",
  "name": "",
  "phones": ["11111111111"],
  "updateAt": ""
}
*/
func (s *Service) GetDutyStaffs_(arg *cmdArgs) (interface{}, error) {
	id := arg.CmdArgs[0]
	if !bson.IsObjectIdHex(id) {
		return nil, ErrInvalidObjectId
	}
	return s.dutyMgr.GetStaff(bson.ObjectIdHex(id))
}

/*
GET /duty/staffs
[
  "hex id",
  ...
]

200 OK
[
  {
    "id": "hex id",
    "name": "",
    "phones": ["11111111111"],
    "updateAt": ""
  },
  ...
]
*/
func (s *Service) GetDutyStaffs() (interface{}, error) {
	return s.dutyMgr.ListStaffs(nil)
}

// =================== roster ===================
//
//  roster 是执勤人员表，一个 Unit(day or week) 对应 roster 数组的一个元素。
//
/*
POST /duty/rosters
{
  "name": "", // 必填
  "begin": "",
  "end": "",
  "unit": "Day" | "Week",
  "startIdx": 1, // 从 roster 的 staffs 数组的哪个位置开始
  "priority", // 越小优先级越高
  "staffs": [ // 必填
    [
      {
        "name": "",
        "phone": ""
      },
      ...
    ]
  ]
}
*/

func (s *Service) PostDutyRosters(arg *Roster) (err error) {
	err = arg.Check()
	if err != nil {
		return
	}
	return s.dutyMgr.CreateRoster(arg)
}

// DELETE /duty/roster/:id
func (s *Service) DeleteDutyRosters_(arg *cmdArgs) error {
	id := arg.CmdArgs[0]
	if !bson.IsObjectIdHex(id) {
		return ErrInvalidObjectId
	}
	return s.dutyMgr.RemoveRoster(bson.ObjectIdHex(id))
}

/*
POST /duty/roster/:id
{
  "name": "", // 必填
  "begin": "",      // 必填
  "end": "",        // 必填
  "unit": "Day" | "Week", // 必填
  "startIdx": 1,      // 必填
  "priority",        // 必填
  "staffs": [ // 必填
    ...
  ]
}
*/
type updateRosterArg struct {
	CmdArgs []string
	UpdateRosterArg
}

func (s *Service) PostDutyRosters_(arg *updateRosterArg) (err error) {
	id := arg.CmdArgs[0]
	if !bson.IsObjectIdHex(id) {
		return ErrInvalidObjectId
	}
	return s.dutyMgr.UpdateRoster(bson.ObjectIdHex(id), &arg.UpdateRosterArg)
}

type batchUpdateRosterArg struct {
	Id string `json:"id"`
	UpdateRosterArg
}

func (s *Service) PostBatchDutyRostersUpdate(env *rpcutil.Env) (err error) {
	var args []batchUpdateRosterArg
	err = json.NewDecoder(env.Req.Body).Decode(&args)
	if err != nil {
		return
	}
	for _, arg := range args {
		if !bson.IsObjectIdHex(arg.Id) {
			return ErrInvalidObjectId
		}
		err = s.dutyMgr.UpdateRoster(bson.ObjectIdHex(arg.Id), &arg.UpdateRosterArg)
	}
	return
}

/*
GET /duty/roster/:id

{
  "id": "hex id",
  "name": "",
  begin": "",
  "end": "",
  "unit": "Day" | "Week",
  "startIdx": 1,
  "priority",
  "updateAt": "",
  "staffs": [
    ...
  ]
}
*/
func (s *Service) GetDutyRosters_(arg *cmdArgs) (interface{}, error) {
	id := arg.CmdArgs[0]
	if !bson.IsObjectIdHex(id) {
		return nil, ErrInvalidObjectId
	}
	return s.dutyMgr.GetRoster(bson.ObjectIdHex(id))
}

/*
GET /duty/rosters

[
  {
    "id": "hex id",
    "name": "",
    "begin": "",
    "end": "",
    "unit": "Day" | "Week",
    "startIdx": 1,
    "priority",
    "updateAt": "",
    "staffs": [
      ...
    ]
  },
  ...
]
*/
func (s *Service) GetDutyRosters() (interface{}, error) {
	return s.dutyMgr.ListRosters()
}

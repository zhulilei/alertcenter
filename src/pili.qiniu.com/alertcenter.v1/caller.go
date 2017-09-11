package alertcenter

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/qiniu/rpc.v1"
	"github.com/qiniu/xlog.v1"
	"qbox.us/oauth"
)

const (
	DefaultCallerMsg          = "123456"
	DefaultCallerFailTryTimes = 2
	DefaultCallIntervals      = 60 * 5
	DefaultReCallTimes        = 2
	DefaultReCallIntervals    = 60
	DefaultCallerFile         = "run/caller.data"
	timeFmt                   = "15:04"
	daySeconds                = 24 * 60 * 60
	CallerName                = "caller"
)

var morseMsgRegex = regexp.MustCompile(`^[\da-zA-Z]{4,8}$`)

// ========================================

type Transport struct {
	clientID  string
	Transport http.RoundTripper
}

func NewTransport(clientId string, transport http.RoundTripper) *Transport {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Transport{clientId, transport}
}

func (t *Transport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	req.Header.Set("Client-Id", t.clientID)
	return t.Transport.RoundTrip(req)
}

type MorseClient struct {
	Host   string
	Client *rpc.Client
}

func NewMorseClient(host string, client *rpc.Client) *MorseClient {
	return &MorseClient{host, client}
}

type SendSmsIn struct {
	Uid         uint   `json:"uid"`
	PhoneNumber string `json:"phone_number"`
	Message     string `json:"message"`
}

type SendOut struct {
	Oid string `json:"oid"`
}

func (m *MorseClient) SendVoiceSms(logger rpc.Logger, param SendSmsIn) (oid string, err error) {
	var resp map[string]interface{}
	err = m.Client.CallWithJson(logger, &resp, m.Host+"/api/notification/send/voicesms", &param)
	if err != nil {
		return "", err
	}
	if errMsg, has := resp["error"]; has {
		if errMsg, ok := errMsg.(string); ok {
			err = errors.New(errMsg)
		} else {
			err = errors.New("error field is not string")
		}
		return
	}
	if v, has := resp["oid"]; has {
		var ok bool
		if oid, ok = v.(string); !ok {
			err = errors.New("error field is not string")
		}
		return
	}
	err = errors.New("Unknown Resp")
	return
}

// ========================================

type CallerCfg struct {
	FilePath        string   `json:"file_path"`
	ClientId        string   `json:"client_id"`
	MorseHost       string   `json:"morse_host"`
	MorseUid        uint     `json:"morse_uid"`
	Alerts          []string `json:"alerts"`
	FailTryTimes    int      `json:"fail_try_times"`
	CallIntervals   int      `json:"call_intervals"` // 同类告警多少秒内打过一次了就先不打，尽量减低打电话的频率，减少不必要的成本，目前不做同类告警下更细分的处理
	RecallTimes     int      `json:"recall_times"`
	RecallIntervals int      `json:"recall_intervals"`
}

type CallerParams struct {
	StartAt      int       `json:"start_at"`       // 每天第几秒开始不打电话, -1 表示未设置
	EndAt        int       `json:"end_at"`         // 每天第几秒结束不打电话, -1 表示未设置
	CloseEndTime time.Time `json:"close_end_time"` // 暂停电话功能的结束时间点
}

type Caller struct {
	*CallerCfg
	CallerParams

	dutyMgr DutyManager
	morse   *MorseClient
	f       func(msg Message)
	mutex   sync.RWMutex
	alerts  map[string]time.Time // key 是要打电话的 alertname，value 是该告警上一次打电话的时间点
}

func NewAdminOAuth(tr http.RoundTripper, host, user, pwd string) (*oauth.Transport, error) {

	transport := &oauth.Transport{
		Config: &oauth.Config{
			Scope:    "Scope",
			TokenURL: host + "/oauth2/token",
		},
		Transport: tr,
	}
	_, code, err := transport.ExchangeByPassword(user, pwd)

	xlog.Infof("NewAdminOAuth", "admin token refresh status, code: [%v], err: [%v]", code, err)

	if code != http.StatusOK || err != nil {
		return nil, err
	}

	return transport, nil
}

func (cfg *CallerCfg) Check() {
	if cfg.FilePath == "" {
		cfg.FilePath = DefaultCallerFile
	}
	if cfg.CallIntervals == 0 {
		cfg.CallIntervals = DefaultCallIntervals
	}
	if cfg.FailTryTimes == 0 {
		cfg.FailTryTimes = DefaultCallerFailTryTimes
	}
	if cfg.RecallIntervals == 0 {
		cfg.RecallIntervals = DefaultReCallIntervals
	}
}

func NewCaller(cfg CallerCfg, dutyMgr DutyManager, f func(msg Message)) Caller {
	cfg.Check()

	tr := NewTransport(cfg.ClientId, nil)
	client := NewMorseClient(cfg.MorseHost, &rpc.Client{Client: &http.Client{Transport: tr}})

	var params CallerParams
	err := load(xlog.NewDummy(), &params, cfg.FilePath)
	if err != nil {
		params.StartAt = -1
		params.EndAt = -1
	}

	return Caller{
		CallerCfg:    &cfg,
		CallerParams: params,
		dutyMgr:      dutyMgr,
		morse:        client,
		f:            f,
		alerts:       make(map[string]time.Time),
	}
}

func (c *Caller) Notify(msg Message) (err error) {
	xl := msg.xl
	for _, a := range msg.Alerts {
		if a.Status == AlertResolved {
			continue
		}

		// 关闭时间点之前不Call
		now := time.Now()
		if now.Before(c.CloseEndTime) {
			xl.Info("<In CloseEndTime>", a.Description)
			continue
		}

		h := int(now.Unix() % daySeconds)
		// silence 时间段内不Call
		if c.StartAt != -1 && c.EndAt != -1 {
			if h >= c.StartAt && h <= c.EndAt {
				xl.Info("<In SilenceTime>", a.Description)
				continue
			}
		}

		c.mutex.RLock()
		// CallIntervals 时间段内不Call
		if time.Now().Sub(c.alerts[a.Alertname]) < time.Duration(c.CallIntervals)*time.Second {
			xl.Info("<In CallIntervals>", a.Description)
			c.mutex.RUnlock()
			continue
		}
		c.mutex.RUnlock()

		for i := 0; i < c.FailTryTimes+1; i++ {
			err1 := c.SendVoiceSms(xl, a.Description)
			if err1 == nil {
				c.mutex.Lock()
				c.alerts[a.Alertname] = time.Now()
				c.mutex.Unlock()
				break
			}
			err = err1
		}

		go c.recall(xl, a.Description)
	}
	return
}

func (c *Caller) recall(xl *xlog.Logger, msg string) {
	cnt := 0
	for range time.After(time.Duration(c.RecallIntervals) * time.Second) {
		if cnt == c.RecallTimes {
			return
		}
		err := c.SendVoiceSms(xl, msg)
		if err != nil {
			xl.Errorf("recall Err, Time: %v, Err: %v", cnt, err)
		}
		cnt++
	}
}

func (c *Caller) TempClose(xl *xlog.Logger, s int) {
	defer save(xl, c.CallerParams, c.FilePath)
	c.CloseEndTime = time.Now().Add(time.Duration(s) * time.Second)
	return
}

func (c *Caller) UnsetTempClose(xl *xlog.Logger) {
	defer save(xl, c.CallerParams, c.FilePath)
	c.CloseEndTime = time.Time{}
	return
}

func (c *Caller) Silence(xl *xlog.Logger, startAt, endAt int) (err error) {
	defer save(xl, c.CallerParams, c.FilePath)
	c.StartAt = startAt
	c.EndAt = endAt
	return
}

func (c *Caller) UnsetSilence(xl *xlog.Logger) (err error) {
	defer save(xl, c.CallerParams, c.FilePath)
	c.StartAt = -1
	c.EndAt = -1
	return
}

func (c *Caller) SendVoiceSms(xl *xlog.Logger, msg string) (err error) {
	xl.Info("(c *Caller) SendVoiceSms Begin", msg)
	defer xl.Info("(c *Caller) SendVoiceSms End")

	if msg == "" {
		msg = DefaultCallerMsg
	}
	if i, err := strconv.ParseInt(msg, 10, 64); err != nil || i < 100000 {
		msg = DefaultCallerMsg
	}
	staffs, err := c.dutyMgr.GetCurrent(xl)
	if err != nil {
		errMsg := fmt.Sprint("c.dutyMgr.GetCurrent(xl) error", err)
		xl.Error(errMsg)
		c.notifyErr(xl, errMsg)
		return
	}
	phones := []string{}
	for _, staff := range staffs {
		for _, phone := range staff.Phones {
			phones = append(phones, phone)
		}
	}
	for _, phone := range phones {
		param := SendSmsIn{
			Uid:         c.MorseUid,
			PhoneNumber: phone,
			Message:     msg, // 由于 morse api 的限制，这边只能填死
		}
		oid, err1 := c.morse.SendVoiceSms(xl, param)
		if err1 != nil {
			errMsg := fmt.Sprintf("Caller.SendVoiceSms param: %#v, Error: %v", param, err1)
			xl.Errorf(errMsg)
			c.notifyErr(xl, errMsg)
			err = err1
			continue
		}
		xl.Infof("SendVoiceSms to %v success, Oid is %v", phone, oid)
	}
	return
}

func (c *Caller) notifyErr(xl *xlog.Logger, errMsg string) {
	as := []*Alert{
		{
			Id:          "notifyErr Message",
			Status:      AlertFiring,
			Severity:    SeverityCritical,
			Description: "打电话失败 " + errMsg,
			StartsAt:    time.Now(),
		},
	}
	c.f(NewMessage(xl, as...))
}

func (n *Caller) Name() string {
	return CallerName
}

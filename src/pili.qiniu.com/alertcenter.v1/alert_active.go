package alertcenter

import (
	"errors"
	"sync"
	"time"

	"github.com/qiniu/xlog.v1"
)

const (
	DefaultEmergencyIntervalS = 2 * 60 * 60
	DefaultResendIntervalS    = 30 * 60
	DefaultBackupIntervalMS   = 60 * 1e3
	DefaultBackupfile         = "run/active.data"
)

type AlertActiveCfg struct {
	EmergenctIntervalS int    `json:"emergency_interval_s"`
	ResendIntervalS    int    `json:"resend_interval_s"`
	BackupFile         string `json:"backup_file"`
	BackupIntervalMS   int    `json:"backup_interval_ms"`
}

func (cfg *AlertActiveCfg) Check() {
	if cfg.EmergenctIntervalS == 0 {
		cfg.EmergenctIntervalS = DefaultEmergencyIntervalS
	}
	if cfg.ResendIntervalS == 0 {
		cfg.ResendIntervalS = DefaultResendIntervalS
	}
	if cfg.BackupIntervalMS == 0 {
		cfg.BackupIntervalMS = DefaultBackupIntervalMS
	}
	if cfg.BackupFile == "" {
		cfg.BackupFile = DefaultBackupfile
	}
}

type AlertActive struct {
	*Alert

	xl        *xlog.Logger
	Tick      *time.Ticker  `json:"-"`
	StopTickC chan struct{} `json:"-"`
	ReqId     string        `json:"-"`
}

func NewAlertActive(alert *Alert, xl *xlog.Logger) *AlertActive {
	return &AlertActive{
		Alert: alert,
		xl:    xl,
		ReqId: xl.ReqId(),
	}
}

func (aa *AlertActive) StopTick() {
	close(aa.StopTickC)
}

func (aa *AlertActive) EmergentLeft(EmergenctIntervalS int) time.Duration {
	if aa.IsEmergent {
		return 0
	}
	d := time.Duration(time.Duration(EmergenctIntervalS)*time.Second - time.Now().Sub(aa.StartsAt))
	if d > 0 {
		return d
	}
	return 0
}

type AlertActiveMgr struct {
	*AlertActiveCfg
	mutex      sync.Mutex
	data       map[string]*AlertActive
	f          func(msg Message)
	historyMgr *HistoryMgr
}

func NewAlertActiveMgr(f func(msg Message), cfg AlertActiveCfg, historyMgr *HistoryMgr) (aam *AlertActiveMgr) {
	xl := xlog.NewDummy()
	cfg.Check()
	data := make(map[string]*AlertActive)

	aam = &AlertActiveMgr{
		AlertActiveCfg: &cfg,
		data:           data,
		f:              f,
		historyMgr:     historyMgr,
	}
	load(xl, &data, cfg.BackupFile)

	// DoEmergenct
	for _, v := range data {
		v.xl = xlog.NewWith(v.ReqId)
		go aam.DoEmergenct(v)
	}

	go aam.TimingBackup(xl)
	return
}

func (aam *AlertActiveMgr) DoEmergenct(aa *AlertActive) {
	if aa.Status == AlertAcked {
		return
	}
	aa.StopTickC = make(chan struct{})
	if d := aa.EmergentLeft(aam.EmergenctIntervalS); d > 0 {
		select {
		case <-aa.StopTickC:
			return
		case <-time.After(d):
		}
	}

	// begin resend
	aa.IsEmergent = true
	aam.f(NewMessage(aa.xl, aa.Alert)) // oncall

	aa.Tick = time.NewTicker(time.Duration(aam.ResendIntervalS) * time.Second)
	for {
		// 如果一个告警发到alertcenter，同时该告警的startsAt是比较早以前的值，是有可能会出现通知两次情况
		// 不过正常情况下不应该出现
		select {
		case <-aa.StopTickC:
			aa.Tick.Stop()
			return
		case <-aa.Tick.C:
			aam.f(NewMessage(aa.xl, aa.Alert))
		}
	}
}

func (aam *AlertActiveMgr) List() (aaMap map[string]*AlertActive, err error) {
	aam.mutex.Lock()
	defer aam.mutex.Unlock()

	aaMap = aam.data
	return
}

func (aam *AlertActiveMgr) Get(key string) (aa *AlertActive, ok bool) {
	aam.mutex.Lock()
	defer aam.mutex.Unlock()

	aa, ok = aam.data[key]
	return
}

func (aam *AlertActiveMgr) Delete(key string) (err error) {
	aam.mutex.Lock()
	defer aam.mutex.Unlock()

	return aam.delete(key)
}

func (aam *AlertActiveMgr) delete(key string) (err error) {
	if aa, ok := aam.data[key]; ok {
		delete(aam.data, key)
		aa.StopTick()
		return
	}
	err = errors.New("cann't find key")
	return
}

func (aam *AlertActiveMgr) Ack(args *AlertsAckArgs) (err error) {
	aam.mutex.Lock()
	defer aam.mutex.Unlock()

	f := func(a *AlertActive) error {
		a.Status = AlertAcked
		ack := Ack{args.Comment, time.Now(), args.Username}
		a.Acks = append(a.Acks, ack)
		a.StopTick()
		return aam.historyMgr.Ack(a.Id, ack)
	}

	if len(args.Alertnames) != 0 {
		for _, an := range args.Alertnames {
			for _, alert := range aam.data {
				if alert.Alertname == an {
					err = f(alert)
				}
			}
		}
	}

	for _, id := range args.Ids {
		for _, alert := range aam.data {
			if alert.Id.Hex() == id {
				err = f(alert)
			}
		}
	}

	return
}

func (aam *AlertActiveMgr) Add(xl *xlog.Logger, a *Alert) (err error) {
	aam.mutex.Lock()
	defer aam.mutex.Unlock()

	return aam.add(xl, a)
}

func (aam *AlertActiveMgr) add(xl *xlog.Logger, a *Alert) (err error) {
	aa := NewAlertActive(a, xl)
	aam.data[a.Key] = aa
	go aam.DoEmergenct(aa)
	return
}

func (aam *AlertActiveMgr) Close() {
	aam.mutex.Lock()
	defer aam.mutex.Unlock()

	for _, aa := range aam.data {
		aa.StopTick()
	}
	return
}

func (aam *AlertActiveMgr) TimingBackup(xl *xlog.Logger) (err error) {
	for range time.Tick(time.Duration(aam.BackupIntervalMS) * time.Millisecond) {
		err := aam.Backup(xl)
		if err != nil {
			xl.Errorf("Backup(%v) failed, err: %v", aam.BackupFile, err)
			continue
		}
		xl.Infof("Backup(%v) success", aam.BackupFile)
	}
	return
}

func (aam *AlertActiveMgr) Backup(xl *xlog.Logger) (err error) {
	aam.mutex.Lock()
	defer aam.mutex.Unlock()

	err = save(xl, aam.data, aam.BackupFile)
	return
}

// implement action interface
func (aam *AlertActiveMgr) Do(xl *xlog.Logger, as []*Alert) (newAs []*Alert) {
	aam.mutex.Lock()
	defer aam.mutex.Unlock()

	for _, a := range as {
		if !a.NeedHandle {
			err := aam.historyMgr.Create(a)
			if err != nil {
				xl.Errorf("AlertActiveMgr.Do ==> aam.historyMgr.Create alert: %v, err: %v", a, err)
			}

			newAs = append(newAs, a)
			continue
		}
		if b, ok := aam.data[a.Key]; ok {
			if a.Status == AlertResolved {
				a.Severity = SeveritySuccess
				err := aam.historyMgr.Update(b.Id, &AlertHistoryUpdateArgs{a.Status, a.EndsAt})
				if err != nil {
					xl.Errorf("AlertActiveMgr.Do ==>  aam.historyMgr.Update alert: %v, err: %v", a, err)
				}

				err = aam.delete(a.Key)
				if err != nil {
					xl.Error("aam.delete(a.Key) a: %v, err: %v", a, err)
				}

				newAs = append(newAs, a)
			}
		} else {
			if a.Status == AlertFiring {
				err := aam.historyMgr.Create(a)
				if err != nil {
					xl.Errorf("AlertActiveMgr.Do ==> aam.historyMgr.Create(alert) alert: %v, err: %v", a, err)
				}

				err = aam.add(xl, a)
				if err != nil {
					xl.Errorf("AlertActiveMgr.Do ==> aam.add(xl, a) alert: %v, err: %v", a, err)
				}
				newAs = append(newAs, a)
			}
		}
	}
	return
}

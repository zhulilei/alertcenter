package alertcenter

import (
	"github.com/qiniu/log.v1"
)

type NotifiersCfg struct {
	Default   string              `json:"default"`
	Routes    map[string][]string `json:"routes"`
	SlackCfgs []SlackCfg          `json:"slack_cfgs"`
	QQCfgs    []QQCfg             `json:"qq_cfgs"`
}

func (cfg *NotifiersCfg) Check() {
	if cfg.Default == "" {
		log.Panic("miss default field")
	}
}

type Notifier interface {
	Name() string
	Notify(msg Message) error
}

type Notifiers struct {
	NotifiersCfg
	names     []string
	notifiers map[string]Notifier
	musts     map[string]Notifier

	apMgr *AlertProfileMgr
}

func NewNotifiers(cfg NotifiersCfg, apMgr *AlertProfileMgr) Notifiers {
	cfg.Check()
	ns := make(map[string]Notifier)
	names := make([]string, 0, len(cfg.SlackCfgs)+len(cfg.QQCfgs))

	// Slack
	for _, c := range cfg.SlackCfgs {
		n := NewSlack(c)
		ns[n.Name()] = n
		names = append(names, n.Name())
	}
	// QQ
	for _, c := range cfg.QQCfgs {
		n := NewQQ(c)
		ns[n.Name()] = n
		names = append(names, n.Name())
	}
	return Notifiers{
		NotifiersCfg: cfg,
		names:        names,
		notifiers:    ns,
		musts:        make(map[string]Notifier),
		apMgr:        apMgr,
	}
}

func (ns *Notifiers) Append(n Notifier) {
	ns.notifiers[n.Name()] = n
	ns.names = append(ns.names, n.Name())
}

func (ns *Notifiers) AppendMust(n Notifier) {
	ns.musts[n.Name()] = n
	ns.names = append(ns.names, n.Name())
}

func (ns Notifiers) Name() string {
	return "Notifiers"
}

func (ns Notifiers) Notify(msg Message) (err error) {
	go ns.MustNotify(msg)

	nMsgMap := make(map[string]Message) // notifier => message
	for _, a := range msg.Alerts {
		var notifiers []string
		if ap, ok := ns.apMgr.GetByCache(a.Alertname); ok {
			if len(ap.Notifiers) != 0 {
				notifiers = ap.Notifiers
			}
		}

		// use default
		if len(notifiers) == 0 {
			notifiers = []string{ns.Default}
		}

		// handle oncall
		if ap, ok := ns.apMgr.GetByCache(a.Alertname); a.IsEmergent || (ok && ap.NeedOncall) {
			notifiers = append(notifiers, CallerName)
		}

		for _, notifier := range notifiers {
			m, ok := nMsgMap[notifier]
			if !ok {
				m.xl = msg.xl
			}
			m.Alerts = append(m.Alerts, a)
			nMsgMap[notifier] = m
		}
	}
	for _, notifier := range ns.notifiers {
		if msg, ok := nMsgMap[notifier.Name()]; ok {
			go notifier.Notify(msg)
		}
	}
	return
}

func (ns Notifiers) MustNotify(msg Message) (err error) {
	for _, n := range ns.musts {
		n.Notify(msg)
	}
	return
}

func (ns Notifiers) GetNames() (names []string) {
	return ns.names
}

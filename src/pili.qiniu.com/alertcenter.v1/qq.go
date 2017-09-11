package alertcenter

import (
	"net/url"
	"strings"

	"github.com/qiniu/log.v1"
	"github.com/qiniu/rpc.v1"
	"github.com/qiniu/xlog.v1"

	"qbox.us/digest_auth"
)

const (
	DefaultQQPath   = "qq/send_messag"
	DefaultQQHost   = "https://x.qiniuts.com"
	DefaultMaxLines = 5
)

type QQCfg struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Path     string `json:"path"`
	Robot    string `json:"robot"`
	To       string `json:"to"`
	OpType   string `json:"op_type"`
	AK       string `json:"ak"`
	SK       string `json:"sk"`
	MaxLines int    `json:"max_lines"`
}

type QQ struct {
	*QQCfg
	Cli rpc.Client
}

func (cfg *QQCfg) Check() {
	if cfg.Name == "" {
		log.Panic("miss Name of slackCfg")
	}
	if cfg.Path == "" {
		cfg.Path = DefaultQQPath
	}
	if cfg.Robot == "" {
		log.Panic("miss Robot of slackCfg")
	}
	if cfg.To == "" {
		log.Panic("miss To of slackCfg")
	}
	if cfg.OpType == "" {
		log.Panic("miss OpType of slackCfg")
	}
	if cfg.AK == "" {
		log.Panic("miss AK of slackCfg")
	}
	if cfg.Host == "" {
		cfg.Host = DefaultQQHost
	}
	if cfg.SK == "" {
		log.Panic("miss SK of slackCfg")
	}
	if cfg.MaxLines == 0 {
		cfg.MaxLines = DefaultMaxLines
	}
}

func NewQQ(cfg QQCfg) *QQ {
	cfg.Check()

	return &QQ{
		QQCfg: &cfg,
		Cli: rpc.Client{
			Client: digest_auth.NewClient(cfg.AK, cfg.SK, nil),
		},
	}
}

func (n *QQ) Name() string {
	return n.QQCfg.Name
}

func (n *QQ) Notify(msg Message) (err error) {
	xl := msg.xl
	if len(msg.Alerts) == 0 {
		return
	}
	desc := ""
	for i, a := range msg.Alerts {
		if i == n.MaxLines {
			continue
		}
		desc += a.Description + "\n"
		if !strings.HasPrefix(a.GeneratorURL, "http://") && !strings.HasPrefix(a.GeneratorURL, "https://") {
			a.GeneratorURL = "http://" + a.GeneratorURL
		}
		desc += a.GeneratorURL
	}

	err = n.SendMsg(xl, desc)
	return
}

func (n *QQ) SendMsg(xl *xlog.Logger, desc string) (err error) {
	params := url.Values{}
	params.Add("to", n.To)
	params.Add("msg", desc)
	params.Add("robot", n.Robot)
	params.Add("op_type", n.OpType)
	err = n.Cli.CallWithForm(xl, nil, n.Host+"/"+n.Path, params)
	if err != nil {
		xl.Errorf("QQ Notify(msg) error: %+v", err)
		return
	}
	return
}

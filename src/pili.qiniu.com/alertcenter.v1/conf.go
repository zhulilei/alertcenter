package alertcenter

import (
	"pili.qiniu.com/alertcenter.v1/analyzer"

	pmgo "pili.qiniu.com/mgo"
)

const (
	DefaultMsgBacklog = 10
)

type Config struct {
	BindHost   string `json:"bind_host"`
	DebugLevel int    `json:"debug_level"`

	NotifiersCfg    NotifiersCfg    `json:"notifiers_cfg"`
	CallerCfg       CallerCfg       `json:"caller_cfg"`
	AlertActiveCfg  AlertActiveCfg  `json:"alert_active_cfg"`
	HistoryCfg      HistoryCfg      `json:"history_cfg"`
	AlertProfileCfg AlertProfileCfg `json:"alerts_profile_cfg"`
	ReloadMgoOpt    pmgo.Option     `json:"reload_mgo_opt"`
	DutyCfg         DutyCfg         `json:"duty_cfg"`
	MsgBacklog      int             `json:"msg_backlog"`

	AnalyzerCfgs []analyzer.Config `json:"jobs"`
}

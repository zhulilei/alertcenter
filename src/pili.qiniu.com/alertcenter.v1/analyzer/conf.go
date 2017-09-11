package analyzer

import (
	pmgo "pili.qiniu.com/mgo"
)

type Config struct {
	Type            string            `json:"type"`
	AlertnameTagMap map[string]string `json:"alertname_tag_map"`

	SrcMgoCfg    pmgo.Option `json:"src_mgo_cfg"`
	ResultMgoCfg pmgo.Option `json:"result_mgo_cfg"`
	Limit        int         `json:"limit"`
}

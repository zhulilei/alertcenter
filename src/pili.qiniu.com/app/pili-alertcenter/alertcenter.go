package main

import (
	"net/http"

	"github.com/qiniu/http/restrpc.v1"
	"github.com/qiniu/log.v1"
	"github.com/qiniu/reqid.v1"
	"github.com/qiniu/xlog.v1"

	"pili.qiniu.com/alertcenter.v1"
	"qbox.us/cc/config"
)

func LoadConf() (conf *alertcenter.Config) {
	if err := config.Load(&conf); err != nil {
		log.Fatal("config.Load failed:", err)
	}
	return
}

func main() {
	config.Init("f", "pili-alertcenter", "pili-alertcenter.conf")
	conf := LoadConf()

	log.SetOutputLevel(conf.DebugLevel)
	xlog.SetGenReqId(reqid.Gen)

	log.Info("Bind host = ", conf.BindHost, "DebugLevel = ", conf.DebugLevel)

	service := alertcenter.NewService(conf)

	log.Infof("starting pili-alertcenter @%s", conf.BindHost)

	router := restrpc.Router{}
	mux := router.Register(service)
	err := http.ListenAndServe(conf.BindHost, mux)

	log.Fatal("http.ListenAndServe:", err)
}

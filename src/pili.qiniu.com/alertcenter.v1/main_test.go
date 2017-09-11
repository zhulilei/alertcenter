package alertcenter

import (
	"testing"
	"time"

	"github.com/qiniu/http/restrpc.v1"
	"github.com/qiniu/log.v1"
	"github.com/qiniu/xlog.v1"
	"github.com/stretchr/testify/assert"
	"labix.org/v2/mgo"

	"pili.qiniu.com/alertcenter.v1/analyzer"
	pmgo "pili.qiniu.com/mgo"
	"qiniupkg.com/qiniutest/httptest.v1"
	"qiniupkg.com/x/mockhttp.v7"
)

func initService() (s *Service) {
	return NewService(
		&Config{
			NotifiersCfg: NotifiersCfg{
				Default: "test",
				SlackCfgs: []SlackCfg{
					{
						Name:          "test",
						Hosts:         []string{"https://hooks.slack.com"},
						ServiceId:     "T025NH73W/B4M5CM31V/J0r7yZ7sz95q4bkEH9itzjgf",
						MaxDisplayCnt: 2,
					},
				},
			},
			ReloadMgoOpt: pmgo.Option{
				MgoDB:   "test-alertcenter",
				MgoColl: "reload",
			},
			AlertProfileCfg: AlertProfileCfg{
				MgoOpt: pmgo.Option{
					MgoDB:   "test-alertcenter",
					MgoColl: "alertprofile",
				},
			},
			HistoryCfg: HistoryCfg{
				MgoOpt: pmgo.Option{
					MgoDB:   "test-alertcenter",
					MgoColl: "history",
				},
			},
		},
	)
}

func clearTestDB() {
	session, err := mgo.Dial("127.0.0.1")
	if err != nil {
		panic("dial to mongo failed.")
	}
	session.DB("test-alertcenter").DropDatabase()
}

func TestReal(t *testing.T) {
	log.Println("TestReal Begin")
	defer log.Println("TestReal End")
	defer clearTestDB()
	xlog.SetOutputLevel(xlog.Ldebug)
	service := initService()
	transport := mockhttp.NewTransport()
	router := restrpc.Router{
		Mux: restrpc.NewServeMux(),
	}
	transport.ListenAndServe("localhost", router.Register(service))

	ctx := httptest.New(t)
	ctx.SetTransport(transport)

	ctx.Exec(`
	post http://localhost/prometheus/alerts
	json '
	{
		"receiver": "webhook-test",
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"labels": {
					"alertname": "test1",
					"dir": "in",
					"monitor": "qiniu-bcgateway",
					"severity": "warning",
					"node": "tan14-fjfz-tel-1-2"
				},
				"annotations": {
					"description": "tan14-fjfz-tel-1-2 pili-streamd fd 4444 > 3000"
				},
				"startsAt": "2016-11-24T00:21:45.887+08:00",
				"endsAt": "0001-01-01T00:00:00Z",
				"generatorURL": "http://pili.bc.prometheus.qiniu.io/graph#%5B%7B%22expr%22%3A%22sum%28delta%28apigate_request_duration_sec_hisogram_pilizeusd_count%7Bcode%21%3D%5C%22570%5C%22%2Ccode%3D~%5C%224..%7C5..%7C6..%5C%22%2Cpattern%3D~%5C%22.%2A%20%2Fv1%2Frtc%2F.%2A%5C%22%7D%5B1m%5D%29%29%20BY%20%28pattern%2C%20code%29%20%3E%200%22%2C%22tab%22%3A0%7D%5D"
			},
			{
				"status": "firing",
				"labels": {
					"alertname": "test2",
					"dir": "in",
					"monitor": "qiniu-bcgateway",
					"severity": "warning",
					"node": "vdn-gzgy-tel-1-2"
				},
				"annotations": {
					"description": "vdn-gzgy-tel-1-2 pili-streamd fd 3052 > 3000"
				},
				"startsAt": "2016-11-23T00:21:45.887+08:00",
				"endsAt": "0001-01-01T00:00:00Z",
				"generatorURL": "http://pili.bc.prometheus.qiniu.io/graph#%5B%7B%22expr%22%3A%22sum%28delta%28apigate_request_duration_sec_hisogram_pilizeusd_count%7Bcode%21%3D%5C%22570%5C%22%2Ccode%3D~%5C%224..%7C5..%7C6..%5C%22%2Cpattern%3D~%5C%22.%2A%20%2Fv1%2Frtc%2F.%2A%5C%22%7D%5B1m%5D%29%29%20BY%20%28pattern%2C%20code%29%20%3E%200%22%2C%22tab%22%3A0%7D%5D"
			}
		],
		"groupLabels": {
			"alertname": "pili_streamgate_bw_in_sharp"
		},
		"commonLabels": {
			"alertname": "pili_streamgate_bw_in_sharp",
			"dir": "in",
			"monitor": "qiniu-bcgateway",
			"severity": "critical"
		},
		"commonAnnotations": {
			"description": "[PILI] v1 StreamGate Traffic In Jitter 1960781Mbps > 300Mbps"
		},
		"externalURL": "http://bc49:9093",
		"version": "3",
		"groupKey": 6784042159559639689
	}'
	ret 200
	`)
}

type FakeNotifier struct {
	SendC chan<- Message
}

func (n *FakeNotifier) Name() string {
	return "FakeNotifier"
}

func (n *FakeNotifier) Notify(msg Message) (err error) {
	n.SendC <- msg
	return
}

type RevServerResult struct {
	timesRet  []int // TODO
	statusRet []AlertStatus
}

func fakeRevServer() func(xl *xlog.Logger, ast *assert.Assertions, revC <-chan Message, result *RevServerResult) {
	return func(xl *xlog.Logger, ast *assert.Assertions, revC <-chan Message, result *RevServerResult) {
		i := 0
		for {
			select {
			case msg := <-revC: //模拟Slack收到告警消息
				xl.Debug("index i is:", i)
				ast.NotEqual(i, len(result.statusRet), "fakeRevServer should not receive more alerts")
				ast.Equal(1, len(msg.Alerts), "just one alerts")
				// ast.Equal(result.timesRet[i], msg.Alerts[0].Times, "Times is not equal, i: ", i)
				ast.Equal(string(result.statusRet[i]), msg.Alerts[0].Status, "Status is not equal")
				xl.Debugf("=========== Recieve msg ===========: %v\n", msg)
				i++
			}
		}
	}
}

func TestAlertSilence(t *testing.T) {
	log.Println("TestAlertSilence Begin")
	defer log.Println("TestAlertSilence End")
	defer clearTestDB()
	xl := xlog.NewDummy()
	ast := assert.New(t)

	service := initService()
	// service.alertActiveMgr.ResendIntervalS = 1
	service.Config.AlertActiveCfg.ResendIntervalS = 1

	internet := make(chan Message)
	ns := NewNotifiers(NotifiersCfg{Default: "FakeNotifier"}, service.alertProfileMgr)
	ns.Append(&FakeNotifier{internet})
	service.notifiers = ns

	result := &RevServerResult{
		timesRet:  []int{1, 2, 3},
		statusRet: []AlertStatus{AlertFiring, AlertFiring, AlertFiring},
	}
	go fakeRevServer()(xl, ast, internet, result)

	transport := mockhttp.NewTransport()
	router := restrpc.Router{
		Mux: restrpc.NewServeMux(),
	}
	transport.ListenAndServe("localhost", router.Register(service))

	ctx := httptest.New(t)
	ctx.SetTransport(transport)
	ctx.Exec(`
	post http://localhost/prometheus/alerts
	header X-Reqid ` + xl.ReqId() + `
	json '{
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"labels": { "severity": "warning"},
				"annotations": { "description": "TestAlertUpdate" },
				"startsAt": "2016-11-24T00:21:45.887+08:00"
			}
		]
	}'
	ret 200
	`) // fakeRevServer 收到第一个请求

	time.Sleep(time.Millisecond * 1100) // 理论上 fakeRevServer 会在这期间再收到1个请求

	var alertId string
	for _, v := range service.alertActiveMgr.data {
		alertId = v.Id.Hex() // 正确的话，应该只有一个SimpleAert，k就是刚才发送的那个告警的AlertId
	}

	ctx.SetTransport(transport)
	ctx.Exec(`
	post http://localhost/alerts/ack?id=` + alertId + `
	ret 200
	`) // 模拟点击不在告警的按钮，fakeRevServer 不会收到请求
	ast.Equal(1, len(service.alertActiveMgr.data), "they shoud be equal")

}

// |    1s    |    1.1s   |     1.5s      |.   时间
// |----------|-----------|---------------|--- ‘-’ 表示1s
// |F---------|F----------|R--------------|R-- 发请求的时机 F 代表 Firing，R 代表 Resolved
// |1--------2|---------3-|4--------------|--- fakeRevServer 收到消息的时机
func TestAll(t *testing.T) {
	log.Println("TestAll Begin")
	defer log.Println("TestAll End")
	defer clearTestDB()
	xl := xlog.NewDummy()
	ast := assert.New(t)

	service := initService()
	service.alertActiveMgr.ResendIntervalS = 1
	service.alertActiveMgr.EmergenctIntervalS = 1

	internet := make(chan Message)
	ns := NewNotifiers(NotifiersCfg{Default: "FakeNotifier"}, service.alertProfileMgr)
	ns.Append(&FakeNotifier{internet})
	service.notifiers = ns

	result := &RevServerResult{
		statusRet: []AlertStatus{AlertFiring, AlertFiring, AlertFiring, AlertResolved},
	}
	go fakeRevServer()(xl, ast, internet, result)

	transport := mockhttp.NewTransport()
	router := restrpc.Router{
		Mux: restrpc.NewServeMux(),
	}
	transport.ListenAndServe("localhost", router.Register(service))

	ctx := httptest.New(t)
	ctx.SetTransport(transport)
	ctx.Exec(`
	post http://localhost/prometheus/alerts
	header X-Reqid ` + xl.ReqId() + `
	json '{
		"alerts": [
			{
				"status": "firing",
				"labels": { "severity": "warning", "alertname": "test"} ,
				"annotations": { "description": "TestAll" },
				"startsAt": "` + time.Now().Format(time.RFC3339) + `"
			}
		]
	}'
	ret 200
	`) // fakeRevServer 收到第一个请求

	time.Sleep(time.Millisecond * 1000)

	ctx.Exec(`
	post http://localhost/prometheus/alerts
	header X-Reqid ` + xl.ReqId() + `
	json '{
		"alerts": [
			{
				"status": "firing",
				"labels": { "severity": "warning", "alertname": "test"} ,
				"annotations": { "description": "TestAll" },
				"startsAt": "` + time.Now().Format(time.RFC3339) + `"
			}
		]
	}'
	ret 200
	`) //  理论上在告警从 firing 到 resolved 之前又收到 firing 的请求都会忽略掉

	time.Sleep(time.Millisecond * 1100) // 理论上 fakeRevServer 会再收到两个请求(来自resend)

	ctx.Exec(`
	post http://localhost/prometheus/alerts
	header X-Reqid ` + xl.ReqId() + `
	json '{
		"alerts": [
			{
				"status": "resolved",
				"labels": { "severity": "warning", "alertname": "test"} ,
				"annotations": { "description": "TestAll" },
				"startsAt": "` + time.Now().Format(time.RFC3339) + `"
			}
		]
	}'
	ret 200
	`) // fakeRevServer 会收到一个resolved请求

	time.Sleep(time.Millisecond * 1500) // 正常情况下，不应该会再收到请求

	ctx.Exec(`
	post http://localhost/prometheus/alerts
	header X-Reqid ` + xl.ReqId() + `
	json '{
		"alerts": [
			{
				"status": "resolved",
				"labels": { "severity": "warning", "alertname": "test"} ,
				"annotations": { "description": "TestAll" },
				"startsAt": "` + time.Now().Format(time.RFC3339) + `"
			}
		]
	}'
	ret 200
	`) // 测试即使发送了 resolved 的告警 fakeRevServer 也不会收到请求
	time.Sleep(time.Millisecond * 1500) // 正常情况下，不应该会再收到请求
}

func TestAnalyzerResult(t *testing.T) {
	log.Println("TestAnalyzerResult Begin")
	defer log.Println("TestAnalyzerResult End")
	ast := assert.New(t)
	opt := pmgo.Option{
		MgoAddr:     "127.0.0.1",
		MgoDB:       "test",
		MgoColl:     "sgForward",
		MgoMode:     "strong",
		MgoPoolSize: 1,
	}
	mgo, err := pmgo.New(opt)
	ast.NoError(err, "pmgo.New(opt) error")
	err = mgo.Coll().Insert(map[string]interface{}{
		"alertId": "1",
		"type":    "sgForward",
		"results": []map[string]interface{}{{
			"url":      "rtmp://test.com/1/1",
			"streamId": "1/1",
			"err":      "1",
			"len":      1,
		}},
	})
	ast.NoError(err, "mgo.Coll().Insert()")
	defer func() {
		mgo.Coll().DropCollection()
	}()

	cfg := analyzer.Config{
		Type:            "sgForward",
		AlertnameTagMap: map[string]string{"test": "test"},
		ResultMgoCfg:    opt,
	}

	sgForward := analyzer.NewSgForward(cfg)

	service := &Service{
		analyzers: map[string]Analyzer{cfg.Type: sgForward},
	}

	transport := mockhttp.NewTransport()
	router := restrpc.Router{
		Mux: restrpc.NewServeMux(),
	}

	transport.ListenAndServe("localhost", router.Register(service))
	ctx := httptest.New(t)
	ctx.SetTransport(transport)
	ctx.Exec(`
	get http://localhost/analyzer/result?alertId=1&type=sgForward
	ret 200
	json '{
		"results": [
			{
				"url" : "rtmp://test.com/1/1",
				"streamId" : "1/1",
				"err" : "1",
				"len" : 1
			}
		],
		"alertId" : "1",
		"type": "sgForward"
	}'`)
}

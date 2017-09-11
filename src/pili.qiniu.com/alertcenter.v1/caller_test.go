package alertcenter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/qiniu/log.v1"
	"github.com/qiniu/xlog.v1"
	"github.com/stretchr/testify/assert"
	"qbox.us/api/message"
	"qbox.us/oauth"
)

var (
	testCallerFilePath = "tmp"
	testCallerToken    = "TestCallerToken"
	testCallerUid      = 0
	testCallerOid      = "0"
)

func mockAccServer(ast *assert.Assertions) (s *httptest.Server) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		tok := oauth.Token{
			AccessToken: testCallerToken,
			Uid:         0,
		}
		b, err := json.Marshal(&tok)
		if !ast.NoError(err, "json.Masrshal(Token) fail") {
			return
		}
		w.Write(b)

		return
	})

	s = httptest.NewServer(mux)
	return
}

var receiceIdx int = 0

func mockMorseServer(ast *assert.Assertions) (s *httptest.Server) {

	result := &struct {
		msg []string
	}{
		msg: []string{"111111", "555555"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/notification/send/voicesms", func(w http.ResponseWriter, r *http.Request) {
		var req message.SendSmsIn
		err := json.NewDecoder(r.Body).Decode(&req)
		if !ast.NoError(err, "json.Decode err") {
			return
		}
		if receiceIdx >= len(result.msg) {
			ast.Fail("receice num out of limit, msg: ", req.Message)
			return
		}
		ast.Equal(result.msg[receiceIdx], req.Message)
		resp := message.SendOut{
			Oid: testCallerOid,
		}
		b, err := json.Marshal(&resp)
		if !ast.NoError(err, "json.Masrshal(SendOut) fail") {
			return
		}
		w.Write(b)

		receiceIdx++
		return
	})

	s = httptest.NewServer(mux)
	return
}

func TestCaller(t *testing.T) {
	log.Println("TestCaller Begin")
	defer log.Println("TestCaller End")
	xlog.SetOutputLevel(xlog.Ldebug)
	ast := assert.New(t)
	xl := xlog.NewDummy()
	alertname := "test"

	mockMorseServer := mockMorseServer(ast)

	cfg := CallerCfg{
		MorseHost:     mockMorseServer.URL,
		CallIntervals: 1,
		Alerts:        []string{alertname},
	}

	dutyMgr := &FakeDutyMgr{}

	caller := NewCaller(cfg, dutyMgr, nil)

	// // 换 Token
	// ast.Equal(caller.Token.AccessToken, testCallerToken, "AccessToken should be same")
	// ast.Equal(caller.Token.Uid, testCallerUid, "Uid should be same")

	// 发送语音电话
	as := []*Alert{{
		Description: "111111",
		Alertname:   alertname,
	}}
	msg := NewMessage(xl, as...)

	caller.Notify(msg)

	// 模拟2秒内发送同类告警, 不会打电话
	msg.Alerts[0].Description = "222222"
	caller.Notify(msg)

	caller.CallIntervals = 0

	// 模拟Silence时间内发送告警, 不会打电话
	caller.Silence(xl, 0, daySeconds-1)
	msg.Alerts[0].Description = "333333"
	caller.Notify(msg)

	// 重置Silence功能，Silence功能失效
	caller.UnsetSilence(xl)

	// 模拟从现在开始到8888秒后暂时关闭打电话功能，不会打电话
	caller.TempClose(xl, 8888)
	msg.Alerts[0].Description = "444444"
	caller.Notify(msg)

	// 重置暂时关闭打电话功能的行为
	caller.UnsetTempClose(xl)

	// 模拟Close时间内发送告警, 会打电话
	msg.Alerts[0].Description = "555555"
	caller.Notify(msg)

	os.Remove(testCallerFilePath)
	ast.Equal(2, receiceIdx, "just receive two calls")
}

// func TestRealCall(t *testing.T) {
// 	caller := NewCaller(
// 		CallerCfg{
// 			ClientId:  "5989673943c8ce7fa8000223",
// 			MorseHost: "http://10.200.20.22:9015",
// 		},
// 		&FakeDutyMgr{},
// 		func(mw MessageWrapper) {
// 			log.Println(mw.Message.Alerts[0].Description)
// 		},
// 	)
// 	caller.SendVoiceSms(xlog.NewDummy(), "testtest")
// }

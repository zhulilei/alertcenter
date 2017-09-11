package alertcenter

import (
	"os"
	"testing"
	"time"

	"github.com/qiniu/log.v1"
	"github.com/qiniu/xlog.v1"
	"github.com/stretchr/testify/assert"
	"labix.org/v2/mgo/bson"
)

func TestBackup(t *testing.T) {
	log.Println("TestBackup Begin")
	defer log.Println("TestBackup End")
	xl := xlog.NewDummy()
	xlog.SetOutputLevel(xlog.Ldebug)
	ast := assert.New(t)

	aas := AlertActiveMgr{
		data: make(map[string]*AlertActive),
		AlertActiveCfg: &AlertActiveCfg{
			BackupFile: "tmp",
		},
	}
	aa := AlertActive{
		Alert: &Alert{
			Id:          bson.NewObjectId(),
			Status:      AlertFiring,
			Description: "test",
			StartsAt:    time.Now(),
			Severity:    SeverityWarning,
		},
		Tick: time.NewTicker(1 * time.Second),
		xl:   xl,
	}
	aas.data[aa.Alert.Key] = &aa
	aas.Backup(xl)

	sendC := make(chan Message)
	done := make(chan struct{})
	NewAlertActiveMgr(func(msg Message) {
		sendC <- msg
	}, AlertActiveCfg{1, 1, "tmp", 10000}, nil)

	go func() {
		msg := <-sendC
		ast.Equal(1, len(msg.Alerts))
		done <- struct{}{}
	}()

	<-done
	os.Remove("tmp")
}

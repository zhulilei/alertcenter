package alertcenter

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/qiniu/xlog.v1"
)

func save(xl *xlog.Logger, v interface{}, filePath string) (err error) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}

	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		xl.Errorf("os.OpenFile(%v) err: %v", file, err)
		return
	}
	defer file.Close()
	file.Write(data)
	return
}

func load(xl *xlog.Logger, v interface{}, filePath string) (err error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		xl.Errorf("ioutil.ReadFile(%v) err: %+v", filePath, err)
		return
	}
	err = json.Unmarshal(data, v)
	if err != nil {
		xl.Errorf("json.Unmarshal(data, &v) data: %v err: %v", string(data), err)
		return
	}
	return
}

var zoneStr = time.Now().Format("-0700")

// 支持以下格式
// 1. 时间戳(秒)
// 2. 20060102
// 3. 20060102/00:00
// 4. 2006-01-02
// 5. 2006-01-02/00:00
// 6. 20060102150405
// 7. 2006-01-02T15:04:05Z07:00 (RFC3339)
// 8. duration-ago, 如 5h-ago
func TimeOf(str string) (t time.Time, ok bool) {
	var err error
	if strings.HasPrefix(str, "20") {
		str2 := str + zoneStr
		try := func(layout string) bool {
			t, err = time.Parse(layout+"-0700", str2)
			return err == nil
		}
		if try("20060102") || try("20060102/15:04") || try("2006-01-02") ||
			try("2006-01-02/15:04") || try("20060102150405") {
			ok = true
			return
		} else {
			t, err = time.Parse(time.RFC3339, str)
			ok = err == nil
			return
		}
	} else if strings.HasSuffix(str, "-ago") {
		str = strings.TrimSuffix(str, "-ago")
		dur, err := time.ParseDuration(str)
		if err != nil {
			ok = false
			return
		}
		return time.Now().Add(-dur), true

	} else {
		var n uint64
		if n, err = strconv.ParseUint(str, 10, 63); err != nil {
			return
		}
		t = time.Unix(int64(n), 0)
		ok = true
		return
	}
}

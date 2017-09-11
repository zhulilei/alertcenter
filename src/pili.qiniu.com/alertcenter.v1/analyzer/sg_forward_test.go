package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	pmgo "pili.qiniu.com/mgo"
)

type ForwardRecord struct {
	Url      string `bson:"url" json:"url"`
	StreamId string `bson:"streamId" json:"streamId"`
	Err      string `bson:"err" json:"err"`
	Tag      string `bson:"tag" json:"tag"`
}

func Init() (s *SgForward) {
	mgoCfg := pmgo.Option{
		MgoAddr:     "127.0.0.1",
		MgoDB:       "test",
		MgoColl:     "sgForward",
		MgoMode:     "strong",
		MgoPoolSize: 1,
	}
	cfg := Config{
		AlertnameTagMap: map[string]string{"cdnTest": "cdn"},
		SrcMgoCfg:       mgoCfg,
		Type:            "sgForward",
		Limit:           10,
	}
	mgoCfg.MgoColl = "result"
	cfg.ResultMgoCfg = mgoCfg
	s = NewSgForward(cfg)
	for i := 0; i < 4; i++ {
		s.srcMgo.Coll().Insert(ForwardRecord{
			Url:      "rtmp://test.com/1/1",
			StreamId: "1/1",
			Err:      "11",
			Tag:      "cdn",
		})
	}
	for i := 0; i < 2; i++ {
		s.srcMgo.Coll().Insert(ForwardRecord{
			Url:      "rtmp://test.com/2/2",
			StreamId: "2/2",
			Err:      "22",
			Tag:      "cdn",
		})
	}
	s.srcMgo.Coll().Insert(ForwardRecord{
		Url:      "rtmp://test.com/3/3",
		StreamId: "3/3",
		Err:      "33",
		Tag:      "cdn",
	})
	for i := 0; i < 2; i++ {
		s.srcMgo.Coll().Insert(ForwardRecord{
			Url:      "rtmp://test.com/4/4",
			StreamId: "4/4",
			Err:      "44",
			Tag:      "customForward",
		})
	}
	s.srcMgo.Coll().Insert(ForwardRecord{
		Url:      "rtmp://test.com/5/5",
		StreamId: "5/5",
		Err:      "55",
		Tag:      "segmenter",
	})
	return
}

func TestSgForward(t *testing.T) {
	ast := assert.New(t)
	s := Init()
	defer AfterRun(s)
	err := s.Run("cdnTest", "alertId")
	ast.NoError(err, "s.Run()")

	r, err := s.GetResults("alertId")
	ast.NoError(err, "s.GetResults(alertId)")

	var result Result
	result = r.(Result)
	ast.Equal(3, len(result.Results), "the length of result.Results is not same")
	ast.Equal(4, result.Results[0].Len, "result.Results[0] should be 4")
	ast.Equal(2, result.Results[1].Len, "result.Results[1] should be 2")
	ast.Equal(1, result.Results[2].Len, "result.Results[2] should be 1")
}

func AfterRun(s *SgForward) {
	s.srcMgo.Coll().DropCollection()
	s.resultMgo.Coll().DropCollection()
}

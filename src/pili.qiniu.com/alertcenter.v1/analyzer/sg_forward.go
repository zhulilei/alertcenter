package analyzer

import (
	"github.com/qiniu/log.v1"
	"labix.org/v2/mgo"

	pmgo "pili.qiniu.com/mgo"
)

type SgForward struct {
	Config

	srcMgo    pmgo.Mongo
	resultMgo pmgo.Mongo
}

func NewSgForward(cfg Config) *SgForward {
	srcMgo, err := pmgo.New(cfg.SrcMgoCfg)
	if err != nil {
		log.Panic("SgForward: NewSgForward pmgo.New(cfg.SrcMgoCfg) err:", err)
		return nil
	}

	resultMgo, err := pmgo.New(cfg.ResultMgoCfg)
	if err != nil {
		log.Panic("SgForward: NewSgForward pmgo.New(cfg.SrcMgoCfg) err:", err)
		return nil
	}

	err = resultMgo.Coll().EnsureIndex(mgo.Index{
		Key:    []string{"alertId", "type"},
		Unique: true,
	})
	if err != nil {
		log.Panic("mgo.EnsureIndex(alertId) failed:", err)
		return nil
	}

	return &SgForward{
		Config:    cfg,
		srcMgo:    srcMgo,
		resultMgo: resultMgo,
	}
}

type M map[string]interface{}

type Result struct {
	Results []struct {
		Url      string `json:"url" bson:"url"`
		StreamId string `json:"streamId" bson:"streamId"`
		Err      string `json:"err" bson:"err"`
		Len      int    `json:"len" bson:"len"`
	} `json:"results" bson:"results"`
	AlertId   string `json:"alertId" bson:"alertId"`
	Type      string `json:"type" bson:"type"`
	Alertname string `json:"alertname" bson:"alertname"`
}

func (s *SgForward) Run(alertname string, alertId string) (err error) {
	pipe := s.srcMgo.Coll().Pipe([]M{
		{"$match": M{"tag": s.AlertnameTagMap[alertname]}},
		{
			"$group": M{
				"_id": M{
					"url":      "$url",
					"streamId": "$streamId",
					"err":      "$err",
				},
				"len": M{"$sum": 1},
			},
		},
		{"$sort": M{"len": -1}},
		{"$limit": s.Limit},
		{
			"$group": M{
				"_id": M{},
				"results": M{
					"$push": M{
						"url":      "$_id.url",
						"streamId": "$_id.streamId",
						"err":      "$_id.err",
						"len":      "$len",
					},
				},
			},
		},
		{"$project": M{"_id": 0, "results": 1, "alertId": M{"$literal": alertId}, "type": M{"$literal": s.Type}, "alertname": M{"$literal": alertname}}},
	})
	var results []interface{}
	iter := pipe.Iter()
	err = iter.All(&results)
	if err != nil {
		log.Println("iter.All(&results) error:", err)
		return err
	}
	err = s.resultMgo.Coll().Insert(results...)
	if err != nil {
		log.Println("s.resultMgo.Coll().Insert(results...) error:", err)
		return err
	}
	return
}

func (s *SgForward) ShouldRun(alertname string) bool {
	for k := range s.AlertnameTagMap {
		if k == alertname {
			return true
		}
	}
	return false
}

func (s *SgForward) GetResults(alertId string) (interface{}, error) {
	var result Result

	err := s.resultMgo.Coll().Find(M{"alertId": alertId, "type": s.Type}).One(&result)
	if err != nil {
		log.Errorf("s.resultMgo.Coll().Find(..).One(..) err: %v", err)
		return nil, err
	}
	return result, nil
}

func (s *SgForward) GetType() string {
	return s.Type
}

package alertcenter

import (
	"encoding/hex"
	"hash/fnv"
	"sort"
	"time"

	"github.com/qiniu/xlog.v1"
	"labix.org/v2/mgo/bson"
)

const (
	AlertNameLabel = "alertname"
	DescLabel      = "description"
	SeverityLabel  = "severity"
)

type M bson.M

type KV map[string]string

// Alert holds one alert for notification templates.
type AlertForDefault struct {
	Alertname    string            `json:"alertname"`
	Desc         string            `json:"desc"`
	Status       AlertStatus       `json:"status"`
	Severity     Severity          `json:"severity"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Labels       map[string]string `json:"labels"`
	NeedHandle   bool              `json:"needHandle"`
}

// for Prometheus
type AlertForProm struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
}

type Severity string

const (
	SeverityP0 Severity = "P0"
	SeverityP1 Severity = "P1"

	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
	SeveritySuccess  Severity = "success"
)

func (s Severity) toP() Severity {
	switch s {
	case SeverityCritical:
		return SeverityP0
	case SeverityWarning:
		return SeverityP1
	default:
		return SeverityP1
	}
}

type AlertStatus string

const (
	AlertFiring   AlertStatus = "firing"
	AlertResolved AlertStatus = "resolved"
	AlertAcked    AlertStatus = "acked"
)

type Ack struct {
	Comment  string    `json:"comment" bson:"comment"`
	Time     time.Time `json:"time" bson:"time"`
	Username string    `json:"username" bson:"username"`
}

// =====================================================================

type Alert struct {
	Id            bson.ObjectId     `bson:"_id" json:"id"`
	Key           string            `json:"key" bson:"key"`
	Status        AlertStatus       `json:"status" bson:"status"`
	Description   string            `json:"desc" bson:"desc"`
	StartsAt      time.Time         `json:"startsAt" bson:"startsAt"`
	EndsAt        time.Time         `json:"endsAt" bson:"endsAt"`
	Severity      Severity          `json:"severity" bson:"severity"`
	Alertname     string            `json:"alertname" bson:"alertname"`
	GeneratorURL  string            `json:"generatorUrl" bson:"generatorUrl"`
	NeedHandle    bool              `json:"needHandle" bson:"needHandle"`
	IsEmergent    bool              `json:"isEmergent" bson:"isEmergent"`
	Labels        map[string]string `json:"labels" bson:"labels"`
	Acks          []Ack             `json:"comments" bson:"acks"` // TODO
	AnalyzerTypes []string          `json:"-" bson:"-"`
}

func NewAlert(alert interface{}) (newAlert *Alert) {
	switch a := alert.(type) {
	case *AlertForDefault:
		newAlert = &Alert{
			Status:        a.Status,
			Description:   a.Desc,
			StartsAt:      a.StartsAt,
			EndsAt:        a.EndsAt,
			Severity:      a.Severity.toP(),
			Alertname:     a.Alertname,
			GeneratorURL:  a.GeneratorURL,
			NeedHandle:    a.NeedHandle,
			Labels:        a.Labels,
			AnalyzerTypes: []string{},
		}
	case *AlertForProm:
		newAlert = &Alert{
			Status:        AlertStatus(a.Status),
			Description:   a.Annotations[DescLabel],
			StartsAt:      a.StartsAt,
			EndsAt:        a.EndsAt,
			Severity:      Severity(a.Labels[SeverityLabel]).toP(),
			Alertname:     a.Labels[AlertNameLabel],
			GeneratorURL:  a.GeneratorURL,
			NeedHandle:    true,
			AnalyzerTypes: []string{},
		}
		delete(a.Labels, AlertNameLabel)
		delete(a.Labels, SeverityLabel)
		newAlert.Labels = a.Labels
	default:
		return
	}
	newAlert.Key = newAlert.CalKey()
	return
}

func (alert *Alert) CalKey() string {
	h := fnv.New32a()

	h.Write([]byte(alert.Alertname))
	h.Write([]byte(alert.Severity))

	values := []string{}
	for k, v := range alert.Labels {
		values = append(values, k+v)
	}
	sort.Strings(values)

	for i := range values {
		h.Write([]byte(values[i]))
	}

	return hex.EncodeToString(h.Sum(nil))
}

func (s *Alert) Spawn() Alert {
	return *s
}

// =====================================================================

type ByStartsAt []*Alert

func (s ByStartsAt) Len() int {
	return len(s)
}

func (s ByStartsAt) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s ByStartsAt) Less(i, j int) bool {
	return s[i].StartsAt.Before(s[j].StartsAt)
}

// =====================================================================

type Message struct {
	xl     *xlog.Logger
	Alerts []*Alert
}

func NewMessage(xl *xlog.Logger, alerts ...*Alert) Message {
	return Message{xl, alerts}
}

// =====================================================================

type Analyzer interface {
	ShouldRun(alertnamne string) bool
	Run(alertnamne string, alertId string) error
	GetResults(AlertId string) (interface{}, error)
	GetType() string
}

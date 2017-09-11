package alertcenter

import (
	"github.com/qiniu/xlog.v1"
)

type Action interface {
	Do(xl *xlog.Logger, as []*Alert) []*Alert
}

type Actions []Action

func NewActions(actions ...Action) Actions {
	return Actions(actions)
}

func (as *Actions) Append(n Action) {
	*as = append(*as, n)
}

func (as Actions) Do(xl *xlog.Logger, alerts []*Alert) []*Alert {
	for _, action := range as {
		alerts = action.Do(xl, alerts)
	}
	return alerts
}

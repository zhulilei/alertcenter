package alertcenter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenAlertKey(t *testing.T) {
	alert1 := NewAlert(&AlertForProm{
		Labels: map[string]string{
			"a": "a",
			"b": "b",
			"c": "c",
		},
	})
	alert2 := NewAlert(&AlertForProm{
		Labels: map[string]string{
			"c": "c",
			"a": "a",
			"b": "b",
		},
	})
	assert.Equal(t, alert1.Key, alert2.Key, "they should be equal")
}

package events

import (
  "github.com/stretchr/testify/assert"
  "testing"
)

func TestValidateURL(t *testing.T) {
  w := &webhookAction{
    es: nil,
    spec: &webhookActionInfo{
      URL: "badurl",
    },
  }

  _, _, err := w.validateURL()
  assert.Error(t, err)

  w.spec.URL = "https://google.com"
  _, _, err = w.validateURL()
  assert.NoError(t, err)
}

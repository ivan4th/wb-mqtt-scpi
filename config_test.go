package main

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
)

var sampleConfigStr = `
ports:
  - name: somedev
    title: Some Device
    port: /dev/ttyS0
    idsubstring: some_dev_id
    controls:
    - name: current1
      title: Current 1
      units: A
      scpiname: CURR
      type: current
      writable: true
    - name: mcurrent1
      title: Measured Current 1
      units: A
      scpiname: MEAS:CURR
      type: current
`

var sampleParsedConfig = &ScpiConfig{
	Ports: []*ScpiPortConfig{
		{
			Name:        "somedev",
			Title:       "Some Device",
			Port:        "/dev/ttyS0",
			IdSubstring: "some_dev_id",
			Controls: []*ScpiControl{
				{
					Name:     "current1",
					Title:    "Current 1",
					Units:    "A",
					ScpiName: "CURR",
					Type:     "current",
					Writable: true,
				},
				{
					Name:     "mcurrent1",
					Title:    "Measured Current 1",
					Units:    "A",
					ScpiName: "MEAS:CURR",
					Type:     "current",
				},
			},
		},
	},
}

func TestParseConfig(t *testing.T) {
	actualConfig, err := ParseConfig([]byte(sampleConfigStr))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if !reflect.DeepEqual(actualConfig, sampleParsedConfig) {
		t.Errorf("config mismatch: got:\n%s\nexpected:\n%s",
			spew.Sdump(actualConfig), spew.Sdump(sampleParsedConfig))
	}
}

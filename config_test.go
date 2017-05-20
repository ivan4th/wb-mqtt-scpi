package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
)

type sampleParameterSpec struct {
	Controls   []*ControlConfig
	SampleName string
}

var _ ParameterSpec = &sampleParameterSpec{}

func (spec *sampleParameterSpec) ListControls() []*ControlConfig {
	return spec.Controls
}

func (spec *sampleParameterSpec) ShouldPoll() bool {
	for _, c := range spec.Controls {
		if c.ShouldPoll() {
			return true
		}
	}
	return false
}

func (spec *sampleParameterSpec) Settable() bool {
	for _, c := range spec.Controls {
		if c.Writable {
			return true
		}
	}
	return false
}

func (spec *sampleParameterSpec) Validate() error {
	for _, control := range spec.Controls {
		if err := control.Validate(); err != nil {
			return err
		}
	}
	if spec.SampleName == "" {
		return errors.New("SampleName not specified")
	}
	if spec.SampleName == "XXX" {
		return errors.New("SampleName XXX is prohibited")
	}
	return nil
}

var sampleConfigStr = `
ports:
- name: somedev
  title: Some Device
  port: /dev/ttyS0
  protocol: sample
  idsubstring: some_dev_id
  commanddelayms: 42
  setup:
  - command: :SYST:REM
  - command: WHATEVER
    response: ORLY
  parameters:
  - samplename: CURRVOLT
    controls:
    - name: current1
    - name: voltage1
  - samplename: CURR
    controls:
    - name: current1
      title: Current 1
      units: A
      type: current
      writable: true
  - samplename: VOLT
    controls:
    - name: voltage1
      title: Voltage 1
      units: V
      type: voltage
      writable: true
  - samplename: MEAS:CURR
    controls:
    - name: mcurrent1
      title: Measured Current 1
      units: A
      type: current
  - samplename: MODE
    controls:
    - name: mode
      title: Mode
      type: text
      enum: *sampleEnum
enums:
- &sampleEnum
  0: "x"
  1: "y"
  2: "z"
`

var sampleParsedConfig = &DriverConfig{
	Ports: []*PortConfig{
		{
			PortSettings: &PortSettings{
				Name:           "somedev",
				Title:          "Some Device",
				Port:           "/dev/ttyS0",
				IdSubstring:    "some_dev_id",
				CommandDelayMs: 42,
				Protocol:       "sample",
				Setup: []*SetupItem{
					{
						Command: ":SYST:REM",
					},
					{
						Command:  "WHATEVER",
						Response: "ORLY",
					},
				},
			},
			Parameters: []ParameterSpec{
				&sampleParameterSpec{
					Controls: []*ControlConfig{
						{
							Name: "current1",
						},
						{
							Name: "voltage1",
						},
					},
					SampleName: "CURRVOLT",
				},
				&sampleParameterSpec{
					Controls: []*ControlConfig{
						{
							Name:     "current1",
							Title:    "Current 1",
							Units:    "A",
							Type:     "current",
							Writable: true,
						},
					},
					SampleName: "CURR",
				},
				&sampleParameterSpec{
					Controls: []*ControlConfig{
						{
							Name:     "voltage1",
							Title:    "Voltage 1",
							Units:    "V",
							Type:     "voltage",
							Writable: true,
						},
					},
					SampleName: "VOLT",
				},
				&sampleParameterSpec{
					Controls: []*ControlConfig{
						{
							Name:  "mcurrent1",
							Title: "Measured Current 1",
							Units: "A",
							Type:  "current",
						},
					},
					SampleName: "MEAS:CURR",
				},
				&sampleParameterSpec{
					Controls: []*ControlConfig{
						{
							Name:  "mode",
							Title: "Mode",
							Type:  "text",
							Enum: map[int]string{
								0: "x",
								1: "y",
								2: "z",
							},
						},
					},
					SampleName: "MODE",
				},
			},
		},
	},
}

func TestParseConfig(t *testing.T) {
	RegisterProtocolConfig("sample", &sampleParameterSpec{})
	actualConfig, err := ParseDriverConfig([]byte(sampleConfigStr))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if !reflect.DeepEqual(actualConfig, sampleParsedConfig) {
		t.Errorf("config mismatch: got:\n%s\nexpected:\n%s",
			spew.Sdump(actualConfig), spew.Sdump(sampleParsedConfig))
	}
}

func TestGetControls(t *testing.T) {
	expectedControls := []*ControlConfig{
		{
			Name:     "current1",
			Title:    "Current 1",
			Units:    "A",
			Type:     "current",
			Writable: true,
		},
		{
			Name:     "voltage1",
			Title:    "Voltage 1",
			Units:    "V",
			Type:     "voltage",
			Writable: true,
		},
		{
			Name:  "mcurrent1",
			Title: "Measured Current 1",
			Units: "A",
			Type:  "current",
		},
		{
			Name:  "mode",
			Title: "Mode",
			Type:  "text",
			Enum: map[int]string{
				0: "x",
				1: "y",
				2: "z",
			},
		},
	}
	actualControls, paramSetMap, err := sampleParsedConfig.Ports[0].GetControls()
	if err != nil {
		t.Fatalf("GetControls() failed: %v", err)
	}
	if !reflect.DeepEqual(actualControls, expectedControls) {
		t.Errorf("controls mismatch: got:\n%s\nexpected:\n%s",
			spew.Sdump(actualControls), spew.Sdump(expectedControls))
	}
	if len(paramSetMap) != 2 {
		t.Errorf("invalid paramSetMap length: %v", paramSetMap)
	}
	if paramSetMap["current1"] != sampleParsedConfig.Ports[0].Parameters[1] {
		t.Errorf("invalid paramSetMap entry for current1: %s", spew.Sdump(paramSetMap["current1"]))
	}
	if paramSetMap["voltage1"] != sampleParsedConfig.Ports[0].Parameters[2] {
		t.Errorf("invalid paramSetMap entry for voltage1: %s", spew.Sdump(paramSetMap["voltage1"]))
	}
}

func TestValidationFailures(t *testing.T) {
	RegisterProtocolConfig("sample", &sampleParameterSpec{})
	for _, testCase := range []struct{ old, new, errStr string }{
		{"samplename: CURRVOLT", "#", "SampleName not specified"},
		{"samplename: CURRVOLT", "samplename: XXX", "SampleName XXX is prohibited"},
		{"name: mcurrent1", "#", "got control without name"},
		// TODO: should validate merged controls
		// {"type: voltage", "#", `no type specified for control "voltage1"`},
	} {
		_, err := ParseDriverConfig([]byte(strings.Replace(sampleConfigStr, testCase.old, testCase.new, -1)))
		switch {
		case err == nil:
			t.Errorf("replacement %q -> %q didn't cause an error", testCase.old, testCase.new)
		case err.Error() != testCase.errStr:
			t.Errorf("bad error after replacing %q -> %q: %q (expected %q)", testCase.old, testCase.new, err, testCase.errStr)
		}
	}
}

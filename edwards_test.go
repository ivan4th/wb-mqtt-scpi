package main

import (
	"reflect"
	"testing"
)

var edwardsConfig = `
ports:
- name: edwards
  title: Edwards
  port: someport
  protocol: edwards
  idsubstring: TIC200
  parameters:
  # parameter 0
  - oid: 902
    read: "?V"
    controls:
    - name: turboState
      title: Turbo State
      type: text
    - name: backingState
      title: Backing State
      type: text
    - name: gaugeState1
      title: Gauge State 1
      type: text
    - name: gaugeState2
      title: Gauge State 2
      type: text
    - name: gaugeState3
      title: Gauge State 3
      type: text
    - name: relayState1
      title: Relay State 1
      type: text
    - name: relayState2
      title: Relay State 2
      type: text
    - name: relayState3
      title: Relay State 3
      type: text
    - name: ticStatusAlertId
      title: TIC Status - Alert ID
      type: text
    - name: ticStatusPriority
      title: TIC Status - Priority
      type: text
  # parameter 1
  - oid: 905
    read: "?S"
    write: "!S"
    controls:
    - name: readStartFailTime
      title: Read Start Fail Time
      type: value
      units: min
    - name: droopFailTime
      title: Droop Fail Time
      type: value
      units: min
  # parameter 2
  - oid: 916
    write: "!C"
    sub: 0
    controls:
    - name: relay1Off
      title: Relay 1 Off
      type: pushbutton
  # parameter 3
  - oid: 916
    write: "!C"
    sub: 1
    controls:
    - name: relay1On
      title: Relay 1 On
      type: pushbutton
  # parameter 4
  - oid: 904
    sub: 21
    read: "?S"
    write: "!S"
    controls:
    - name: pumpStartDelay
      title: Pump Start Delay
      type: value
      units: min
`

type edwardsTester struct {
	t          *testing.T
	commander  *fakeCommander
	protocol   Protocol
	portConfig *PortConfig
}

func newEdwardsTester(t *testing.T) *edwardsTester {
	config, err := ParseDriverConfig([]byte(edwardsConfig))
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}
	commander := newFakeCommander(t)
	protocol, err := CreateProtocol(config.Ports[0])
	if err != nil {
		t.Fatalf("CreateProtocol(): %v", err)
	}
	commander.Connect()
	<-commander.Ready()
	return &edwardsTester{t, commander, protocol, config.Ports[0]}
}

func (et *edwardsTester) param(paramIndex int) Parameter {
	param, err := et.protocol.Parameter(et.portConfig.Parameters[paramIndex])
	if err != nil {
		et.t.Fatalf("Parameter(): %v", err)
	}
	return param
}

func (et *edwardsTester) verifyQuery(paramIndex int, expectedResult map[string]interface{}) {
	param := et.param(paramIndex)
	r := make(map[string]interface{})
	if err := param.Query(et.commander, func(name string, value interface{}) {
		r[name] = value
	}); err != nil {
		et.t.Fatalf("Query(): %v", err)
	}
	if !reflect.DeepEqual(r, expectedResult) {
		et.t.Errorf("bad query result: %#v (expected: %#v)", r, expectedResult)
	}
}

func (et *edwardsTester) verifySet(paramIndex int, controlName string, value interface{}) {
	param := et.param(paramIndex)
	if err := param.Set(et.commander, controlName, value); err != nil {
		et.t.Fatalf("Set(): %v", err)
	}
}

func (et *edwardsTester) verifyQueryError(paramIndex int, errStr string) {
	param := et.param(paramIndex)
	err := param.Query(et.commander, func(string, interface{}) {
		et.t.Errorf("unexpected query handler call")
	})
	if err == nil {
		et.t.Errorf("no error received for param %d", paramIndex)
	}
	if err.Error() != errStr {
		et.t.Errorf("unexpected error string %q (expected %q)", err, errStr)
	}
}

func (et *edwardsTester) verifySetError(paramIndex int, controlName string, value interface{}, errStr string) {
	param := et.param(paramIndex)
	err := param.Set(et.commander, controlName, value)
	if err == nil {
		et.t.Errorf("no error received for param %d", paramIndex)
	}
	if err.Error() != errStr {
		et.t.Errorf("unexpected error string %q (expected %q)", err, errStr)
	}
}

func TestEdwardsIdentify(t *testing.T) {
	et := newEdwardsTester(t)
	// don't know what zero byte is exactly doing there
	et.commander.enqueue("?S902", "=S902 TIC200;D39700640S;150326362\x00;5.0")
	id, err := et.protocol.Identify(et.commander)
	if err != nil {
		t.Fatalf("Identify(): %v", err)
	}
	if id != "TIC200/D39700640S/150326362/5.0" {
		t.Errorf("Bad id %q", id)
	}
}

func TestEdwardsQuery(t *testing.T) {
	et := newEdwardsTester(t)
	// T;B;G1;G2;G3;R1;R2;R3;Alert ID; priority
	// T – turbo state
	// B – backing state
	// G – gauge state
	// R – relay state
	et.commander.enqueue("?V902", "=V902 0;1;0;0;1;0;0;1;0;0")
	et.verifyQuery(0, map[string]interface{}{
		"turboState":        "0",
		"backingState":      "1",
		"gaugeState1":       "0",
		"gaugeState2":       "0",
		"gaugeState3":       "1",
		"relayState1":       "0",
		"relayState2":       "0",
		"relayState3":       "1",
		"ticStatusAlertId":  "0",
		"ticStatusPriority": "0",
	})
	et.commander.enqueue("?S905", "=S905 8;8")
	et.verifyQuery(1, map[string]interface{}{
		"readStartFailTime": "8",
		"droopFailTime":     "8",
	})
	et.commander.enqueue("?S904 21", "=S904 21;42")
	et.verifyQuery(4, map[string]interface{}{
		"pumpStartDelay": "42",
	})
}

func TestEdwardsSet(t *testing.T) {
	et := newEdwardsTester(t)
	et.commander.enqueue("!C916 0", "*C916 0")
	et.verifySet(2, "relay1Off", 1)
	et.commander.enqueue("!C916 1", "*C916 0")
	et.verifySet(3, "relay1On", 1)
	et.commander.enqueue("?S905", "=S905 5;6", "!S905 5;8", "*S905 0")
	et.verifySet(1, "droopFailTime", 8)
	et.commander.enqueue("!S904 21;42", "*S904 0")
	et.verifySet(4, "pumpStartDelay", 42)
}

func TestErrorResponses(t *testing.T) {
	et := newEdwardsTester(t)
	et.commander.enqueue("?V902", "*V902 8")
	et.verifyQueryError(0, "device error: Operation took too long")
	et.commander.enqueue("!C916 0", "*C916 7")
	et.verifySetError(2, "relay1Off", 1, "device error: EEPROM read or write error")
}

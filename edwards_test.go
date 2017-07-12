package main

import "testing"

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

func TestEdwardsIdentify(t *testing.T) {
	pt := newProtocolTester(t, edwardsConfig)
	// don't know what zero byte is exactly doing there
	pt.commander.enqueue("?S902", "=S902 TIC200;D39700640S;150326362\x00;5.0")
	id, err := pt.protocol.Identify(pt.commander)
	if err != nil {
		t.Fatalf("Identify(): %v", err)
	}
	if id != "TIC200/D39700640S/150326362/5.0" {
		t.Errorf("Bad id %q", id)
	}
}

func TestEdwardsQuery(t *testing.T) {
	pt := newProtocolTester(t, edwardsConfig)
	// T;B;G1;G2;G3;R1;R2;R3;Alert ID; priority
	// T – turbo state
	// B – backing state
	// G – gauge state
	// R – relay state
	pt.commander.enqueue("?V902", "=V902 0;1;0;0;1;0;0;1;0;0")
	pt.verifyQuery(0, map[string]interface{}{
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
	pt.commander.enqueue("?S905", "=S905 8;8")
	pt.verifyQuery(1, map[string]interface{}{
		"readStartFailTime": "8",
		"droopFailTime":     "8",
	})
	pt.commander.enqueue("?S904 21", "=S904 21;42")
	pt.verifyQuery(4, map[string]interface{}{
		"pumpStartDelay": "42",
	})
}

func TestEdwardsSet(t *testing.T) {
	pt := newProtocolTester(t, edwardsConfig)
	pt.commander.enqueue("!C916 0", "*C916 0")
	pt.verifySet(2, "relay1Off", 1)
	pt.commander.enqueue("!C916 1", "*C916 0")
	pt.verifySet(3, "relay1On", 1)
	pt.commander.enqueue("?S905", "=S905 5;6", "!S905 5;8", "*S905 0")
	pt.verifySet(1, "droopFailTime", 8)
	pt.commander.enqueue("!S904 21;42", "*S904 0")
	pt.verifySet(4, "pumpStartDelay", 42)
}

func TestErrorResponses(t *testing.T) {
	pt := newProtocolTester(t, edwardsConfig)
	pt.commander.enqueue("?V902", "*V902 8")
	pt.verifyQueryError(0, "device error: Operation took too long")
	pt.commander.enqueue("!C916 0", "*C916 7")
	pt.verifySetError(2, "relay1Off", 1, "device error: EEPROM read or write error")
}

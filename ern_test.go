package main

import "testing"

// id:      'Z44NN\r' --> '!44N>\xc8\xcf\xd1-1200-220\xc2/7\xea\xc2-1\xc0'
// (converted to UTF-8: !44N>ИПС-1200-220В/7кВ-1А)
// measure: 'Z4441\r' --> '!444>1+07018+000,012'
// disable: 'Z441D\r' --> '!441'
// enable:  'Z441E\r' --> '!441'
var ernConfig = `
ports:
- name: ern
  title: ern
  port: someport
  protocol: ern
  idsubstring: "-1200-220"
  lineending: cr
  address: 44
  parameters:
  - command: "41"
    resplen: 20
    respskip: 1
    controls:
    - name: U
      units: V
      type: value
    - name: I
      units: A
      type: value
  - command: "1E"
    controls:
    - name: On
      type: pushbutton
      writable: true
  - command: "1D"
    controls:
    - name: Off
      type: pushbutton
      writable: true
`

func TestErnIdentify(t *testing.T) {
	pt := newProtocolTester(t, ernConfig)
	pt.commander.enqueue("Z44NN", "!44N>\xc8\xcf\xd1-1200-220\xc2/7\xea\xc2-1\xc0")
	id, err := pt.protocol.Identify(pt.commander)
	if err != nil {
		t.Fatalf("Identify(): %v", err)
	}
	if id != "ИПС-1200-220В/7кВ-1А" {
		t.Errorf("Bad id %q", id)
	}
}

func TestErnQuery(t *testing.T) {
	pt := newProtocolTester(t, ernConfig)
	pt.commander.enqueue(20, "Z4441", "!444>1+07018+000,012")
	pt.verifyQuery(0, map[string]interface{}{
		"U": float64(7018),
		"I": float64(0.012),
	})
}

func TestErnSet(t *testing.T) {
	pt := newProtocolTester(t, ernConfig)
	pt.commander.enqueue("Z441E", "!441")
	pt.verifySet(1, "On", 1)
	pt.commander.enqueue("Z441D", "!441")
	pt.verifySet(2, "Off", 1)
}

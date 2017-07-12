package main

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var scpiPortConfig = &PortConfig{
	PortSettings: &PortSettings{
		Name:        "somedev",
		Port:        samplePort,
		Protocol:    "scpi",
		IdSubstring: "IZNAKURNOZH",
	},
	Parameters: []ParameterSpec{
		&scpiParameterSpec{
			Control: ControlConfig{
				Name:     "current1",
				Title:    "Current 1",
				Units:    "A",
				Writable: true,
			},
			ScpiName: "CURR",
		},
	},
}

func prepareScpiTest(t *testing.T) (*cmdTester, *DeviceCommander, Protocol) {
	tester := newCmdTester(t, scpiPortConfig.Port)
	commander := NewCommander(tester.connect, scpiPortConfig.PortSettings)
	protocol, err := CreateProtocol(scpiPortConfig)
	if err != nil {
		t.Fatalf("CreateProtocol(): %v", err)
	}
	commander.Connect()
	<-commander.Ready()
	commander.SetClock(tester)
	return tester, commander, protocol
}

func verifyQuery(t *testing.T, commander Commander, param Parameter, expectedName string) (string, error) {
	var r string
	var err1 error
	handlerCalled := false
	err := param.Query(commander, func(name string, value interface{}) {
		if name != expectedName {
			err1 = fmt.Errorf("bad param name %q instead of expected %q", name, expectedName)
		} else if handlerCalled {
			err1 = errors.New("the handler called more than one time")
		}
		handlerCalled = true
		r = value.(string)
	})
	if err == nil {
		err = err1
	}
	if err != nil {
		t.Error(err)
	}
	return r, err
}

func TestScpi(t *testing.T) {
	tester, commander, protocol := prepareScpiTest(t)
	tester.chat("*IDN?", "IZNAKURNOZH", func() (string, error) {
		return protocol.Identify(commander)
	})
	param, err := protocol.Parameter(scpiPortConfig.Parameters[0])
	if err != nil {
		t.Fatalf("Parameter(): %v", err)
	}
	tester.chat("CURR?", "3.500", func() (string, error) {
		return verifyQuery(t, commander, param, "current1")
	})
	tester.acceptSetCommand("CURR 3.4; *OPC?", "1", func() error {
		return param.Set(commander, "current1", "3.4")
	})
}

func TestScpiBadIdn(t *testing.T) {
	tester, commander, protocol := prepareScpiTest(t)
	errCh := make(chan error)
	go func() {
		_, err := protocol.Identify(commander)
		errCh <- err
	}()

	tester.expectCommand("*IDN?")
	tester.writeResponse("wrongresponse")
	tester.expectCommand("*IDN?")
	tester.writeResponse("wrongagain")
	tester.expectCommand("*IDN?")
	tester.writeResponse("IZNAKURNOZH,1,2,3,4")

	select {
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Identify() to finish")
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Identify() failed: %v", err)
		}
	}
}

var scpiConfig = `
ports:
- name: somedev
  port: someport
  protocol: scpi
  idsubstring: IZNAKURNOZH
  parameters:
  - name: current1
    title: Current 1
    units: A
    writable: true
    scpiname: CURR
`

func TestScpiWithFakeCommander(t *testing.T) {
	pt := newProtocolTester(t, scpiConfig)

	pt.commander.enqueue("*IDN?", "IZNAKURNOZH")
	id, err := pt.protocol.Identify(pt.commander)
	if err != nil {
		t.Fatalf("Identify(): %v", err)
	}
	if id != "IZNAKURNOZH" {
		t.Errorf("Bad id %q", id)
	}
	pt.commander.verifyAndFlush()

	pt.commander.enqueue("CURR?", "3.500")
	pt.verifyQuery(0, map[string]interface{}{"current1": "3.500"})
	pt.commander.verifyAndFlush()

	pt.commander.enqueue("CURR 3.4; *OPC?", "1")
	pt.verifySet(0, "current1", "3.4")
	pt.commander.verifyAndFlush()
}

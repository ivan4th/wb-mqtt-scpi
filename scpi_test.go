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

func TestScpiWithFakeCommander(t *testing.T) {
	commander := newFakeCommander(t)
	protocol, err := CreateProtocol(scpiPortConfig)
	if err != nil {
		t.Fatalf("CreateProtocol(): %v", err)
	}
	commander.Connect()
	<-commander.Ready()

	commander.enqueue("*IDN?", "IZNAKURNOZH")
	id, err := protocol.Identify(commander)
	if err != nil {
		t.Fatalf("Identify(): %v", err)
	}
	if id != "IZNAKURNOZH" {
		t.Errorf("Bad id %q", id)
	}
	commander.verifyAndFlush()

	commander.enqueue("CURR?", "3.500")
	param, err := protocol.Parameter(scpiPortConfig.Parameters[0])
	if err != nil {
		t.Fatalf("Parameter(): %v", err)
	}
	verifyQuery(t, commander, param, "current1")
	commander.verifyAndFlush()

	commander.enqueue("CURR 3.4; *OPC?", "1")
	param.Set(commander, "3.4")
	commander.verifyAndFlush()
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
		return param.Set(commander, "3.4")
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

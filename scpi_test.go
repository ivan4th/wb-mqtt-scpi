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
		var r string
		handlerCalled := false
		if err = param.Query(commander, func(name string, value interface{}) {
			if name != "current1" {
				err = fmt.Errorf("bad param name %q", name)
			} else if handlerCalled {
				err = errors.New("the handler called more than one time")
			}
			handlerCalled = true
			r = value.(string)
		}); err != nil {
			return "", err
		}
		return r, nil
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

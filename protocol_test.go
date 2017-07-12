package main

import (
	"reflect"
	"testing"
)

type protocolTester struct {
	t          *testing.T
	commander  *fakeCommander
	protocol   Protocol
	portConfig *PortConfig
}

func newProtocolTester(t *testing.T, configText string) *protocolTester {
	config, err := ParseDriverConfig([]byte(configText))
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
	return &protocolTester{t, commander, protocol, config.Ports[0]}
}

func (pt *protocolTester) param(paramIndex int) Parameter {
	param, err := pt.protocol.Parameter(pt.portConfig.Parameters[paramIndex])
	if err != nil {
		pt.t.Fatalf("Parameter(): %v", err)
	}
	return param
}

func (pt *protocolTester) verifyQuery(paramIndex int, expectedResult map[string]interface{}) {
	param := pt.param(paramIndex)
	r := make(map[string]interface{})
	if err := param.Query(pt.commander, func(name string, value interface{}) {
		r[name] = value
	}); err != nil {
		pt.t.Fatalf("Query(): %v", err)
	}
	if !reflect.DeepEqual(r, expectedResult) {
		pt.t.Errorf("bad query result: %#v (expected: %#v)", r, expectedResult)
	}
}

func (pt *protocolTester) verifySet(paramIndex int, controlName string, value interface{}) {
	param := pt.param(paramIndex)
	if err := param.Set(pt.commander, controlName, value); err != nil {
		pt.t.Fatalf("Set(): %v", err)
	}
}

func (pt *protocolTester) verifyQueryError(paramIndex int, errStr string) {
	param := pt.param(paramIndex)
	err := param.Query(pt.commander, func(string, interface{}) {
		pt.t.Errorf("unexpected query handler call")
	})
	if err == nil {
		pt.t.Errorf("no error received for param %d", paramIndex)
	}
	if err.Error() != errStr {
		pt.t.Errorf("unexpected error string %q (expected %q)", err, errStr)
	}
}

func (pt *protocolTester) verifySetError(paramIndex int, controlName string, value interface{}, errStr string) {
	param := pt.param(paramIndex)
	err := param.Set(pt.commander, controlName, value)
	if err == nil {
		pt.t.Errorf("no error received for param %d", paramIndex)
	}
	if err.Error() != errStr {
		pt.t.Errorf("unexpected error string %q (expected %q)", err, errStr)
	}
}

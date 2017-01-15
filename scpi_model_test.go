package main

import (
	"errors"
	"github.com/contactless/wbgo"
	"github.com/contactless/wbgo/testutils"
	"testing"
)

var (
	errBadPort   = errors.New("bad port passed to connector")
	sampleConfig = &ScpiConfig{
		Ports: []*ScpiPortConfig{
			{
				Name:        "sample",
				Title:       "Sample Dev",
				Port:        "localhost:10010",
				IdSubstring: "some_dev_id",
				Controls: []*ScpiControl{
					{
						Name:     "voltage",
						Title:    "Measured voltage",
						Units:    "V",
						ScpiName: "MEAS:VOLT",
						Type:     "voltage",
					},
					{
						Name:     "current",
						Title:    "Current",
						Units:    "A",
						ScpiName: "CURR",
						Type:     "current",
						Writable: true,
					},
				},
			},
		},
	}
)

type ScpiModelSuite struct {
	testutils.Suite
	*testutils.FakeMQTTFixture
	client *testutils.FakeMQTTClient
	driver *wbgo.Driver
	model  *ScpiModel
	tester *scpiTester
}

func (s *ScpiModelSuite) T() *testing.T {
	return s.Suite.T()
}

func (s *ScpiModelSuite) SetupTest() {
	s.Suite.SetupTest()
	s.FakeMQTTFixture = testutils.NewFakeMQTTFixture(s.T())
}

func (s *ScpiModelSuite) Start() {
	s.tester = newScpiTester(s.T(), sampleConfig.Ports[0].Port)
	s.model = NewScpiModel(s.tester.connect, sampleConfig)
	s.client = s.Broker.MakeClient("tst")
	s.client.Start()
	s.driver = wbgo.NewDriver(s.model, s.Broker.MakeClient("driver"))
	s.driver.SetAutoPoll(false)
	if err := s.driver.Start(); err != nil {
		s.T().Fatalf("failed to start the driver: %v", err)
	}
	<-s.model.Ready()
	s.Verify(
		"driver -> /devices/sample/meta/name: [Sample Dev] (QoS 1, retained)",
	)
}

func (s *ScpiModelSuite) TearDownTest() {
	if s.driver != nil {
		s.driver.Stop()
		s.Verify(
			"stop: driver",
		)
	}
	s.Suite.TearDownTest()
}

func (s *ScpiModelSuite) verifyPoll() {
	s.driver.Poll()
	s.tester.simpleChat("*IDN?", "some_dev_id")
	s.tester.simpleChat("MEAS:VOLT?", "12.0")
	s.tester.simpleChat("CURR?", "3.5")
	s.Verify(
		"driver -> /devices/sample/controls/id/meta/type: [text] (QoS 1, retained)",
		"driver -> /devices/sample/controls/id/meta/readonly: [1] (QoS 1, retained)",
		"driver -> /devices/sample/controls/id/meta/order: [1] (QoS 1, retained)",
		"driver -> /devices/sample/controls/id: [some_dev_id] (QoS 1, retained)",
		"driver -> /devices/sample/controls/voltage/meta/type: [voltage] (QoS 1, retained)",
		"driver -> /devices/sample/controls/voltage/meta/name: [Measured voltage] (QoS 1, retained)",
		"driver -> /devices/sample/controls/voltage/meta/units: [V] (QoS 1, retained)",
		"driver -> /devices/sample/controls/voltage/meta/readonly: [1] (QoS 1, retained)",
		"driver -> /devices/sample/controls/voltage/meta/order: [2] (QoS 1, retained)",
		"driver -> /devices/sample/controls/voltage: [12.0] (QoS 1, retained)",
		"driver -> /devices/sample/controls/current/meta/type: [current] (QoS 1, retained)",
		"driver -> /devices/sample/controls/current/meta/name: [Current] (QoS 1, retained)",
		"driver -> /devices/sample/controls/current/meta/units: [A] (QoS 1, retained)",
		"driver -> /devices/sample/controls/current/meta/writable: [1] (QoS 1, retained)",
		"driver -> /devices/sample/controls/current/meta/order: [3] (QoS 1, retained)",
		"driver -> /devices/sample/controls/current: [3.5] (QoS 1, retained)",
		"Subscribe -- driver: /devices/sample/controls/current/on",
	)
}

func (s *ScpiModelSuite) TestPoll() {
	s.Start()
	s.verifyPoll()
	// second poll doesn't generate .../meta/... and doesn't poll device id
	s.driver.Poll()
	s.tester.simpleChat("MEAS:VOLT?", "12.0")
	s.tester.simpleChat("CURR?", "3.5")
	s.Verify(
		"driver -> /devices/sample/controls/voltage: [12.0] (QoS 1, retained)",
		"driver -> /devices/sample/controls/current: [3.5] (QoS 1, retained)",
	)
}

func (s *ScpiModelSuite) TestSet() {
	s.Start()
	s.verifyPoll()
	s.client.Publish(wbgo.MQTTMessage{"/devices/sample/controls/current/on", "3.6", 1, false})
	s.tester.simpleChat("CURR 3.6; *OPC?", "1")
	s.Verify(
		"tst -> /devices/sample/controls/current/on: [3.6] (QoS 1)",
		"driver -> /devices/sample/controls/current: [3.6] (QoS 1, retained)",
	)
}

func TestSmartbusDriverSuite(t *testing.T) {
	testutils.RunSuites(t, new(ScpiModelSuite))
}

// TBD: multiple devices w/separate connections per scpi_model
// TBD: open conn in Start(), close in Stop()
// TBD: add OnError to DeviceObserver, support error handling
// TBD: reconnect on network timeout / connection errors
//      (don't 'reconnect' to serial upon timeout)
// TBD: only confirm writes after subsequent checks
//      Related: async value setting / querying
//      will need to change wbgo.Driver code
// TBD: autopoll, delay between params
// TBD: in ScpiModel, close devices & set 'devs' value to nil on stop
// TBD: publish writable topics as 'writable' (check homeui srcs)

// TBD: test config w/o title
// TBD: config parsing
// TBD: test handling of errors returned by connector

// TBD: don't send values that didn't change

// TBD: parallel poll

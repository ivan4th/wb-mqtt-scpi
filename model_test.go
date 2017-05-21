package main

import (
	"time"

	"github.com/contactless/wbgo"
	"github.com/contactless/wbgo/testutils"
	"testing"
)

func sampleConfig() *DriverConfig {
	return &DriverConfig{
		Ports: []*PortConfig{
			{
				PortSettings: &PortSettings{
					Name:        "sample",
					Title:       "Sample Dev",
					Port:        "localhost:10010",
					Protocol:    "scpi",
					IdSubstring: "some_dev_id",
				},
				Parameters: []ParameterSpec{
					&scpiParameterSpec{
						Control: ControlConfig{
							Name:  "voltage",
							Title: "Measured voltage",
							Units: "V",
							Type:  "voltage",
						},
						ScpiName: "MEAS:VOLT",
					},
					&scpiParameterSpec{
						Control: ControlConfig{
							Name:     "current",
							Title:    "Current",
							Units:    "A",
							Type:     "current",
							Writable: true,
						},
						ScpiName: "CURR",
					},
					&scpiParameterSpec{
						Control: ControlConfig{
							Name:  "mode",
							Title: "Mode",
							Type:  "text",
							Enum: map[int]string{
								0: "Foo",
								1: "Bar",
								2: "Baz",
							},
						},
						ScpiName: "MODE",
					},
					&scpiParameterSpec{
						Control: ControlConfig{
							Name:  "doit",
							Title: "Do it",
							Type:  "pushbutton",
						},
						ScpiName: "DOIT",
					},
				},
			},
		},
	}
}

type ModelSuite struct {
	testutils.Suite
	*testutils.FakeMQTTFixture
	client        *testutils.FakeMQTTClient
	driver        *wbgo.Driver
	model         *Model
	tester        *cmdTester
	pollTriggerCh chan struct{}
}

func (s *ModelSuite) T() *testing.T {
	return s.Suite.T()
}

func (s *ModelSuite) SetupTest() {
	s.Suite.SetupTest()
	s.FakeMQTTFixture = testutils.NewFakeMQTTFixture(s.T())
}

func (s *ModelSuite) Start(config *DriverConfig) {
	s.tester = newCmdTester(s.T(), config.Ports[0].Port)
	s.model = NewModel(DefaultCommanderFactory(s.tester.connect), config)
	s.pollTriggerCh = make(chan struct{})
	s.model.SetPollTriggerCh(s.pollTriggerCh)
	s.client = s.Broker.MakeClient("tst")
	s.client.Start()
	s.driver = wbgo.NewDriver(s.model, s.Broker.MakeClient("driver"))
	s.driver.SetPollInterval(50 * time.Millisecond)
	if err := s.driver.Start(); err != nil {
		s.T().Fatalf("failed to start the driver: %v", err)
	}
	<-s.model.Ready()
	s.Verify(
		"driver -> /devices/sample/meta/name: [Sample Dev] (QoS 1, retained)",
	)
}

func (s *ModelSuite) TearDownTest() {
	if s.tester != nil {
		s.tester.close()
	}
	if s.driver != nil {
		s.driver.Stop()
		s.Verify(
			"stop: driver",
		)
	}
	s.Suite.TearDownTest()
}

func (s *ModelSuite) verifyPoll() {
	s.pollTriggerCh <- struct{}{}

	s.tester.simpleChat("*IDN?", "some_dev_id")
	s.tester.simpleChat("MEAS:VOLT?", "12.0")
	s.tester.simpleChat("CURR?", "3.5")
	s.tester.simpleChat("MODE?", "1")

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
		"driver -> /devices/sample/controls/mode/meta/type: [text] (QoS 1, retained)",
		"driver -> /devices/sample/controls/mode/meta/name: [Mode] (QoS 1, retained)",
		"driver -> /devices/sample/controls/mode/meta/readonly: [1] (QoS 1, retained)",
		"driver -> /devices/sample/controls/mode/meta/order: [4] (QoS 1, retained)",
		"driver -> /devices/sample/controls/mode: [Bar] (QoS 1, retained)",
		"driver -> /devices/sample/controls/doit/meta/type: [pushbutton] (QoS 1, retained)",
		"driver -> /devices/sample/controls/doit/meta/name: [Do it] (QoS 1, retained)",
		"driver -> /devices/sample/controls/doit/meta/order: [5] (QoS 1, retained)",
		"Subscribe -- driver: /devices/sample/controls/doit/on",
	)
}

func (s *ModelSuite) TestPoll() {
	s.Start(sampleConfig())
	s.verifyPoll()
	for i := 0; i < 3; i++ {
		s.pollTriggerCh <- struct{}{}

		// second and the following polls don't generate .../meta/... and doesn't poll device id
		s.tester.simpleChat("MEAS:VOLT?", "12.0")
		s.tester.simpleChat("CURR?", "3.5")
		s.tester.simpleChat("MODE?", "0")

		s.Verify(
			"driver -> /devices/sample/controls/voltage: [12.0] (QoS 1, retained)",
			"driver -> /devices/sample/controls/current: [3.5] (QoS 1, retained)",
			"driver -> /devices/sample/controls/mode: [Foo] (QoS 1, retained)",
		)
	}
}
func (s *ModelSuite) TestPollWithResync() {
	config := sampleConfig()
	config.Ports[0].Resync = true
	s.Start(config)
	s.verifyPoll()
	for i := 0; i < 3; i++ {
		s.pollTriggerCh <- struct{}{}

		// second and the following polls don't generate .../meta/... and doesn't poll device id
		s.tester.simpleChat("*IDN?", "some_dev_id")
		s.tester.simpleChat("MEAS:VOLT?", "12.0")
		s.tester.simpleChat("CURR?", "3.5")
		s.tester.simpleChat("MODE?", "0")

		s.Verify(
			"driver -> /devices/sample/controls/voltage: [12.0] (QoS 1, retained)",
			"driver -> /devices/sample/controls/current: [3.5] (QoS 1, retained)",
			"driver -> /devices/sample/controls/mode: [Foo] (QoS 1, retained)",
		)
	}
}

func (s *ModelSuite) TestSet() {
	s.Start(sampleConfig())
	s.verifyPoll()
	s.client.Publish(wbgo.MQTTMessage{"/devices/sample/controls/current/on", "3.6", 1, false})
	s.tester.simpleChat("CURR 3.6; *OPC?", "1")

	s.Verify(
		"tst -> /devices/sample/controls/current/on: [3.6] (QoS 1)",
		"driver -> /devices/sample/controls/current: [3.6] (QoS 1, retained)",
	)
	s.client.Publish(wbgo.MQTTMessage{"/devices/sample/controls/doit/on", "1", 1, false})
	s.tester.simpleChat("DOIT; *OPC?", "1")

	s.Verify(
		"tst -> /devices/sample/controls/doit/on: [1] (QoS 1)",
		// note that button value is not retained
		"driver -> /devices/sample/controls/doit: [1] (QoS 1)",
	)
}

func TestModelSuite(t *testing.T) {
	testutils.RunSuites(t, new(ModelSuite))
}

// TBD: open conn in Start(), close in Stop()
// TBD: add OnError to DeviceObserver, support error handling
// TBD: reconnect on network timeout / connection errors
//      (don't 'reconnect' to serial upon timeout)
// TBD: only confirm writes after subsequent checks
//      Related: async value setting / querying
//      will need to change wbgo.Driver code
// TBD: autopoll, delay between params
// TBD: in Model, close devices & set 'devs' value to nil on stop
// TBD: publish writable topics as 'writable' (check homeui srcs)

// TBD: test config w/o title
// TBD: config parsing
// TBD: test handling of errors returned by connector

// TBD: don't send values that didn't change

// TBD: parallel poll

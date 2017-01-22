package main

import (
	"fmt"
	"log"

	"github.com/contactless/wbgo"
)

type device struct {
	wbgo.DeviceBase
	commander           Commander
	protocol            Protocol
	portConfig          *PortConfig
	controls            []*ControlConfig
	parameters          []Parameter
	nameToSettableParam map[string]Parameter
	nameToControl       map[string]*ControlConfig
	controlsSent        map[string]bool
}

var (
	idControl = &ControlConfig{
		Name:  "id",
		Title: "id",
		Type:  "text",
	}
)

func newDevice(commander Commander, portConfig *PortConfig) (*device, error) {
	protocol, err := CreateProtocol(portConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create protocol: %v", err)
	}

	controls, paramSpecSetMap, err := portConfig.GetControls()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve controls: %v", err)
	}

	title := portConfig.Title
	if title == "" {
		title = portConfig.Name
	}

	var params []Parameter
	paramMap := make(map[ParameterSpec]Parameter)
	for _, paramSpec := range portConfig.Parameters {
		param, err := protocol.Parameter(paramSpec)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve parameter: %v", err)
		}
		params = append(params, param)
		paramMap[paramSpec] = param
	}

	paramSetMap := make(map[string]Parameter)
	for name, paramSpec := range paramSpecSetMap {
		if paramMap[paramSpec] == nil {
			log.Panicf("internal error: can't find paramSpec for %v", name)
		}
		paramSetMap[name] = paramMap[paramSpec]
	}

	d := &device{
		DeviceBase: wbgo.DeviceBase{
			DevName:  portConfig.Name,
			DevTitle: title,
		},
		commander:           commander,
		protocol:            protocol,
		portConfig:          portConfig,
		controls:            controls,
		parameters:          params,
		nameToSettableParam: paramSetMap,
		nameToControl:       make(map[string]*ControlConfig),
		controlsSent:        make(map[string]bool),
	}

	for _, control := range controls {
		d.nameToControl[control.Name] = control
	}
	return d, nil
}

func (d *device) handleQueryResponse(control *ControlConfig, r string) {
	if !d.controlsSent[control.Name] {
		writability := wbgo.ForceReadOnly
		if control.Writable {
			writability = wbgo.ForceWritable
		}
		title := control.Title
		if title == control.Name {
			// use auto title
			title = ""
		}
		d.Observer.OnNewControl(d, wbgo.Control{
			Name:        control.Name,
			Title:       title,
			Type:        control.Type,
			Units:       control.Units,
			Value:       r,
			Writability: writability,
		})
		// d.Observer.OnNewControl(d, control.Name, control.Type, r, !control.Writable, -1, true)
		d.controlsSent[control.Name] = true
	} else {
		d.Observer.OnValue(d, control.Name, r)
	}
}

func (d *device) identify() bool {
	r, err := d.protocol.Identify(d.commander)
	if err != nil {
		wbgo.Error.Printf("Identify() failed for device %s: %v", d.portConfig.Name, err)
		return false
	}
	d.handleQueryResponse(idControl, r)
	return true
}

func (d *device) poll() {
	select {
	case <-d.commander.Ready():
	default:
		return
	}

	// only poll 'id' once
	if !d.controlsSent["id"] && !d.identify() {
		return
	}

	for _, param := range d.parameters {
		err := param.Query(d.commander, func(name string, v interface{}) {
			control, found := d.nameToControl[name]
			if !found {
				log.Panicf("internal error: control %v not found by device", name)
			}
			// TODO: just pass string there from Parameter.Query()
			d.handleQueryResponse(control, fmt.Sprintf("%v", v))
		})
		if err != nil {
			wbgo.Error.Printf("failed to read %s from %q: %v", param.Name(), d.portConfig.Name, err)

		}
	}
}

func (d *device) AcceptValue(string, string) {
	// ignore retained values
}

func (d *device) AcceptOnValue(name, value string) bool {
	control, found := d.nameToControl[name]
	if !found {
		wbgo.Error.Printf("unknown control %q for device %q", name, d.portConfig.Name)
		return false
	}
	if !control.Writable {
		wbgo.Error.Printf("trying to set value %q for non-writable control %s/%s", value, d.portConfig.Name, name)
		return false
	}
	param, found := d.nameToSettableParam[name]
	if !found {
		wbgo.Error.Printf("no settable parameter for control %q in device %q", name, d.portConfig.Name)
		return false
	}
	param.Set(d.commander, value)
	return true
}

func (d *device) IsVirtual() bool {
	return false
}

type Model struct {
	wbgo.ModelBase
	cmdFactory CommanderFactory
	config     *DriverConfig
	devs       []*device
	readyCh    chan struct{}
}

func NewModel(commanderFactory CommanderFactory, config *DriverConfig) *Model {
	return &Model{
		cmdFactory: commanderFactory,
		config:     config,
		readyCh:    make(chan struct{}),
	}
}

func (m *Model) Start() error {
	if m.devs != nil {
		return nil
	}
	if len(m.config.Ports) == 0 {
		return errNoPortsDefined
	}
	m.devs = []*device{}
	for _, portConfig := range m.config.Ports {
		commander := m.cmdFactory(portConfig.PortSettings)
		dev, err := newDevice(commander, portConfig)
		if err != nil {
			return fmt.Errorf("failed to set up device %q: %v", portConfig.Name, err)
		}
		m.devs = append(m.devs, dev)
		m.Observer.OnNewDevice(dev)
	}
	if len(m.devs) == 0 {
		return errNoPortsOpen
	}
	go func() {
		for _, d := range m.devs {
			d.commander.Connect()
		}
		for _, d := range m.devs {
			<-d.commander.Ready()
		}
		close(m.readyCh)
	}()
	return nil
}

func (m *Model) Ready() chan struct{} {
	return m.readyCh
}

func (m *Model) Poll() {
	if m.devs == nil {
		panic("trying to poll without starting the model")
	}
	for _, d := range m.devs {
		d.poll()
	}
}

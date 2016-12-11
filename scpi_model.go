package main

import (
	"github.com/contactless/wbgo"
	"io"
)

type Connector func(port string) (io.ReadWriteCloser, error)

type scpiDevice struct {
	wbgo.DeviceBase
	scpi          *Scpi
	portConfig    *ScpiPortConfig
	nameToControl map[string]*ScpiControl
	controlsSent  map[string]bool
}

var (
	idControl = &ScpiControl{
		Name:     "id",
		Title:    "id",
		ScpiName: "*IDN",
		Type:     "text",
	}
)

func newScpiDevice(scpi *Scpi, portConfig *ScpiPortConfig) *scpiDevice {
	title := portConfig.Title
	if title == "" {
		title = portConfig.Name
	}
	d := &scpiDevice{
		DeviceBase: wbgo.DeviceBase{
			DevName:  portConfig.Name,
			DevTitle: title,
		},
		scpi:          scpi,
		portConfig:    portConfig,
		nameToControl: make(map[string]*ScpiControl),
		controlsSent:  make(map[string]bool),
	}
	for _, control := range portConfig.Controls {
		d.nameToControl[control.Name] = control
	}
	return d
}

func (d *scpiDevice) handleQueryResponse(control *ScpiControl, r string) {
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

func (d *scpiDevice) identify() {
	r, err := d.scpi.Identify()
	if err != nil {
		wbgo.Error.Printf("Identify() failed for device %s: %v", d.portConfig.Name, err)
		return
	}
	d.handleQueryResponse(idControl, r)
}

func (d *scpiDevice) pollControl(control *ScpiControl) {
	r, err := d.scpi.Query(control.ScpiName + "?")
	if err != nil {
		wbgo.Error.Printf("failed to read %s/%s %q: %v", d.portConfig.Name, control.Name, control.ScpiName)
		return
	}
	d.handleQueryResponse(control, r)
}

func (d *scpiDevice) poll() {
	// only poll 'id' once
	if !d.controlsSent["id"] {
		d.identify()
	}
	for _, control := range d.portConfig.Controls {
		d.pollControl(control)
	}
}

func (d *scpiDevice) AcceptValue(string, string) {
	// ignore retained values
}

func (d *scpiDevice) AcceptOnValue(name, value string) bool {
	control, found := d.nameToControl[name]
	if !found {
		wbgo.Error.Printf("unknown control %q for device %q", name, d.portConfig.Name)
		return false
	}
	if !control.Writable {
		wbgo.Error.Printf("trying to set value %q for non-writable control %s/%s", value, d.portConfig.Name, name)
		return false
	}
	d.scpi.Set(control.ScpiName, value)
	return true
}

func (d *scpiDevice) IsVirtual() bool {
	return false
}

type ScpiModel struct {
	wbgo.ModelBase
	connector Connector
	config    *ScpiConfig
	devs      []*scpiDevice
}

func NewScpiModel(connector Connector, config *ScpiConfig) *ScpiModel {
	return &ScpiModel{connector: connector, config: config}
}

func (m *ScpiModel) Start() error {
	if m.devs != nil {
		return nil
	}
	if len(m.config.Ports) == 0 {
		return errNoPortsDefined
	}
	m.devs = []*scpiDevice{}
	for _, portConfig := range m.config.Ports {
		rwc, err := m.connector(portConfig.Port)
		if err != nil {
			wbgo.Error.Printf("failed to open port %q: %v", portConfig.Name, err)
			continue
		}
		dev := newScpiDevice(NewScpi(rwc, portConfig.IdSubstring), portConfig)
		m.devs = append(m.devs, dev)
		m.Observer.OnNewDevice(dev)
	}
	if len(m.devs) == 0 {
		return errNoPortsOpen
	}
	return nil
}

func (m *ScpiModel) Poll() {
	if m.devs == nil {
		panic("trying to scpi poll without starting the model")
	}
	for _, d := range m.devs {
		d.poll()
	}
}

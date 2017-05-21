package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/contactless/wbgo"
)

const (
	minPollInterval = 50 * time.Millisecond
)

type deviceControl struct {
	sync.Mutex    // protects 'sent' and 'value'
	settableParam Parameter
	config        *ControlConfig
	dirty         bool
	sent          bool
	writing       bool
	value         string
}

func (dc *deviceControl) writability() wbgo.Writability {
	switch {
	case dc.config.Type == "pushbutton":
		return wbgo.DefaultWritability
	case dc.config.Writable:
		return wbgo.ForceWritable
	default:
		return wbgo.ForceReadOnly
	}
}

func (dc *deviceControl) title() string {
	if dc.config.Title == dc.config.Name {
		// use auto title
		return ""
	}
	return dc.config.Title
}

func (dc *deviceControl) toWbgoControl() wbgo.Control {
	return wbgo.Control{
		Name:        dc.config.Name,
		Title:       dc.title(),
		Type:        dc.config.Type,
		Units:       dc.config.Units,
		Value:       dc.value,
		Writability: dc.writability(),
	}
}

func (dc *deviceControl) wasPolled() bool {
	dc.Lock()
	defer dc.Unlock()
	return dc.dirty || dc.sent
}

func (dc *deviceControl) setValueFromDevice(v interface{}) {
	dc.Lock()
	defer dc.Unlock()
	if dc.writing {
		return
	}
	dc.value = dc.config.TransformDeviceValue(v)
	// should only send id value once
	dc.dirty = !dc.sent || (dc.config.Name != idControlName && dc.config.ShouldPoll())
}

func (dc *deviceControl) startWrite(value string) {
	dc.Lock()
	defer dc.Unlock()
	dc.value = value
	dc.dirty = false
	dc.writing = true
}

func (dc *deviceControl) endWrite() {
	dc.Lock()
	defer dc.Unlock()
	dc.writing = false
}

func (dc *deviceControl) send(dev wbgo.LocalDeviceModel, observer wbgo.DeviceObserver) {
	dc.Lock()
	if !dc.dirty {
		dc.Unlock()
		return
	}
	dc.dirty = false
	if !dc.sent {
		wbgoControl := dc.toWbgoControl()
		dc.sent = true
		dc.Unlock()
		observer.OnNewControl(dev, wbgoControl)
	} else {
		v := dc.value
		dc.Unlock()
		observer.OnValue(dev, dc.config.Name, v)
	}
}

type device struct {
	wbgo.DeviceBase
	commander  Commander
	protocol   Protocol
	portConfig *PortConfig
	stopCh     chan struct{}
	controls   map[string]*deviceControl
	parameters []Parameter
}

var (
	idControlName = "id"
	idControl     = &ControlConfig{
		Name:  idControlName,
		Title: "id",
		Type:  "text",
	}
)

func newDevice(commander Commander, portConfig *PortConfig, stopCh chan struct{}) (*device, error) {
	protocol, err := CreateProtocol(portConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create protocol: %v", err)
	}

	controlConfigs, paramSpecSetMap, err := portConfig.GetControls()
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

	d := &device{
		DeviceBase: wbgo.DeviceBase{
			DevName:  portConfig.Name,
			DevTitle: title,
		},
		commander:  commander,
		protocol:   protocol,
		portConfig: portConfig,
		stopCh:     stopCh,
		controls:   make(map[string]*deviceControl),
		parameters: params,
	}

	d.controls[idControlName] = &deviceControl{config: idControl}
	for _, controlConfig := range controlConfigs {
		d.controls[controlConfig.Name] = &deviceControl{
			config: controlConfig,
		}
	}

	for name, paramSpec := range paramSpecSetMap {
		if paramMap[paramSpec] == nil {
			log.Panicf("internal error: can't find paramSpec for %v", name)
		}
		d.control(name).settableParam = paramMap[paramSpec]
	}

	return d, nil
}

func (d *device) control(name string) *deviceControl {
	if control, ok := d.controls[name]; !ok {
		panic("bad control name: " + name)
	} else {
		return control
	}
}

func (d *device) idControl() *deviceControl {
	return d.control(idControlName)
}

func (d *device) identify() bool {
	r, err := d.protocol.Identify(d.commander)
	if err != nil {
		select {
		case <-d.stopCh:
			return false
			// ignore errors if stopping
		default:
			wbgo.Error.Printf("Identify() failed for device %s: %v", d.portConfig.Name, err)
		}
		return false
	}
	d.idControl().setValueFromDevice(r)
	return true
}

// poll polls the underlying device and marks any updated control as dirty
func (d *device) poll() {
	// only poll 'id' once unless Resync is enabled, in which
	// case read id on each poll loop
	if (d.portConfig.Resync || !d.idControl().wasPolled()) && !d.identify() {
		return
	}

	for n, param := range d.parameters {
		// FIXME: don't assume same indices to these arrays!
		paramSpec := d.portConfig.Parameters[n]
		if !paramSpec.ShouldPoll() {
			for _, controlConfig := range paramSpec.ListControls() {
				if !controlConfig.ShouldPoll() {
					d.control(controlConfig.Name).setValueFromDevice("")
				}
			}
			continue
		}
		err := param.Query(d.commander, func(name string, v interface{}) {
			d.control(name).setValueFromDevice(v)
		})
		if err != nil {
			select {
			case <-d.stopCh:
				// ignore errors if stopping
			default:
				wbgo.Error.Printf("failed to read %s from %q: %v", param.Name(), d.portConfig.Name, err)
			}

		}
	}
}

// send sends any dirty controls, or values for dirty controls for which metadata
// was already sent. The function is threadsafe in the sense that it can be
// called safely from another goroutine while poll() is still running
func (d *device) send() {
	// TODO: keep an ordered list of controls
	d.idControl().send(d, d.Observer)
	for n, _ := range d.parameters {
		// FIXME: don't assume same indices to these arrays!
		paramSpec := d.portConfig.Parameters[n]
		for _, controlConfig := range paramSpec.ListControls() {
			d.control(controlConfig.Name).send(d, d.Observer)
		}
	}
}

func (d *device) AcceptValue(string, string) {
	// ignore retained values
}

func (d *device) AcceptOnValue(name, value string) bool {
	dc, found := d.controls[name]
	if !found {
		wbgo.Error.Printf("unknown control %q for device %q", name, d.portConfig.Name)
		return false
	}
	if !dc.config.Writable {
		wbgo.Error.Printf("trying to set value %q for non-writable control %s/%s", value, d.portConfig.Name, name)
		return false
	}
	if dc.settableParam == nil {
		wbgo.Error.Printf("no settable parameter for control %q in device %q", name, d.portConfig.Name)
		return false
	}
	dc.startWrite(value)
	dc.settableParam.Set(d.commander, dc.config.Name, value)
	dc.endWrite()
	return true
}

func (d *device) IsVirtual() bool {
	return false
}

func (d *device) close() {
	d.commander.Close()
}

type Model struct {
	wbgo.ModelBase
	cmdFactory    CommanderFactory
	config        *DriverConfig
	devs          []*device
	readyCh       chan struct{}
	stopCh        chan struct{}
	stoppedCh     chan struct{}
	pollTriggerCh chan struct{}
}

func NewModel(commanderFactory CommanderFactory, config *DriverConfig) *Model {
	return &Model{
		cmdFactory: commanderFactory,
		config:     config,
		readyCh:    make(chan struct{}),
		stopCh:     make(chan struct{}),
		stoppedCh:  make(chan struct{}),
	}
}

func (m *Model) SetPollTriggerCh(pollTriggerCh chan struct{}) {
	m.pollTriggerCh = pollTriggerCh
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
		dev, err := newDevice(commander, portConfig, m.stopCh)
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
		var wg sync.WaitGroup
		for _, dev := range m.devs {
			d := dev
			wg.Add(1)
			go func() {
				defer wg.Done()
			pollLoop:
				for {
					nextAt := time.Now().Add(minPollInterval)
					if m.pollTriggerCh != nil {
						select {
						case <-m.stopCh:
							break pollLoop
						case <-m.pollTriggerCh:
						}
					} else {
						select {
						case <-m.stopCh:
							break pollLoop
						default:
						}
					}
					d.poll()
					now := time.Now()
					if nextAt.After(now) {
						select {
						case <-m.stopCh:
							break pollLoop
						case <-time.After(nextAt.Sub(now)):
						}
					}
				}
			}()
		}
		wg.Wait()
		close(m.stoppedCh)
	}()
	return nil
}

func (m *Model) Stop() {
	if m.devs == nil {
		return
	}
	close(m.stopCh)
	// this should interrupt the poll loop
	for _, d := range m.devs {
		d.close()
	}
	<-m.stoppedCh
	m.devs = nil
}

func (m *Model) Ready() chan struct{} {
	return m.readyCh
}

func (m *Model) Poll() {
	if m.devs == nil {
		panic("trying to poll without starting the model")
	}
	for _, d := range m.devs {
		d.send()
	}
}

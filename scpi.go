package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/contactless/wbgo"
)

const (
	// FIXME: don't hardcode these values, use config
	scpiIdentifyNumAttempts = 10
)

type scpiParameter struct {
	scpiName, name string
}

var _ Parameter = &scpiParameter{}

func (p *scpiParameter) Name() string { return p.scpiName }

func (p *scpiParameter) Query(c Commander, receive func(name string, value interface{})) error {
	v, err := c.Query(p.scpiName + "?")
	if err != nil {
		return err
	}
	receive(p.name, v)
	return nil
}

func (p *scpiParameter) Set(c Commander, value interface{}) error {
	if r, err := c.Query(fmt.Sprintf("%s %s; *OPC?", p.scpiName, value)); err != nil {
		return err
	} else if r != "1" {
		return fmt.Errorf("unexpected set response %q", r)
	}
	return nil
}

type scpiParameterSpec struct {
	Control  ControlConfig `yaml:",inline"`
	ScpiName string
}

var _ ParameterSpec = &scpiParameterSpec{}

func (spec *scpiParameterSpec) ListControls() []*ControlConfig {
	return []*ControlConfig{&spec.Control}
}

func (spec *scpiParameterSpec) Settable() bool {
	return spec.Control.Writable
}

type scpiProtocol struct {
	idSubstring string
}

var _ Protocol = &scpiProtocol{}

func newScpiProtocol(config *PortConfig) (Protocol, error) {
	return &scpiProtocol{config.IdSubstring}, nil
}

func (p *scpiProtocol) Identify(c Commander) (r string, err error) {
	for i := 0; i < scpiIdentifyNumAttempts; i++ {
		r, err = c.Query("*IDN?")
		switch {
		case err == ErrTimeout:
			wbgo.Error.Print("Identify() timeout")
			continue
		case err != nil:
			wbgo.Error.Printf("Identify() error: %v", err)
			return "", err
		case p.idSubstring != "" && !strings.Contains(r, p.idSubstring):
			err = fmt.Errorf("bad id string %q (expected it to contain %q)", r, p.idSubstring)
		default:
			return r, nil
		}
	}
	return
}

func (p *scpiProtocol) Parameter(spec ParameterSpec) (Parameter, error) {
	scpiSpec, ok := spec.(*scpiParameterSpec)
	if !ok {
		return nil, errors.New("SCPI parameter spec expected")
	}
	return &scpiParameter{scpiName: scpiSpec.ScpiName, name: scpiSpec.Control.Name}, nil
}

func init() {
	RegisterProtocol("scpi", newScpiProtocol, &scpiParameterSpec{})
}

// TODO: rm rmme_scpi.go

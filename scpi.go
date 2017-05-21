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

type scpiParameterSpec struct {
	Control  ControlConfig `yaml:",inline"`
	ScpiName string
}

var _ ParameterSpec = &scpiParameterSpec{}

func (spec *scpiParameterSpec) ListControls() []*ControlConfig {
	return []*ControlConfig{&spec.Control}
}

func (spec *scpiParameterSpec) ShouldPoll() bool {
	return spec.Control.ShouldPoll()
}

func (spec *scpiParameterSpec) Settable() bool {
	return spec.Control.Writable
}

func (spec *scpiParameterSpec) Validate() error {
	if err := spec.Control.Validate(); err != nil {
		return err
	}
	if spec.ScpiName == "" {
		return errors.New("scpiName not specified")
	}
	return nil
}

type scpiParameter struct {
	scpiName, name string
	skipValue      bool
	prefix         string
}

var _ Parameter = &scpiParameter{}

func (p *scpiParameter) Name() string { return p.scpiName }

func (p *scpiParameter) Query(c Commander, handler QueryHandler) error {
	v, err := c.Query(p.scpiName + "?")
	if err != nil {
		return err
	}
	handler(p.name, v)
	return nil
}

func (p *scpiParameter) Set(c Commander, name string, value interface{}) error {
	if name != p.name {
		return fmt.Errorf("unknown control name %q", name)
	}
	var q string
	// FIXME: instead of prefix, should use slave addr (and build the whole command)
	if p.skipValue {
		q = fmt.Sprintf("%s; %s*OPC?", p.scpiName, p.prefix)
	} else {
		q = fmt.Sprintf("%s %s; %s*OPC?", p.scpiName, value, p.prefix)
	}
	if r, err := c.Query(q); err != nil {
		return err
	} else if r != "1" {
		return fmt.Errorf("unexpected set response %q", r)
	}
	return nil
}

type scpiProtocol struct {
	idSubstring, prefix string
}

var _ Protocol = &scpiProtocol{}

func newScpiProtocol(config *PortConfig) (Protocol, error) {
	return &scpiProtocol{config.IdSubstring, config.Prefix}, nil
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
	return &scpiParameter{
		scpiName:  scpiSpec.ScpiName,
		name:      scpiSpec.Control.Name,
		skipValue: scpiSpec.Control.Type == "pushbutton", // FIXME
		prefix:    p.prefix,
	}, nil
}

func init() {
	RegisterProtocol("scpi", newScpiProtocol, &scpiParameterSpec{})
}

// TODO: rm rmme_scpi.go

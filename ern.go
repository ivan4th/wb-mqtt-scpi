package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

func parseErnResponse(resp, commandStr string, expectDataItems int) ([]string, error) {
	prefix := "!" + commandStr[:3]
	if !strings.HasPrefix(resp, prefix) {
		return nil, fmt.Errorf("bad ern response %q", resp)
	}
	if len(resp) == len(prefix) {
		return nil, nil
	}
	if resp[len(prefix)] != '>' {
		return nil, fmt.Errorf("malformed response %q", resp)
	}

	if expectDataItems == 0 {
		return nil, nil
	}

	decoder := charmap.Windows1251.NewDecoder()
	data, err := decoder.String(resp[len(prefix)+1:])
	if err != nil {
		return nil, fmt.Errorf("error decoding device response %q", resp)
	}
	if expectDataItems > 1 {
		parts := strings.Split(data, "+")
		if len(parts) != expectDataItems {
			return nil, fmt.Errorf("insufficient N of response items: %q", resp)
		}
		return parts, nil
	}
	return []string{data}, nil
}

type ernParameterSpec struct {
	Command  string
	RespLen  int
	RespSkip int
	Controls []*ControlConfig
}

var _ ParameterSpec = &ernParameterSpec{}

func (spec *ernParameterSpec) ListControls() []*ControlConfig {
	return spec.Controls
}

func (spec *ernParameterSpec) ShouldPoll() bool {
	for _, control := range spec.Controls {
		if control.ShouldPoll() {
			return true
		}
	}
	return false
}

func (spec *ernParameterSpec) Settable() bool {
	for _, control := range spec.Controls {
		if control.Writable {
			return true
		}
	}
	return false
}

func (spec *ernParameterSpec) Validate() error {
	for _, control := range spec.Controls {
		if err := control.Validate(); err != nil {
			return err
		}
	}
	if spec.Command == "" {
		return fmt.Errorf("ern: no command specified")
	}
	return nil
}

type ernParameter struct {
	*ernParameterSpec
	address int
}

var _ Parameter = &ernParameter{}

func (p *ernParameter) Name() string {
	return p.commandStr()
}

func (p *ernParameter) commandStr() string {
	return fmt.Sprintf("%02d%s", p.address, p.Command)
}

func (p *ernParameter) parseResponse(resp string, expectData bool) ([]string, error) {
	expectDataItems := 0
	if expectData {
		expectDataItems = len(p.Controls) + p.RespSkip
	}
	parts, err := parseErnResponse(resp, p.commandStr(), expectDataItems)
	if err != nil {
		return nil, fmt.Errorf("parameter %s: %v", p.Name(), err)
	}
	return parts[p.RespSkip:], nil
}

func (p *ernParameter) Query(c Commander, handler QueryHandler) error {
	resp, err := c.Query("Z"+p.commandStr(), p.RespLen)
	if err != nil {
		return err
	}
	values, err := p.parseResponse(resp, true)
	if err != nil {
		return err
	}
	for n, control := range p.Controls {
		var v interface{}
		if control.Type == "text" {
			v = values[n]
		} else {
			s := strings.Replace(values[n], ",", ".", -1)
			v, err = strconv.ParseFloat(s, 64)
			if err != nil {
				return fmt.Errorf("can't parse number %q: %v", s, err)
			}
		}
		handler(control.Name, v)
	}
	return nil
}

func (p *ernParameter) Set(c Commander, name string, value interface{}) error {
	// this only works for pushbuttons as of now
	// TODO: need to support setting voltage/current
	resp, err := c.Query("Z"+p.commandStr(), 0)
	if err == nil {
		_, err = p.parseResponse(resp, false)
	}
	return err
}

type ernProtocol struct {
	idSubstring string
	address     int
}

func newErnProtocol(config *PortConfig) (Protocol, error) {
	if config.Address < 0 || config.Address >= 100 {
		return nil, fmt.Errorf("ern: bad address %d", config.Address)
	}
	return &ernProtocol{
		idSubstring: config.IdSubstring,
		address:     config.Address,
	}, nil
}

func (p *ernProtocol) Identify(c Commander) (r string, err error) {
	commandStr := fmt.Sprintf("%02dNN", p.address)
	resp, err := c.Query("Z"+commandStr, 0)
	if err != nil {
		return "", err
	}
	parts, err := parseErnResponse(resp, commandStr, 1)
	if err != nil {
		return "", err
	}
	return parts[0], nil
}

func (p *ernProtocol) Parameter(spec ParameterSpec) (Parameter, error) {
	ernSpec, ok := spec.(*ernParameterSpec)
	if !ok {
		return nil, errors.New("ERN parameter spec expected")
	}
	return &ernParameter{ernSpec, p.address}, nil
}

func init() {
	RegisterProtocol("ern", newErnProtocol, &ernParameterSpec{})
}

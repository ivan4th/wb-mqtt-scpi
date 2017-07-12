package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/contactless/wbgo"
)

const (
	edwardsIdCommand         = "?S902"
	edwardsIdResponsePrefix  = "=S902 "
	edwardsGeneralCommand    = "!C"
	edwardsSetupCommand      = "!S"
	edwardsQuerySetupCommand = "?S"
	edwardsQueryValueCommand = "?V"
	// FIXME: don't hardcode the following values directly, use config
	edwardsIdentifyNumAttempts = 10
)

var edwardsErrorCodes = []string{
	"no error",                         // 0
	"Invalid command for object ID",    // 1
	"Invalid query/command",            // 2
	"Missing parameter",                // 3
	"Parameter out of range",           // 4
	"Invalid command in current state", // 5
	"Data checksum error",              // 6
	"EEPROM read or write error",       // 7
	"Operation took too long",          // 8
	"Invalid config ID",                // 9
}

// TODO: param spec should contain a list of controls --
// there should be semicolon-separated value list in the response
// matching the number of controls
// TODO: it may include per-control decoding tables (enum)
// TODO: for enum types, may use yaml anchor/ref for now
// http://stackoverflow.com/a/2063741
type edwardsParameterSpec struct {
	Oid      int
	Sub      *int
	Controls []*ControlConfig
	Read     string
	Write    string
}

var _ ParameterSpec = &edwardsParameterSpec{}

func (spec *edwardsParameterSpec) ListControls() []*ControlConfig {
	return spec.Controls
}

func (spec *edwardsParameterSpec) ShouldPoll() bool {
	for _, control := range spec.Controls {
		if control.ShouldPoll() {
			return true
		}
	}
	return false
}

func (spec *edwardsParameterSpec) Settable() bool {
	return spec.Write != ""
}

func (spec *edwardsParameterSpec) Validate() error {
	for _, control := range spec.Controls {
		if err := control.Validate(); err != nil {
			return err
		}
	}
	if spec.Oid <= 0 {
		return fmt.Errorf("Invalid OID %d", spec.Oid)
	}
	if spec.Sub != nil && *spec.Sub < 0 {
		return fmt.Errorf("Negative sub %d, OID=%d", *spec.Sub, spec.Oid)
	}
	switch {
	case spec.Read == "" && spec.Write == "":
		return fmt.Errorf("OID %d: must specify read and/or write command", spec.Oid)
	case spec.Read != "" && spec.Read != edwardsQuerySetupCommand && spec.Read != edwardsQueryValueCommand:
		return fmt.Errorf("OID %d: 'read' must be either empty, %q or %q, but is %q", spec.Oid, edwardsQuerySetupCommand, edwardsQueryValueCommand, spec.Read)
	case spec.Write != "" && spec.Write != edwardsGeneralCommand && spec.Write != edwardsSetupCommand:
		return fmt.Errorf("OID %d: 'write' must be either empty, %q or %q, but is %q", spec.Oid, edwardsGeneralCommand, edwardsSetupCommand, spec.Write)
	}
	return nil
}

type edwardsParameter struct {
	*edwardsParameterSpec
}

var _ Parameter = &edwardsParameter{}

func (p *edwardsParameter) Name() string {
	if p.Sub != nil {
		return fmt.Sprintf("%d/%d", p.Oid, *p.Sub)
	} else {
		return strconv.Itoa(p.Oid)
	}
}

func (p *edwardsParameter) parseResponse(resp, cmdPrefix string) ([]string, error) {
	// req:  ?V904
	// resp: '=V904 0;0;0'
	// req:  ?S904 3
	// resp: '=S904 3;0'
	// error response looks like '*Cnnn 1'
	if len(resp) <= len(cmdPrefix)+1 || resp[1:len(cmdPrefix)+1] != cmdPrefix[1:]+" " {
		return nil, errors.New("invalid device response")
	}
	tail := resp[len(cmdPrefix)+1:]
	if resp[0] == '*' {
		errCode, err := strconv.Atoi(tail)
		if err != nil {
			return nil, errors.New("invalid error response")
		}
		if errCode == 0 {
			return nil, nil
		}
		if errCode >= len(edwardsErrorCodes) {
			return nil, fmt.Errorf("invalid error code %d", errCode)
		}
		return nil, fmt.Errorf("device error: %s", edwardsErrorCodes[errCode])
	}
	if resp[0] != '=' {
		return nil, errors.New("invalid device response")
	}
	values := strings.Split(tail, ";")
	if p.Sub != nil {
		if values[0] != strconv.Itoa(*p.Sub) {
			return nil, fmt.Errorf("invalid sub in response: %s", values[0])
		}
		values = values[1:]
	}
	return values, nil
}

func (p *edwardsParameter) command(c Commander, cmdType, data string) ([]string, error) {
	cmdPrefix := fmt.Sprintf("%s%d", cmdType, p.Oid)
	cmd := cmdPrefix
	if p.Sub != nil {
		cmd += fmt.Sprintf(" %d", *p.Sub)
		if data != "" {
			cmd += ";" + data
		}
	} else if data != "" {
		cmd += " " + data
	}
	resp, err := c.Query(cmd, 0)
	if err != nil {
		return nil, err
	}
	return p.parseResponse(resp, cmdPrefix)
}

func (p *edwardsParameter) Query(c Commander, handler QueryHandler) error {
	if p.Read == "" {
		return fmt.Errorf("no read command for %q", p.Name())
	}
	values, err := p.command(c, p.Read, "")
	if err != nil {
		return err
	}
	if len(values) != len(p.Controls) {
		return errors.New("mismatched number of params in response")
	}
	for n, control := range p.Controls {
		// TODO: perhaps convert values to numbers here
		handler(control.Name, values[n])
	}
	return nil
}

func (p *edwardsParameter) Set(c Commander, name string, value interface{}) error {
	controlIndex := -1
	for n, control := range p.Controls {
		if control.Name == name {
			controlIndex = n
			break
		}
	}
	if controlIndex < 0 {
		return fmt.Errorf("bad control %q for param %q", name, p.Name())
	}
	if p.Write == "" {
		return fmt.Errorf("no write command for %q", p.Name())
	}
	data := ""
	if p.Write == edwardsSetupCommand || (p.Write == edwardsGeneralCommand && p.Sub == nil) {
		data = fmt.Sprintf("%v", value)
	}
	if p.Write == edwardsSetupCommand && len(p.Controls) > 1 {
		// if the parameter is multi-valued, we must read the old values first
		if p.Read == "" {
			return fmt.Errorf("trying to write multi-valued param %q without read command", p.Name())
		}
		values, err := p.command(c, p.Read, "")
		if err != nil {
			return err
		}
		if len(values) != len(p.Controls) {
			return errors.New("mismatched number of params in response")
		}
		values[controlIndex] = data
		data = strings.Join(values, ";")
	}

	values, err := p.command(c, p.Write, data)
	if err != nil {
		return err
	}
	if len(values) > 0 {
		return errors.New("didn't expect values from set command")
	}
	return nil
}

type edwardsProtocol struct {
	idSubstring string
}

func newEdwardsProtocol(config *PortConfig) (Protocol, error) {
	return &edwardsProtocol{config.IdSubstring}, nil
}

func (p *edwardsProtocol) Identify(c Commander) (r string, err error) {
	for i := 0; i < edwardsIdentifyNumAttempts; i++ {
		r, err = c.Query(edwardsIdCommand, 0)
		switch {
		case err == ErrTimeout:
			wbgo.Error.Print("Identify() timeout")
			continue
		case err != nil:
			wbgo.Error.Printf("Identify() error: %v", err)
			return "", err
		case !strings.HasPrefix(r, edwardsIdResponsePrefix) || (p.idSubstring != "" && !strings.Contains(r, p.idSubstring)):
			err = fmt.Errorf("bad id string %q (expected it to contain %q)", r, p.idSubstring)
		default:
			r = r[len(edwardsIdResponsePrefix):]
			return strings.Replace(strings.Replace(r, "\x00", "", -1), ";", "/", -1), nil
		}
	}
	return
}

func (p *edwardsProtocol) Parameter(spec ParameterSpec) (Parameter, error) {
	edwardsSpec, ok := spec.(*edwardsParameterSpec)
	if !ok {
		return nil, errors.New("EDWARDS parameter spec expected")
	}
	return &edwardsParameter{edwardsSpec}, nil
}

func init() {
	RegisterProtocol("edwards", newEdwardsProtocol, &edwardsParameterSpec{})
}

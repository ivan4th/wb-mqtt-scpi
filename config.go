package main

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/go-yaml/yaml"
)

type SetupItem struct {
	Command  string
	Response string
}

// TODO: rename to ControlSpec
type ControlConfig struct {
	Name     string
	Title    string
	Units    string
	Type     string
	Writable bool
	Enum     map[int]string
}

type ParameterSpec interface {
	// ListControls returns the list of controls for this parameter.
	// If multiple parameters refer to the same control, the
	// control definitions are merged together. In this case empty
	// values of Title/Units/Type are ignored in favor of
	// non-empty ones and non-empty ones must not contradict each other.
	// If at least one of such control definitions has true in Writable
	// field, the resulting control is considered writable.
	ListControls() []*ControlConfig
	// ShouldPoll returns true if the parameter should be polled
	ShouldPoll() bool
	// Settable returns true if this parameter can be used for setting values
	// of the controls that have Writable: true
	Settable() bool
	// Validate checks if ParameterSpec is valid and returns an
	// error if it isn't
	Validate() error
}

type PortSettings struct {
	Name  string
	Title string
	Port  string
	// LineEnding can be 'crlf' (default) or 'lf', or empty meaning the default
	// TODO: the default should be taken from the protocol
	LineEnding     string
	IdSubstring    string
	Protocol       string
	Prefix         string // FIXME: use protocol-specific address spec
	CommandDelayMs int
	Setup          []*SetupItem
}

func (s *PortSettings) CommandDelay() time.Duration {
	return time.Duration(s.CommandDelayMs) * time.Millisecond
}

type PortConfig struct {
	*PortSettings
	Parameters []ParameterSpec
}

type DriverConfig struct {
	Ports []*PortConfig
}

type ParameterUnmarshaler func(unmarshal func(interface{}) error) ([]ParameterSpec, error)

func (c *ControlConfig) ShouldPoll() bool {
	return c.Type != "pushbutton"
}

func (c *ControlConfig) TransformDeviceValue(v interface{}) string {
	s := fmt.Sprintf("%v", v)
	if c.Enum == nil {
		return s
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return s
	}
	if name, found := c.Enum[n]; found {
		return name
	}
	return s
}

func (c *ControlConfig) Validate() error {
	if c.Name == "" {
		return errors.New("got control without name")
	}
	// FIXME: should do this validation on merged controls
	// if c.Type == "" {
	// 	return fmt.Errorf("no type specified for control %q", c.Name)
	// }
	return nil
}

func mergeControls(a, b *ControlConfig) (*ControlConfig, error) {
	r := *a
	if a.Name == "" || a.Name != b.Name {
		return nil, errors.New("merge: control names must be the same and non-empty")
	}
	switch {
	case a.Title != "" && b.Title != "" && a.Title != b.Title:
		return nil, fmt.Errorf("merge: title conflict for %q", a.Name)
	case a.Units != "" && b.Units != "" && a.Units != b.Units:
		return nil, fmt.Errorf("merge: units conflict for %q", a.Name)
	case a.Type != "" && b.Type != "" && a.Type != b.Type:
		return nil, fmt.Errorf("merge: type conflict for %q", a.Name)
	}
	if a.Title == "" {
		r.Title = b.Title
	}
	if a.Units == "" {
		r.Units = b.Units
	}
	if a.Type == "" {
		r.Type = b.Type
	}
	if b.Writable {
		r.Writable = true
	}
	if a.Enum == nil {
		r.Enum = b.Enum
	} else if b.Enum != nil {
		return nil, fmt.Errorf("enum conflict for %q", a.Name)
	}
	return &r, nil
}

func (config *PortConfig) GetControls() ([]*ControlConfig, map[string]ParameterSpec, error) {
	var controls []*ControlConfig
	controlMap := make(map[string]*ControlConfig)
	paramSetMap := make(map[string]ParameterSpec)
	for _, param := range config.Parameters {
		for _, control := range param.ListControls() {
			if control.Name == "" {
				return nil, nil, errors.New("Got control without name")
			}
			if control.Type == "pushbutton" {
				// FIXME
				control.Writable = true
			}
			if control.Writable && param.Settable() && paramSetMap[control.Name] == nil {
				paramSetMap[control.Name] = param
			}
			prev, found := controlMap[control.Name]
			if !found {
				controlMap[control.Name] = control
				controls = append(controls, control)
				continue
			}
			merged, err := mergeControls(prev, control)
			if err != nil {
				return nil, nil, err
			}
			*prev = *merged
		}
	}
	return controls, paramSetMap, nil
}

var paramFactories map[string]func() interface{} = make(map[string]func() interface{})

func RegisterProtocolConfig(name string, param ParameterSpec) {
	t := reflect.StructOf([]reflect.StructField{
		{
			Name: "Parameters",
			Type: reflect.SliceOf(reflect.TypeOf(param)),
		},
	})
	paramFactories[name] = func() interface{} {
		return reflect.New(t).Interface()
	}
}

func (config *PortConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var settings PortSettings
	if err := unmarshal(&settings); err != nil {
		return err
	}

	if settings.Protocol == "" {
		return errors.New("must specify the protocol")
	}

	makeParamList, found := paramFactories[settings.Protocol]
	if !found {
		return fmt.Errorf("unknown protocol %q", settings.Protocol)
	}

	paramList := makeParamList()
	if err := unmarshal(paramList); err != nil {
		return fmt.Errorf("error unmarshaling parameters: %v", err)
	}

	config.PortSettings = &settings
	params := reflect.ValueOf(paramList).Elem().FieldByName("Parameters")
	config.Parameters = make([]ParameterSpec, params.Len())
	for i := 0; i < params.Len(); i++ {
		spec := params.Index(i).Interface().(ParameterSpec)
		if err := spec.Validate(); err != nil {
			return err
		}
		config.Parameters[i] = spec
	}

	return nil
}

// TODO: rename back to ParseConfig
func ParseDriverConfig(in []byte) (*DriverConfig, error) {
	var cfg DriverConfig
	err := yaml.Unmarshal(in, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// TODO: see decode_test.go in go-yaml

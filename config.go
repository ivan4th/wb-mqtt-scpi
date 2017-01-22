package main

import (
	"errors"
	"fmt"
	"reflect"
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
	// Settable return true if this parameter can be used for setting values
	// of the controls that have Writable: true
	Settable() bool
}

type PortSettings struct {
	Name           string
	Title          string
	Port           string
	IdSubstring    string
	Protocol       string
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
		config.Parameters[i] = params.Index(i).Interface().(ParameterSpec)
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

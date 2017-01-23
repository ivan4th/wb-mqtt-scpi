package main

import "fmt"

type Commander interface {
	Connect()
	Ready() <-chan struct{}
	Query(query string) (string, error)
}

type QueryHandler func(string, interface{})

type Parameter interface {
	Name() string
	Query(Commander, QueryHandler) error
	Set(Commander, interface{}) error
}

type Protocol interface {
	Identify(Commander) (string, error)
	Parameter(ParameterSpec) (Parameter, error)
}

type ProtocolFactory func(*PortConfig) (Protocol, error)

var protocols map[string]ProtocolFactory = make(map[string]ProtocolFactory)

func RegisterProtocol(name string, factory ProtocolFactory, param ParameterSpec) {
	RegisterProtocolConfig(name, param)
	protocols[name] = factory
}

func CreateProtocol(config *PortConfig) (Protocol, error) {
	factory, found := protocols[config.Protocol]
	if !found {
		return nil, fmt.Errorf("unknown protocol %q", config.Protocol)
	}
	return factory(config)
}

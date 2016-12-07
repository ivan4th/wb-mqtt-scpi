package main

import (
	"github.com/go-yaml/yaml"
)

type ScpiControl struct {
	Name     string
	Title    string
	Units    string
	ScpiName string
	Type     string
	Writable bool
}

type ScpiPortConfig struct {
	Name     string
	Title    string
	Port     string
	Controls []*ScpiControl
}

type ScpiConfig struct {
	Ports []*ScpiPortConfig
}

func ParseConfig(in []byte) (*ScpiConfig, error) {
	var cfg ScpiConfig
	err := yaml.Unmarshal(in, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

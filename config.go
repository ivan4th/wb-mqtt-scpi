package main

import (
	"github.com/go-yaml/yaml"
	"time"
)

type ScpiSetupItem struct {
	Command  string
	Response string
}

type ScpiControl struct {
	Name     string
	Title    string
	Units    string
	ScpiName string
	Type     string
	Writable bool
}

type ScpiPortConfig struct {
	Name           string
	Title          string
	Port           string
	IdSubstring    string
	CommandDelayMs int
	Setup          []*ScpiSetupItem
	Controls       []*ScpiControl
}

func (c *ScpiPortConfig) CommandDelay() time.Duration {
	return time.Duration(c.CommandDelayMs) * time.Millisecond
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

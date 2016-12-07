package main

import "errors"

var (
	errNoPortsDefined = errors.New("no ports defined")
	errNoPortsOpen    = errors.New("couldn't open any ports")
	ErrTimeout        = errors.New("serial timeout")
)

package main

import (
	"github.com/goburrow/serial"
	"io"
	"net"
	"strings"
	"time"
)

const (
	serialTimeout = 250 * time.Millisecond
	tcpTimeout    = 500 * time.Millisecond
)

type serialWrapper struct {
	serial.Port
}

func (w *serialWrapper) Read(b []byte) (n int, err error) {
	if n, err = w.Read(b); err == serial.ErrTimeout {
		err = ErrTimeout
	}
	return
}

func (w *serialWrapper) Write(b []byte) (n int, err error) {
	if n, err = w.Write(b); err == serial.ErrTimeout {
		err = ErrTimeout
	}
	return
}

// func (w *netWrapper) Read(p []byte) ()
func connect(serialAddress string) (io.ReadWriteCloser, error) {
	switch {
	case strings.HasPrefix(serialAddress, "/"):
		if port, err := serial.Open(&serial.Config{
			Address:  serialAddress,
			BaudRate: 9600,
			DataBits: 8,
			StopBits: 1,
			Parity:   "N",
			Timeout:  serialTimeout,
		}); err != nil {
			return nil, err
		} else {
			return &serialWrapper{port}, nil
		}
	case strings.HasPrefix(serialAddress, "tcp://"):
		return net.Dial("tcp", serialAddress[6:])
	}

	return net.Dial("tcp", serialAddress)
}

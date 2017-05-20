package main

import (
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"sync"
	"time"

	"github.com/contactless/wbgo"
)

const (
	// FIXME: don't hardcode these values, use config
	commanderTimeout = 5 * time.Second
	reconnectDelay   = 3 * time.Second
)

type Connector func(port string) (io.ReadWriteCloser, error)

type ConnectionWithDeadline interface {
	SetDeadline(t time.Time) error
}

type connectionWrapper struct {
	*textproto.Conn
	innerConn io.ReadWriteCloser
}

func newConnectionWrapper(conn io.ReadWriteCloser, noLf bool) *connectionWrapper {
	c := conn
	if noLf {
		c = struct {
			io.Reader
			io.Writer
			io.Closer
		}{
			NewAddLfReader(conn),
			NewNoLfWriter(conn),
			conn,
		}
	}
	return &connectionWrapper{textproto.NewConn(c), conn}
}

func (c connectionWrapper) SetDeadline(time time.Time) error {
	if d, ok := c.innerConn.(ConnectionWithDeadline); ok {
		return d.SetDeadline(time)
	}
	return nil
}

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type DefaultClock struct{}

func (c *DefaultClock) Now() time.Time {
	return time.Now()
}

func (c *DefaultClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

var defaultClock = &DefaultClock{}

type DeviceCommander struct {
	sync.Mutex
	settings  *PortSettings
	connector Connector
	readyCh   chan struct{}
	c         *connectionWrapper
	clock     Clock
}

var _ Commander = &DeviceCommander{}

func NewCommander(connector Connector, settings *PortSettings) *DeviceCommander {
	return &DeviceCommander{
		settings:  settings,
		connector: connector,
		readyCh:   make(chan struct{}),
		clock:     defaultClock,
	}
}

func (dc *DeviceCommander) setup(c *connectionWrapper) error {
	for _, si := range dc.settings.Setup {
		// XXX: do getConnection() here and in Query
		// doQuery should accept the connection
		if si.Response == "" {
			if err := dc.sendCommand(c, si.Command); err != nil {
				return err
			}
		} else if r, err := dc.doQuery(c, si.Command); err != nil && r != si.Response {
			return fmt.Errorf("invalid response to %q: %q", r, si.Response)
		}
	}
	return nil
}

func (dc *DeviceCommander) Connect() {
	go func() {
		for {
			wbgo.Debug.Printf("connecting to %s", dc.settings.Port)
			conn, err := dc.connector(dc.settings.Port)
			if err != nil {
				wbgo.Warn.Printf("Commander: error connecting to %q: %v", dc.settings.Port, err)
				<-dc.clock.After(reconnectDelay)
				continue
			}
			dc.Lock()
			defer dc.Unlock()
			dc.c = newConnectionWrapper(conn, dc.settings.LineEnding == "cr")
			wbgo.Debug.Printf("connected to %s", dc.settings.Port)
			// TODO: avoid holding mutex during 'setup'. We need state machine
			// (CommanderState interface)
			dc.setup(dc.c)
			wbgo.Debug.Printf("setup done for %s", dc.settings.Port)
			close(dc.readyCh)
			break
		}
	}()
}

func (dc *DeviceCommander) reconnect() {
	dc.Lock()
	defer dc.Unlock()
	wbgo.Debug.Printf("initiating reconnect\n")
	if dc.c != nil {
		if err := dc.c.Close(); err != nil {
			wbgo.Error.Printf("Error closing the connection: %v", err)
		}
		dc.c = nil
	}
	dc.readyCh = make(chan struct{})
	dc.Connect()
}

func (dc *DeviceCommander) Ready() <-chan struct{} {
	dc.Lock()
	defer dc.Unlock()
	return dc.readyCh
}

func (dc *DeviceCommander) SetClock(clock Clock) {
	dc.clock = clock
}

func (dc *DeviceCommander) getConnection() (*connectionWrapper, error) {
	dc.Lock()
	defer dc.Unlock()
	if dc.c == nil {
		return nil, errors.New("not connected")
	}
	return dc.c, nil
}

func (dc *DeviceCommander) Query(query string) (string, error) {
	wbgo.Debug.Printf("Query: %q", query)
	c, err := dc.getConnection()
	if err != nil {
		return "", err
	}

	r, err := dc.doQuery(c, query)
	if err == nil || err == ErrTimeout {
		return r, err
	}
	dc.reconnect()
	return "", err
}

func (dc *DeviceCommander) maybeDelay() {
	if dc.settings.CommandDelayMs > 0 {
		<-dc.clock.After(dc.settings.CommandDelay())
	}
}

func (dc *DeviceCommander) sendCommand(c *connectionWrapper, cmd string) error {
	cmd = dc.settings.Prefix + cmd
	wbgo.Debug.Printf("sendCommand: %q", cmd)
	dc.maybeDelay()
	id, err := c.Cmd(cmd)
	if err != nil {
		wbgo.Debug.Printf("sendCommand: Cmd err: %v", err)
		return err
	}
	c.StartResponse(id)
	c.EndResponse(id)
	return nil
}

func (dc *DeviceCommander) doQuery(c *connectionWrapper, query string) (string, error) {
	dc.maybeDelay()
	query = dc.settings.Prefix + query
	id, err := c.Cmd(query)
	if err != nil {
		wbgo.Debug.Printf("Query: Cmd err: %v", err)
		return "", err
	}
	wbgo.Debug.Printf("Query: waiting for response")
	c.StartResponse(id)
	defer c.EndResponse(id)
	if err := c.SetDeadline(dc.clock.Now().Add(commanderTimeout)); err != nil {
		wbgo.Debug.Printf("Query: SetDeadline error: %v", err)
		return "", fmt.Errorf("SetDeadline error: %v", err)
	}
	r, err := c.ReadLine()
	if err != nil {
		wbgo.Debug.Printf("Query: error: %v", err)
		return "", err
	}
	wbgo.Debug.Printf("Query: response: %q", r)
	return r, nil
}

type CommanderFactory func(*PortSettings) Commander

func DefaultCommanderFactory(connector Connector) CommanderFactory {
	return func(portSettings *PortSettings) Commander {
		return NewCommander(connector, portSettings)
	}
}

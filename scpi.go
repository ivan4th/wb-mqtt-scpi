package main

import (
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/contactless/wbgo"
)

const (
	// FIXME: don't hardcode these values, use config
	scpiTimeout             = 5 * time.Second
	scpiIdentifyNumAttempts = 10
	reconnectDelay          = 3 * time.Second
)

type Connector func(port string) (io.ReadWriteCloser, error)

type ConnectionWithDeadline interface {
	SetDeadline(t time.Time) error
}

type connectionWrapper struct {
	*textproto.Conn
	innerConn io.ReadWriteCloser
}

func newConnectionWrapper(conn io.ReadWriteCloser) *connectionWrapper {
	return &connectionWrapper{textproto.NewConn(conn), conn}
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

type Scpi struct {
	sync.Mutex
	port        string
	connector   Connector
	readyCh     chan struct{}
	c           *connectionWrapper
	clock       Clock
	idSubstring string
	setupItems  []*ScpiSetupItem
	delay       time.Duration
}

func NewScpi(connector Connector, port, idSubstring string, setupItems []*ScpiSetupItem, delay time.Duration) *Scpi {
	return &Scpi{
		port:        port,
		connector:   connector,
		readyCh:     make(chan struct{}),
		clock:       defaultClock,
		idSubstring: idSubstring,
		setupItems:  setupItems,
		delay:       delay,
	}
}

func (s *Scpi) setup(c *connectionWrapper) error {
	for _, si := range s.setupItems {
		// XXX: do getConnection() here and in Query
		// doQuery should accept the connection
		if si.Response == "" {
			if err := s.sendCommand(c, si.Command); err != nil {
				return err
			}
		} else if r, err := s.doQuery(c, si.Command); err != nil && r != si.Response {
			return fmt.Errorf("invalid response to %q: %q", r, si.Response)
		}
	}
	return nil
}

func (s *Scpi) Connect() {
	go func() {
		for {
			wbgo.Debug.Printf("connecting to %s", s.port)
			conn, err := s.connector(s.port)
			if err != nil {
				wbgo.Warn.Printf("Scpi: error connecting to %q: %v", s.port, err)
				<-s.clock.After(reconnectDelay)
				continue
			}
			s.Lock()
			defer s.Unlock()
			s.c = newConnectionWrapper(conn)
			wbgo.Debug.Printf("connected to %s", s.port)
			// TODO: avoid holding mutex during 'setup'. We need state machine
			// (ScpiState interface)
			s.setup(s.c)
			wbgo.Debug.Printf("setup done for %s", s.port)
			close(s.readyCh)
			break
		}
	}()
}

func (s *Scpi) reconnect() {
	s.Lock()
	defer s.Unlock()
	wbgo.Debug.Printf("initiating reconnect\n")
	if s.c != nil {
		if err := s.c.Close(); err != nil {
			wbgo.Error.Printf("Error closing the connection: %v", err)
		}
		s.c = nil
	}
	s.readyCh = make(chan struct{})
	s.Connect()
}

func (s *Scpi) Ready() chan struct{} {
	s.Lock()
	defer s.Unlock()
	return s.readyCh
}

func (s *Scpi) SetClock(clock Clock) {
	s.clock = clock
}

func (s *Scpi) Identify() (r string, err error) {
	for i := 0; i < scpiIdentifyNumAttempts; i++ {
		r, err = s.Query("*IDN?")
		switch {
		case err == ErrTimeout:
			wbgo.Error.Print("Identify() timeout")
			continue
		case err != nil:
			wbgo.Error.Printf("Identify() error: %v", err)
			return "", err
		case s.idSubstring != "" && !strings.Contains(r, s.idSubstring):
			err = fmt.Errorf("bad id string %q (expected it to contain %q)", r, s.idSubstring)
		default:
			return r, nil
		}
	}
	return
}

func (s *Scpi) getConnection() (*connectionWrapper, error) {
	s.Lock()
	defer s.Unlock()
	if s.c == nil {
		return nil, errors.New("not connected")
	}
	return s.c, nil
}

func (s *Scpi) Query(query string) (string, error) {
	wbgo.Debug.Printf("Query: %q", query)
	c, err := s.getConnection()
	if err != nil {
		return "", err
	}

	r, err := s.doQuery(c, query)
	if err == nil || err == ErrTimeout {
		return r, err
	}
	s.reconnect()
	return "", err
}

func (s *Scpi) maybeDelay() {
	if s.delay > 0 {
		<-s.clock.After(s.delay)
	}
}

func (s *Scpi) sendCommand(c *connectionWrapper, cmd string) error {
	wbgo.Debug.Printf("sendCommand: %q", cmd)
	s.maybeDelay()
	id, err := c.Cmd(cmd)
	if err != nil {
		wbgo.Debug.Printf("sendCommand: Cmd err: %v", err)
		return err
	}
	c.StartResponse(id)
	c.EndResponse(id)
	return nil
}

func (s *Scpi) doQuery(c *connectionWrapper, query string) (string, error) {
	s.maybeDelay()
	id, err := c.Cmd(query)
	if err != nil {
		wbgo.Debug.Printf("Query: Cmd err: %v", err)
		return "", err
	}
	wbgo.Debug.Printf("Query: waiting for response")
	c.StartResponse(id)
	defer c.EndResponse(id)
	if err := c.SetDeadline(s.clock.Now().Add(scpiTimeout)); err != nil {
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

func (s *Scpi) Set(cmd string, param string) error {
	if r, err := s.Query(fmt.Sprintf("%s %s; *OPC?", cmd, param)); err != nil {
		return err
	} else if r != "1" {
		return fmt.Errorf("unexpected set response %q", r)
	}
	return nil
}

// TODO: try to retrieve the error

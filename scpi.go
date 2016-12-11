package main

import (
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"time"

	"github.com/contactless/wbgo"
)

const (
	// FIXME: don't hardcode these values, use config
	scpiTimeout             = 5 * time.Second
	scpiIdentifyNumAttempts = 10
)

type ConnectionWithDeadline interface {
	SetDeadline(t time.Time) error
}

type dummyDeadline struct{}

func (d dummyDeadline) SetDeadline(time time.Time) error {
	return nil
}

type Scpi struct {
	c           *textproto.Conn
	timeFunc    func() time.Time
	d           ConnectionWithDeadline
	idSubstring string
}

func NewScpi(conn io.ReadWriteCloser, idSubstring string) *Scpi {
	d, ok := conn.(ConnectionWithDeadline)
	if !ok {
		d = dummyDeadline{}
	}
	return &Scpi{
		c:           textproto.NewConn(conn),
		timeFunc:    time.Now,
		d:           d,
		idSubstring: idSubstring,
	}
}

func (s *Scpi) SetTimeFunc(timeFunc func() time.Time) {
	s.timeFunc = timeFunc
}

func (s *Scpi) Identify() (r string, err error) {
	for i := 0; i < scpiIdentifyNumAttempts; i++ {
		r, err = s.Query("*IDN?")
		if err != nil {
			wbgo.Error.Printf("Identify() error: %v", err)
			continue
		}
		if s.idSubstring != "" && !strings.Contains(r, s.idSubstring) {
			err = fmt.Errorf("bad id string %q (expected it to contain %q)", r, s.idSubstring)
		} else {
			break
		}
	}
	return
}

func (s *Scpi) Query(query string) (string, error) {
	wbgo.Debug.Printf("Query: %q", query)
	id, err := s.c.Cmd(query)
	if err != nil {
		wbgo.Debug.Printf("Query: Cmd err: %v", err)
		return "", err
	}
	wbgo.Debug.Printf("Query: waiting for response")
	s.c.StartResponse(id)
	defer s.c.EndResponse(id)
	if err := s.d.SetDeadline(s.timeFunc().Add(scpiTimeout)); err != nil {
		wbgo.Debug.Printf("Query: SetDeadline error: %v", err)
		return "", fmt.Errorf("SetDeadline error: %v", err)
	}
	r, err := s.c.ReadLine()
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

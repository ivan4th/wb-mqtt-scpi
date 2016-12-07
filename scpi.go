package main

import (
	"github.com/contactless/wbgo"
	"io"
	"net/textproto"
)

type Scpi struct {
	c *textproto.Conn
}

func NewScpi(conn io.ReadWriteCloser) *Scpi {
	return &Scpi{textproto.NewConn(conn)}
}

func (s *Scpi) Identify() (string, error) {
	return s.Query("*IDN?")
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
	r, err := s.c.ReadLine()
	if err != nil {
		wbgo.Debug.Printf("Query: error: %v", err)
		return "", err
	}
	wbgo.Debug.Printf("Query: response: %q", r)
	return r, nil
}

func (s *Scpi) Set(cmd string, param string) error {
	wbgo.Debug.Printf("Set: %q %q", cmd, param)
	id, err := s.c.Cmd(cmd + " " + param)
	s.c.StartResponse(id)
	// no response to 'set' command
	s.c.EndResponse(id)
	if err != nil {
		wbgo.Debug.Printf("Set: error: %v", err)
		return err
	}
	wbgo.Debug.Printf("Set finished")
	return nil
}

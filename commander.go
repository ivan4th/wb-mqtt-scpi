package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
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
	*bufio.ReadWriter
	innerConn io.ReadWriteCloser
}

func newConnectionWrapper(conn io.ReadWriteCloser) *connectionWrapper {
	return &connectionWrapper{bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)), conn}
}

func (c connectionWrapper) SetDeadline(time time.Time) error {
	if d, ok := c.innerConn.(ConnectionWithDeadline); ok {
		return d.SetDeadline(time)
	}
	return nil
}

func (c connectionWrapper) Close() error {
	return c.innerConn.Close()
}

func (c connectionWrapper) sendCommand(command, lineEnding string, readResponse bool, now time.Time) (string, error) {
	wbgo.Debug.Printf("sendCommand: %q", command)
	if err := c.SetDeadline(now.Add(commanderTimeout)); err != nil {
		wbgo.Debug.Printf("Query: SetDeadline error: %v", err)
		return "", fmt.Errorf("SetDeadline error: %v", err)
	}

	_, err := c.Write([]byte(command + lineEnding))
	if err == nil {
		err = c.Flush()
	}
	if err != nil {
		return "", fmt.Errorf("write error: %v", err)
	}
	if !readResponse {
		return "", nil
	}

	lastChar := lineEnding[len(lineEnding)-1]
	resp, err := c.ReadString(lastChar)
	if err == ErrTimeout {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}
	wbgo.Debug.Printf("sendCommand: resp for %q: %#v", command, resp)
	switch {
	case len(resp) >= len(lineEnding) && resp[len(resp)-len(lineEnding):] == lineEnding:
		return resp[:len(resp)-len(lineEnding)], nil
	case resp[len(resp)-1] == lastChar:
		// allow responses to cmd + "\r\n" to end with just "\n"
		return resp[:len(resp)-1], nil
	default:
		return resp, nil
	}
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

type commandItem struct {
	command    string
	errCh      chan error
	responseCh chan string
}

type commanderState interface {
	Enter(dc *DeviceCommander) commanderState
	Timeout(dc *DeviceCommander) commanderState
	Connect(dc *DeviceCommander) commanderState
	Disconnect(dc *DeviceCommander) commanderState
	CommandFailed(dc *DeviceCommander) commanderState
	Connected(dc *DeviceCommander, c *connectionWrapper) commanderState
	ConnectFailed(dc *DeviceCommander) commanderState
	Command(dc *DeviceCommander, item *commandItem) commanderState
	CommandFinished(dc *DeviceCommander) commanderState
}

type commanderStateBase struct{}

var _ commanderState = &commanderStateBase{}

func (s *commanderStateBase) Enter(dc *DeviceCommander) commanderState         { return nil }
func (s *commanderStateBase) Timeout(dc *DeviceCommander) commanderState       { return nil }
func (s *commanderStateBase) Connect(dc *DeviceCommander) commanderState       { return nil }
func (s *commanderStateBase) Disconnect(dc *DeviceCommander) commanderState    { return nil }
func (s *commanderStateBase) CommandFailed(dc *DeviceCommander) commanderState { return nil }
func (s *commanderStateBase) Connected(dc *DeviceCommander, c *connectionWrapper) commanderState {
	c.Close()
	return nil
}
func (s *commanderStateBase) ConnectFailed(dc *DeviceCommander) commanderState { return nil }
func (d *commanderStateBase) Command(dc *DeviceCommander, item *commandItem) commanderState {
	go func() {
		item.errCh <- errors.New("not connected")
	}()
	return nil
}
func (d *commanderStateBase) CommandFinished(dc *DeviceCommander) commanderState { return nil }

type commanderStateOffline struct{ commanderStateBase }

func (s *commanderStateOffline) Connect(dc *DeviceCommander) commanderState {
	return &commanderStateConnecting{}
}

type commanderStateConnecting struct {
	commanderStateBase
	stopCh chan struct{}
	doneCh chan struct{}
}

func (s *commanderStateConnecting) Enter(dc *DeviceCommander) commanderState {
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	go func() {
		defer close(s.doneCh)
		wbgo.Debug.Printf("connecting to %s", dc.settings.Port)
		connCh := make(chan io.ReadWriteCloser)
		errCh := make(chan error)
		go func() {
			conn, err := dc.connector(dc.settings.Port)
			if err != nil {
				errCh <- err
			} else {
				connCh <- conn
			}
		}()
		var conn io.ReadWriteCloser
		select {
		case <-s.stopCh:
			select {
			case conn := <-connCh:
				conn.Close()
			case <-errCh:
			}
		case err := <-errCh:
			wbgo.Warn.Printf("Commander: error connecting to %q: %v", dc.settings.Port, err)
			dc.stateAction(func(s commanderState) commanderState { return s.ConnectFailed(dc) })
			return
		case conn = <-connCh:
		}

		wbgo.Debug.Printf("connected to %s", dc.settings.Port)
		wrapper := newConnectionWrapper(conn)
		go func() {
			errCh <- dc.setup(wrapper)
		}()
		select {
		case <-s.stopCh:
			<-errCh
			wrapper.Close()
		case err := <-errCh:
			if err != nil {
				wbgo.Warn.Printf("Commander: setup failed for %q: %v", dc.settings.Port, err)
				wrapper.Close()
				dc.stateAction(func(s commanderState) commanderState {
					return s.ConnectFailed(dc)
				})
				return
			}
		}
		wbgo.Debug.Printf("setup done for %s", dc.settings.Port)
		dc.stateAction(func(s commanderState) commanderState {
			return s.Connected(dc, wrapper)
		})
	}()

	return nil
}

func (s *commanderStateConnecting) Connected(dc *DeviceCommander, c *connectionWrapper) commanderState {
	dc.c = c
	return &commanderStateOnline{}
}

func (s *commanderStateConnecting) ConnectFailed(dc *DeviceCommander) commanderState {
	return &commanderStateReconnect{}
}

func (s *commanderStateConnecting) Disconnect(dc *DeviceCommander) commanderState {
	close(s.stopCh)
	<-s.doneCh
	return &commanderStateOffline{}
}

type commanderStateReconnect struct {
	commanderStateBase
	stopCh chan struct{}
}

func (s *commanderStateReconnect) Enter(dc *DeviceCommander) commanderState {
	// acquire delay channel synchronously because this makes the tests easier
	s.stopCh = make(chan struct{})
	afterCh := dc.clock.After(reconnectDelay)
	go func() {
		select {
		case <-s.stopCh:
		case <-afterCh:
			dc.stateAction(func(s commanderState) commanderState {
				return s.Timeout(dc)
			})
		}
	}()
	return nil
}

func (s *commanderStateReconnect) Timeout(dc *DeviceCommander) commanderState {
	return &commanderStateConnecting{}
}

func (s *commanderStateReconnect) Disconnect(dc *DeviceCommander) commanderState {
	close(s.stopCh)
	return &commanderStateOffline{}
}

type commanderStateOnline struct {
	commanderStateBase
}

func (s *commanderStateOnline) Enter(dc *DeviceCommander) commanderState {
	for _, ch := range dc.readyChs {
		close(ch)
	}
	dc.readyChs = nil
	return nil
}

func (s *commanderStateOnline) Command(dc *DeviceCommander, item *commandItem) commanderState {
	return &commanderStateBusy{queue: []*commandItem{item}}
}

func (s *commanderStateOnline) Disconnect(dc *DeviceCommander) commanderState {
	if err := dc.c.Close(); err != nil {
		wbgo.Error.Printf("Error closing the connection: %v", err)
	}
	dc.c = nil
	return &commanderStateOffline{}
}

type commanderStateBusy struct {
	commanderStateBase
	queue  []*commandItem
	stopCh chan struct{}
	doneCh chan struct{}
}

func (s *commanderStateBusy) send(dc *DeviceCommander) commanderState {
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	item := s.queue[0]
	c := dc.c
	go func() {
		defer close(s.doneCh)
		errCh := make(chan error)
		respCh := make(chan string)
		go func() {
			resp, err := c.sendCommand(dc.settings.Prefix+item.command, dc.lineEnding(), true, dc.clock.Now())
			if err != nil {
				errCh <- err
			} else {
				respCh <- resp
			}
		}()
		select {
		case <-s.stopCh:
			item.errCh <- errors.New("disconnect requested")
			return
		case resp := <-respCh:
			item.responseCh <- resp
			dc.stateAction(func(s commanderState) commanderState {
				return s.CommandFinished(dc)
			})
		case err := <-errCh:
			wbgo.Error.Printf("Error executing the command: %v", err)
			// let the following happen after s.doneCh is closed
			go func() {
				if err == ErrTimeout {
					dc.stateAction(func(s commanderState) commanderState {
						return s.CommandFinished(dc)
					})
				} else {
					dc.stateAction(func(s commanderState) commanderState {
						return s.CommandFailed(dc)
					})
				}
				item.errCh <- err
			}()
			return
		}
	}()
	return nil
}

func (s *commanderStateBusy) Enter(dc *DeviceCommander) commanderState {
	if dc.settings.CommandDelayMs > 0 {
		go func() {
			select {
			case <-dc.clock.After(dc.settings.CommandDelay()):
				s.send(dc)
			case <-s.stopCh:
				close(s.doneCh)
			}
		}()
	} else {
		s.send(dc)
	}
	return nil
}

func (s *commanderStateBusy) Timeout(dc *DeviceCommander) commanderState {
	s.send(dc)
	return nil
}

func (s *commanderStateBusy) Disconnect(dc *DeviceCommander) commanderState {
	close(s.stopCh)
	<-s.doneCh
	if err := dc.c.Close(); err != nil {
		wbgo.Error.Printf("Error closing the connection: %v", err)
	}
	dc.c = nil
	return &commanderStateOffline{}
}

func (s *commanderStateBusy) Command(dc *DeviceCommander, item *commandItem) commanderState {
	s.queue = append(s.queue, item)
	return nil
}

func (s *commanderStateBusy) CommandFinished(dc *DeviceCommander) commanderState {
	if len(s.queue) == 1 {
		return &commanderStateOnline{}
	} else {
		return &commanderStateBusy{queue: s.queue[1:]}
	}
}

func (s *commanderStateBusy) CommandFailed(dc *DeviceCommander) commanderState {
	close(s.stopCh)
	<-s.doneCh
	if err := dc.c.Close(); err != nil {
		wbgo.Error.Printf("Error closing the connection: %v", err)
	}
	dc.c = nil
	return &commanderStateReconnect{}
}

type DeviceCommander struct {
	sync.Mutex
	settings  *PortSettings
	connector Connector
	readyChs  []chan struct{}
	c         *connectionWrapper
	clock     Clock
	state     commanderState
}

var _ Commander = &DeviceCommander{}

func NewCommander(connector Connector, settings *PortSettings) *DeviceCommander {
	dc := &DeviceCommander{
		settings:  settings,
		connector: connector,
		clock:     defaultClock,
	}
	dc.enterState(&commanderStateOffline{})
	return dc
}

func (dc *DeviceCommander) enterState(state commanderState) {
	for state != nil {
		wbgo.Debug.Printf("DebugCommander.enterState(): %T -> %T", dc.state, state)
		dc.state = state
		state = state.Enter(dc)
	}
}

func (dc *DeviceCommander) stateAction(thunk func(state commanderState) commanderState) {
	dc.Lock()
	defer dc.Unlock()
	dc.enterState(thunk(dc.state))
}

func (dc *DeviceCommander) setup(c *connectionWrapper) error {
	for _, si := range dc.settings.Setup {
		resp, err := c.sendCommand(dc.settings.Prefix+si.Command, dc.lineEnding(), si.Response != "", dc.clock.Now())
		if err != nil {
			return err
		}
		if si.Response != "" && resp != si.Response {
			return fmt.Errorf("invalid response to %q: %q", resp, si.Response)
		}
	}
	wbgo.Debug.Printf("setup done for %s", dc.settings.Port)
	return nil
}

func (dc *DeviceCommander) lineEnding() string {
	switch dc.settings.LineEnding {
	case "cr":
		return "\r"
	case "lf":
		return "\n"
	case "":
		fallthrough
	case "crlf":
		return "\r\n"
	default:
		panic("bad line ending spec: " + dc.settings.LineEnding)
	}
}

func (dc *DeviceCommander) Connect() {
	dc.stateAction(func(s commanderState) commanderState { return s.Connect(dc) })
}

func (dc *DeviceCommander) Ready() <-chan struct{} {
	dc.Lock()
	defer dc.Unlock()
	ch := make(chan struct{})
	if dc.c == nil {
		dc.readyChs = append(dc.readyChs, ch)
	} else {
		close(ch)
	}
	return ch
}

func (dc *DeviceCommander) SetClock(clock Clock) {
	dc.clock = clock
}

func (dc *DeviceCommander) Query(query string) (string, error) {
	item := &commandItem{
		command:    query,
		errCh:      make(chan error, 1),
		responseCh: make(chan string, 1),
	}
	dc.stateAction(func(s commanderState) commanderState { return s.Command(dc, item) })
	select {
	case err := <-item.errCh:
		return "", err
	case resp := <-item.responseCh:
		return resp, nil
	}
}

func (dc *DeviceCommander) Close() {
	dc.stateAction(func(s commanderState) commanderState { return s.Disconnect(dc) })
	dc.Lock()
	for dc.c != nil {
		dc.Unlock()
		time.Sleep(100 * time.Millisecond)
		dc.Lock()
	}
	dc.Unlock()
}

func (dc *DeviceCommander) maybeDelay() {
	if dc.settings.CommandDelayMs > 0 {
		<-dc.clock.After(dc.settings.CommandDelay())
	}
}

type CommanderFactory func(*PortSettings) Commander

func DefaultCommanderFactory(connector Connector) CommanderFactory {
	return func(portSettings *PortSettings) Commander {
		return NewCommander(connector, portSettings)
	}
}

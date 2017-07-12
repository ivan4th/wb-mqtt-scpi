package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"sync"
	"testing"
	"time"
)

const (
	samplePort = "someport"
)

type fakeClockDeadline struct {
	time time.Time
	ch   chan time.Time
}

type fakeClock struct {
	sync.Mutex
	time      time.Time
	deadlines []fakeClockDeadline
}

func newFakeClock() *fakeClock {
	return &fakeClock{time: time.Now()}
}

func (c *fakeClock) Now() time.Time {
	c.Lock()
	defer c.Unlock()
	return c.time
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.Lock()
	defer c.Unlock()
	t := c.time.Add(d)
	ch := make(chan time.Time)
	c.deadlines = append(c.deadlines, fakeClockDeadline{t, ch})
	return ch
}

func (c *fakeClock) elapse(d time.Duration) {
	c.Lock()
	defer c.Unlock()
	c.time = c.time.Add(d)
	deadlines := c.deadlines
	c.deadlines = []fakeClockDeadline{}
	for _, d := range deadlines {
		if !d.time.After(c.time) {
			close(d.ch)
		} else {
			c.deadlines = append(c.deadlines, d)
		}
	}
}

type fakeConnection struct {
	io.Reader
	io.Writer
	io.Closer
	deadline, readTime time.Time
	pendingError       error
	closed             bool
}

func (fc *fakeConnection) SetDeadline(time time.Time) error {
	fc.deadline = time
	return nil
}

func (fc *fakeConnection) Write(p []byte) (n int, err error) {
	if fc.pendingError != nil {
		err = fc.pendingError
		fc.pendingError = nil
		return
	}
	return fc.Writer.Write(p)
}

func (fc *fakeConnection) Read(p []byte) (n int, err error) {
	if fc.pendingError != nil {
		err = fc.pendingError
		fc.pendingError = nil
		return
	}
	if fc.readTime.After(fc.deadline) {
		return 0, ErrTimeout
	}
	return fc.Reader.Read(p)
}

func (fc *fakeConnection) Close() error {
	if err := fc.Closer.Close(); err != nil {
		return err
	}
	fc.closed = true
	return nil
}

type cmdTester struct {
	*fakeClock
	t              *testing.T
	ourInnerReader *io.PipeReader
	ourReader      *bufio.Reader
	ourWriter      *io.PipeWriter
	fc             *fakeConnection
	connectCount   int
	connectPort    string
	connectCh      chan struct{}
	lineEnding     string
}

func newCmdTester(t *testing.T, connectPort string) *cmdTester {
	return &cmdTester{
		fakeClock:   newFakeClock(),
		t:           t,
		connectPort: connectPort,
		connectCh:   make(chan struct{}, 100),
		lineEnding:  "\r\n",
	}
}

func (tester *cmdTester) expectCommands(cmds []string) string {
	var l string
	errCh := make(chan error)
	go func() {
		var err error
		lastCh := tester.lineEnding[len(tester.lineEnding)-1]
		l, err = tester.ourReader.ReadString(lastCh)
		errCh <- err
	}()
	select {
	case <-time.After(30 * time.Second):
		tester.t.Fatalf("timed out waiting for command(s): %v", cmds)
	case err := <-errCh:
		if err != nil {
			tester.t.Fatalf("failed to read the command(s), expected: %v", cmds)
		}
	}
	for _, cmd := range cmds {
		if l == cmd+tester.lineEnding {
			return cmd
		}
	}
	tester.t.Fatalf("invalid command: %#v (expected one of %#v)", l, cmds)
	return ""
}

func (tester *cmdTester) expectCommand(cmd string) {
	tester.expectCommands([]string{cmd})
}

func (tester *cmdTester) writeResponse(response string) {
	if _, err := tester.ourWriter.Write([]byte(response + tester.lineEnding)); err != nil {
		tester.t.Fatalf("Write failed: %v", err)
	}
}

func (tester *cmdTester) writeResponseWithoutLineEnding(response string) {
	if _, err := tester.ourWriter.Write([]byte(response)); err != nil {
		tester.t.Fatalf("Write failed: %v", err)
	}
}

func (tester *cmdTester) respondToCommand(response string, ch chan string) {
	tester.writeResponse(response)
	result := <-ch
	if result != response {
		tester.t.Fatalf("bad result: %#v instead of %#v", result, response)
	}
}

func (tester *cmdTester) chat(cmd, response string, thunk func() (string, error)) {
	ch := make(chan string)
	go func() {
		if r, err := thunk(); err != nil {
			log.Panicf("failed to invoke command: %v", err)
		} else {
			ch <- r
		}
	}()
	tester.expectCommand(cmd)
	if response != "" {
		tester.respondToCommand(response, ch)
	}
}

func (tester *cmdTester) simpleChat(cmd, response string) {
	tester.expectCommand(cmd)
	if response != "" {
		tester.writeResponse(response)
	}
}

func (tester *cmdTester) unorderedChat(cmdsAndResponses map[string]string) {
	gotCmds := make(map[string]bool)
	cmds := make([]string, len(cmdsAndResponses))
	n := 0
	for cmd, _ := range cmdsAndResponses {
		cmds[n] = cmd
		n++
	}
	sort.Strings(cmds)
	for n := 0; n < len(cmdsAndResponses); n++ {
		cmd := tester.expectCommands(cmds)
		if gotCmds[cmd] {
			tester.t.Fatalf("duplicate command %q", cmd)
		}
		gotCmds[cmd] = true
		if cmdsAndResponses[cmd] != "" {
			tester.writeResponse(cmdsAndResponses[cmd])
		}
	}
}

func (tester *cmdTester) acceptSetCommand(cmd, response string, thunk func() error) {
	errCh := make(chan error)
	go func() {
		errCh <- thunk()
	}()
	tester.expectCommand(cmd)
	tester.ourWriter.Write([]byte(response + tester.lineEnding))
	if err := <-errCh; err != nil {
		tester.t.Fatalf("failed to invoke command: %v", err)
	}
}

func (tester *cmdTester) connect(port string) (io.ReadWriteCloser, error) {
	if port != tester.connectPort {
		log.Panicf("bad connect() port: %q instead of %q", port, tester.connectPort)
	}
	ourInnerReader, theirWriter := io.Pipe()
	theirReader, ourWriter := io.Pipe()
	tester.ourInnerReader = ourInnerReader
	tester.ourReader = bufio.NewReader(ourInnerReader)
	tester.ourWriter = ourWriter
	tester.fc = &fakeConnection{
		Reader: theirReader,
		Writer: theirWriter,
		Closer: theirWriter,
	}
	tester.connectCount++
	tester.connectCh <- struct{}{}

	return tester.fc, nil
}

func (tester *cmdTester) verifyConnectCount(expectedCount int) {
	if tester.connectCount != expectedCount {
		tester.t.Errorf("Invalid connect count, expected %d but got %d", expectedCount, tester.connectCount)
	}
}

func (tester *cmdTester) close() {
	tester.ourInnerReader.Close()
	tester.ourWriter.Close()
}

func TestCommander(t *testing.T) {
	tester := newCmdTester(t, samplePort)
	commander := NewCommander(tester.connect, &PortSettings{Port: samplePort})
	commander.Connect()
	<-commander.Ready()
	commander.SetClock(tester)
	tester.chat("*IDN?", "IZNAKURNOZH", func() (string, error) {
		return commander.Query("*IDN?", 0)
	})
	tester.chat("CURR?", "3.500", func() (string, error) {
		return commander.Query("CURR?", 0)
	})
	tester.chat("CURR 3.4; *OPC?", "1", func() (string, error) {
		return commander.Query("CURR 3.4; *OPC?", 0)
	})
	// make sure setting the value didn't break DeviceCommander
	tester.chat("CURR?", "3.400", func() (string, error) {
		return commander.Query("CURR?", 0)
	})

	tester.fc.readTime = tester.time.Add(10 * time.Second)
	errCh := make(chan error)
	go func() {
		_, err := commander.Query("CURR?", 0)
		errCh <- err
	}()
	if _, err := tester.ourReader.ReadString('\n'); err != nil {
		t.Fatalf("failed to read the command: %v", err)
	}
	if err := <-errCh; err != ErrTimeout {
		t.Errorf("unexpected error value: %#v (expected ErrTimeout)", err)
	}

	tester.fc.readTime = tester.time

	// make sure things didn't break, again
	tester.chat("CURR?", "3.400", func() (string, error) {
		return commander.Query("CURR?", 0)
	})
}

func TestCommanderSetup(t *testing.T) {
	tester := newCmdTester(t, samplePort)
	commander := NewCommander(tester.connect, &PortSettings{
		Port: samplePort,
		Setup: []*SetupItem{
			{
				Command: ":SYST:REM",
			},
			{
				Command:  "WHATEVER",
				Response: "ORLY",
			},
		},
	})
	commander.SetClock(tester)
	commander.Connect()
	<-tester.connectCh
	tester.expectCommand(":SYST:REM")
	tester.expectCommand("WHATEVER")
	tester.writeResponse("ORLY")
	<-commander.Ready()
	tester.chat("*IDN?", "IZNAKURNOZH", func() (string, error) {
		return commander.Query("*IDN?", 0)
	})
}

func TestReconnect(t *testing.T) {
	tester := newCmdTester(t, samplePort)
	commander := NewCommander(tester.connect, &PortSettings{Port: samplePort})
	commander.SetClock(tester)
	commander.Connect()
	readyCh := commander.Ready()
	<-readyCh
	tester.verifyConnectCount(1)
	oldFc := tester.fc
	tester.chat("*IDN?", "IZNAKURNOZH", func() (string, error) {
		return commander.Query("*IDN?", 0)
	})
	tester.fc.pendingError = errors.New("oops")
	if _, err := commander.Query("*IDN?", 0); err == nil {
		t.Errorf("Identify() didn't return the expected error")
	}

	tester.elapse(10 * time.Second)
	newReadyCh := commander.Ready()
	if newReadyCh == readyCh {
		t.Fatalf("readyCh didn't change")
	}
	<-newReadyCh
	tester.verifyConnectCount(2)
	if !oldFc.closed {
		t.Errorf("The old connection was not closed")
	}
	tester.chat("*IDN?", "IZNAKURNOZH", func() (string, error) {
		return commander.Query("*IDN?", 0)
	})
}

func TestAltLineEnding(t *testing.T) {
	tester := newCmdTester(t, samplePort)
	tester.lineEnding = "\r"
	commander := NewCommander(tester.connect, &PortSettings{Port: samplePort, LineEnding: "cr"})
	commander.Connect()
	<-commander.Ready()
	commander.SetClock(tester)
	tester.chat("*IDN?", "IZNAKURNOZH", func() (string, error) {
		return commander.Query("*IDN?", 0)
	})
}

func TestFixedSizeResponse(t *testing.T) {
	tester := newCmdTester(t, samplePort)
	commander := NewCommander(tester.connect, &PortSettings{Port: samplePort})
	commander.Connect()
	<-commander.Ready()
	commander.SetClock(tester)

	ch := make(chan string)
	for i := 0; i < 3; i++ {
		go func() {
			if r, err := commander.Query("FOOBAR", 8); err != nil {
				log.Panicf("failed to invoke command: %v", err)
			} else {
				ch <- r
			}
		}()
		tester.expectCommand("FOOBAR")
		tester.writeResponseWithoutLineEnding("WHATEVER")
		tester.elapse(1 * time.Second)
		tester.expectCommand("")
		resp := <-ch
		if resp != "WHATEVER" {
			t.Errorf("bad command response: %q", resp)
		}
	}
}

type queueItem struct {
	query, resp       string
	fixedResponseSize int
}

type fakeCommander struct {
	connected bool
	t         *testing.T
	readyCh   chan struct{}
	queue     []queueItem
}

var _ Commander = &fakeCommander{}

func newFakeCommander(t *testing.T) *fakeCommander {
	return &fakeCommander{t: t, readyCh: make(chan struct{})}
}

func (c *fakeCommander) Connect() {
	if !c.connected {
		c.connected = true
		close(c.readyCh)
	}
}

func (c *fakeCommander) Ready() <-chan struct{} {
	return c.readyCh
}

func (c *fakeCommander) Query(query string, fixedResponseSize int) (string, error) {
	if !c.connected {
		err := errors.New("fakeCommander: not connected")
		c.t.Error(err)
		return "", err
	}
	if len(c.queue) == 0 {
		err := errors.New("fakeCommander: response queue is empty")
		c.t.Error(err)
		return "", err
	}
	item := c.queue[0]
	c.queue = c.queue[1:]
	if query != item.query {
		err := fmt.Errorf("fakeCommander: bad command %q instead of %q", query, item.query)
		c.t.Error(err)
		return "", err
	}
	if item.fixedResponseSize != fixedResponseSize {
		err := fmt.Errorf("fakeCommander: bad fixedResponseSize: %d instead of %d", fixedResponseSize, item.fixedResponseSize)
		c.t.Error(err)
		return "", err
	}
	return item.resp, nil
}

func (c *fakeCommander) Close() {
	c.connected = false
}

func (c *fakeCommander) enqueue(items ...interface{}) {
	for i := 0; i < len(items); {
		qi := queueItem{}
		var ok bool
		qi.fixedResponseSize, ok = items[i].(int)
		if ok {
			i++
		}
		if i+1 >= len(items) {
			c.t.Fatalf("bad enqueue call -- expected queueItem or query and response pair")
		}
		qi.query, ok = items[i].(string)
		if !ok {
			c.t.Fatalf("query must be a string but got %#v", items[i])
		}
		qi.resp, ok = items[i+1].(string)
		if !ok {
			c.t.Fatalf("response must be a string but got %#v", items[i+1])
		}

		if qi.fixedResponseSize != 0 && len(qi.resp) != qi.fixedResponseSize {
			c.t.Fatalf("fixedResponseSize %d doesn't match the length of the response (%d): %q", qi.fixedResponseSize, len(qi.resp), qi.resp)
		}

		i += 2
		c.queue = append(c.queue, qi)
	}
}

func (c *fakeCommander) verifyAndFlush() {
	if len(c.queue) > 0 {
		c.t.Errorf("fakeCommander: unexpected items in queue: %#v", c.queue)
	}
}

// TODO: test command delays
// TODO: test pauses between connection attempts
// TODO: test failure upon connect & reconnect!
// TODO: test bad response to reconnection
// TODO: test errors while connecting

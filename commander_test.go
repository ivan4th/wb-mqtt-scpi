package main

import (
	"bufio"
	"errors"
	"io"
	"log"
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
	time      time.Time
	deadlines []fakeClockDeadline
}

func newFakeClock() *fakeClock {
	return &fakeClock{time.Now(), nil}
}

func (c *fakeClock) Now() time.Time {
	return c.time
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	t := c.time.Add(d)
	ch := make(chan time.Time)
	c.deadlines = append(c.deadlines, fakeClockDeadline{t, ch})
	return ch
}

func (c *fakeClock) elapse(d time.Duration) {
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
	t            *testing.T
	ourReader    *bufio.Reader
	ourWriter    io.Writer
	fc           *fakeConnection
	connectCount int
	connectPort  string
	connectCh    chan struct{}
	lineEnding   string
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

func (tester *cmdTester) expectCommand(cmd string) {
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
		tester.t.Fatalf("timed out waiting for command: %v", cmd)
	case err := <-errCh:
		if err != nil {
			tester.t.Fatalf("failed to read the command, expected: %v", cmd)
		}
	}
	if l != cmd+tester.lineEnding {
		tester.t.Errorf("invalid command: %#v instead of %#v", l, cmd+tester.lineEnding)
	}
}

func (tester *cmdTester) writeResponse(response string) {
	if _, err := tester.ourWriter.Write([]byte(response + tester.lineEnding)); err != nil {
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

func TestCommander(t *testing.T) {
	tester := newCmdTester(t, samplePort)
	commander := NewCommander(tester.connect, &PortSettings{Port: samplePort})
	commander.Connect()
	<-commander.Ready()
	commander.SetClock(tester)
	tester.chat("*IDN?", "IZNAKURNOZH", func() (string, error) {
		return commander.Query("*IDN?")
	})
	tester.chat("CURR?", "3.500", func() (string, error) {
		return commander.Query("CURR?")
	})
	tester.chat("CURR 3.4; *OPC?", "1", func() (string, error) {
		return commander.Query("CURR 3.4; *OPC?")
	})
	// make sure setting the value didn't break DeviceCommander
	tester.chat("CURR?", "3.400", func() (string, error) {
		return commander.Query("CURR?")
	})

	tester.fc.readTime = tester.time.Add(10 * time.Second)
	errCh := make(chan error)
	go func() {
		_, err := commander.Query("CURR?")
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
		return commander.Query("CURR?")
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
		return commander.Query("*IDN?")
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
		return commander.Query("*IDN?")
	})
	tester.fc.pendingError = errors.New("oops")
	if _, err := commander.Query("*IDN?"); err == nil {
		t.Errorf("Identify() didn't return the expected error")
	}

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
		return commander.Query("*IDN?")
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
		return commander.Query("*IDN?")
	})
}

// TODO: use just '\n' for line endings
// TODO: test command delays
// TODO: test pauses between connection attempts
// TODO: test failure upon connect & reconnect!
// TODO: test bad response to reconnection
// TODO: conn setup (after reconnect too) -- :SYST:REM (setup) -- sent w/o response!
// TODO: test errors while connecting
// TODO: Scpi.Ready()
// TODO: don't poll when not Scpi.Ready()

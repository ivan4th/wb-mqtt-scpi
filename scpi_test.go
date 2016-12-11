package main

import (
	"bufio"
	"io"
	"testing"
	"time"
)

type fakeConnection struct {
	io.Reader
	io.Writer
	io.Closer
	deadline, readTime time.Time
}

func (fc *fakeConnection) SetDeadline(time time.Time) error {
	fc.deadline = time
	return nil
}

func (fc *fakeConnection) Read(p []byte) (n int, err error) {
	if fc.readTime.After(fc.deadline) {
		return 0, ErrTimeout
	}
	return fc.Reader.Read(p)
}

type scpiTester struct {
	t         *testing.T
	ourReader *bufio.Reader
	ourWriter io.Writer
	fc        *fakeConnection
	time      time.Time
}

func newScpiTester(t *testing.T) *scpiTester {
	ourInnerReader, theirWriter := io.Pipe()
	theirReader, ourWriter := io.Pipe()
	tester := &scpiTester{
		t:         t,
		ourReader: bufio.NewReader(ourInnerReader),
		ourWriter: ourWriter,
		time:      time.Now(),
	}
	tester.fc = &fakeConnection{
		Reader: theirReader,
		Writer: theirWriter,
		Closer: theirWriter,
	}
	return tester
}

func (tester *scpiTester) expectCommand(cmd string) {
	var l string
	errCh := make(chan error)
	go func() {
		var err error
		l, err = tester.ourReader.ReadString('\n')
		errCh <- err
	}()
	select {
	case <-time.After(3 * time.Second):
		tester.t.Fatalf("timed out waiting for command: %v", cmd)
	case err := <-errCh:
		if err != nil {
			tester.t.Fatalf("failed to read the command, expected: %v", cmd)
		}
	}
	if l != cmd+"\r\n" {
		tester.t.Errorf("invalid command: %#v instead of %#v", l, cmd+"\r\n")
	}
}

func (tester *scpiTester) writeResponse(response string) {
	if _, err := tester.ourWriter.Write([]byte(response + "\r\n")); err != nil {
		tester.t.Fatalf("Write failed: %v", err)
	}
}

func (tester *scpiTester) respondToCommand(response string, ch chan string) {
	tester.writeResponse(response)
	result := <-ch
	if result != response {
		tester.t.Fatalf("bad result: %#v instead of %#v", result, response)
	}
}

func (tester *scpiTester) chat(cmd, response string, thunk func() (string, error)) {
	ch := make(chan string)
	go func() {
		if r, err := thunk(); err != nil {
			tester.t.Fatalf("failed to invoke command: %v", err)
		} else {
			ch <- r
		}
	}()
	tester.expectCommand(cmd)
	if response != "" {
		tester.respondToCommand(response, ch)
	}
}

func (tester *scpiTester) simpleChat(cmd, response string) {
	tester.expectCommand(cmd)
	if response != "" {
		tester.writeResponse(response)
	}
}

func (tester *scpiTester) getTime() time.Time { return tester.time }

func (tester *scpiTester) elapse(d time.Duration) {
	tester.time = tester.time.Add(d)
}

func (tester *scpiTester) acceptSetCommand(cmd string, thunk func() error) {
	errCh := make(chan error)
	go func() {
		errCh <- thunk()
	}()
	tester.expectCommand(cmd)
	tester.ourWriter.Write([]byte("1\r\n"))
	if err := <-errCh; err != nil {
		tester.t.Fatalf("failed to invoke command: %v", err)
	}
}

func TestScpi(t *testing.T) {
	tester := newScpiTester(t)
	scpi := NewScpi(tester.fc, "")
	scpi.SetTimeFunc(tester.getTime)
	tester.chat("*IDN?", "IZNAKURNOZH", scpi.Identify)
	tester.chat("CURR?", "3.500", func() (string, error) {
		return scpi.Query("CURR?")
	})
	tester.acceptSetCommand("CURR 3.4; *OPC?", func() error {
		return scpi.Set("CURR", "3.4")
	})
	// make sure setting the value didn't break Scpi
	tester.chat("CURR?", "3.400", func() (string, error) {
		return scpi.Query("CURR?")
	})

	tester.fc.readTime = tester.time.Add(10 * time.Second)
	errCh := make(chan error)
	go func() {
		_, err := scpi.Query("CURR?")
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
		return scpi.Query("CURR?")
	})
}

func TestScpiBadIdn(t *testing.T) {
	tester := newScpiTester(t)
	scpi := NewScpi(tester.fc, "IZNAKURNOZH")
	scpi.SetTimeFunc(tester.getTime)
	errCh := make(chan error)
	go func() {
		_, err := scpi.Identify()
		errCh <- err
	}()

	tester.expectCommand("*IDN?")
	tester.writeResponse("wrongresponse")
	tester.expectCommand("*IDN?")
	tester.writeResponse("wrongagain")
	tester.expectCommand("*IDN?")
	tester.writeResponse("IZNAKURNOZH,1,2,3,4")

	select {
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Identify() to finish")
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Identify() failed: %v", err)
		}
	}
}

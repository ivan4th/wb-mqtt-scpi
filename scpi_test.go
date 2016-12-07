package main

import (
	"bufio"
	"io"
	"testing"
)

type timeoutSimulator struct {
	io.Reader
	simulateTimeout bool
}

func (ts *timeoutSimulator) Read(p []byte) (n int, err error) {
	if ts.simulateTimeout {
		ts.simulateTimeout = false
		return 0, ErrTimeout
	}
	return ts.Reader.Read(p)
}

type scpiTester struct {
	t         *testing.T
	ourReader *bufio.Reader
	ourWriter io.Writer
	ts        *timeoutSimulator
	theirEnd  io.ReadWriteCloser
}

func newScpiTester(t *testing.T) *scpiTester {
	ourInnerReader, theirWriter := io.Pipe()
	theirReader, ourWriter := io.Pipe()
	ts := &timeoutSimulator{theirReader, false}
	theirEnd := struct {
		io.Reader
		io.Writer
		io.Closer
	}{ts, theirWriter, theirWriter}
	return &scpiTester{
		t:         t,
		ourReader: bufio.NewReader(ourInnerReader),
		ourWriter: ourWriter,
		ts:        ts,
		theirEnd:  theirEnd,
	}
}

func (tester *scpiTester) expectCommand(cmd string) {
	l, err := tester.ourReader.ReadString('\n')
	if err != nil {
		tester.t.Fatalf("failed to read the command")
	}
	if l != cmd+"\r\n" {
		tester.t.Errorf("invalid command: %#v instead of %#v", l, cmd+"\r\n")
	}
}

func (tester *scpiTester) respondToCommand(response string, ch chan string) {
	tester.ourWriter.Write([]byte(response + "\r\n"))
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
		tester.ourWriter.Write([]byte(response + "\r\n"))
	}
}

func (tester *scpiTester) acceptSetCommand(cmd string, thunk func() error) {
	errCh := make(chan error)
	go func() {
		errCh <- thunk()
	}()
	tester.expectCommand(cmd)
	if err := <-errCh; err != nil {
		tester.t.Fatalf("failed to invoke command: %v", err)
	}
}

func (tester *scpiTester) simulateTimeout() {
	tester.ts.simulateTimeout = true
}

func TestScpi(t *testing.T) {
	tester := newScpiTester(t)
	scpi := NewScpi(tester.theirEnd)
	tester.chat("*IDN?", "IZNAKURNOZH", scpi.Identify)
	tester.chat("CURR?", "3.500", func() (string, error) {
		return scpi.Query("CURR?")
	})
	tester.acceptSetCommand("CURR 3.4", func() error {
		return scpi.Set("CURR", "3.4")
	})
	// make sure setting the value didn't break Scpi
	tester.chat("CURR?", "3.400", func() (string, error) {
		return scpi.Query("CURR?")
	})

	errCh := make(chan error)
	tester.simulateTimeout()
	go func() {
		_, err := scpi.Query("CURR?")
		errCh <- err
	}()
	if _, err := tester.ourReader.ReadString('\n'); err != nil {
		t.Fatalf("failed to read the command")
	}
	if err := <-errCh; err != ErrTimeout {
		t.Errorf("unexpected error value: %#v (expected ErrTimeout)", err)
	}

	// make sure things didn't break, again
	tester.chat("CURR?", "3.400", func() (string, error) {
		return scpi.Query("CURR?")
	})
}

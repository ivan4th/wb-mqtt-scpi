package main

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"testing"
)

func TestNoLfWriter(t *testing.T) {
	var b bytes.Buffer
	noLfWriter := NewNoLfWriter(&b)
	toWrite := "abc\r\nabc\r\nqqq\n\n"
	expected := "abc\rabc\rqqq"
	n, err := noLfWriter.Write([]byte(toWrite))
	switch {
	case err != nil:
		t.Fatalf("Write(): %v", err)
	case string(b.Bytes()) != expected:
		t.Errorf("unexpected result %#v, expected %#v", string(b.Bytes()), expected)
	case n != len(toWrite):
		t.Errorf("unexpected write len %d instead of %d", n, len(toWrite))
	}
}

func TestAddLfReader(t *testing.T) {
	b := bytes.NewBufferString("abc1\rabc2\rqq\rq\r")
	r := bufio.NewReader(NewAddLfReader(b))
	var lines []string
readLines:
	for {
		l, err := r.ReadString('\n')
		if l != "" { // l == "" only happens when delimiters aren't read
			lines = append(lines, l)
		}
		switch {
		case err == io.EOF:
			break readLines
		case err != nil:
			t.Fatalf("Read(): %v", err)
		}
	}

	expectedLines := []string{
		"abc1\r\n",
		"abc2\r\n",
		"qq\r\n",
		"q\r\n",
	}

	if !reflect.DeepEqual(lines, expectedLines) {
		t.Errorf("bad lines: %#v instead of %#v", lines, expectedLines)
	}
}

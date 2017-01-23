package main

import (
	"bufio"
	"io"
)

type noLfWriter struct {
	io.Writer
}

func NewNoLfWriter(writer io.Writer) io.Writer {
	return &noLfWriter{writer}
}

func (w *noLfWriter) Write(bs []byte) (int, error) {
	var nb [1]byte
	for n, b := range bs {
		if b == 10 {
			continue
		}
		nb[0] = b
		nw, err := w.Writer.Write(nb[:])
		if err != nil {
			return n + nw, err
		}
		// no error implies that nw == 1
		if nw != 1 {
			panic("bad Write() count")
		}
	}
	return len(bs), nil
}

type addLfReader struct {
	io.Reader
	gotCr bool
	// acc   []byte
}

func NewAddLfReader(reader io.Reader) io.Reader {
	return &addLfReader{Reader: bufio.NewReader(reader)}
}

func (r *addLfReader) Read(bs []byte) (int, error) {
	// XXX: this works around problem with pipes
	// (TestAltLineEnding test hangs otherwise)
	if len(bs) > 1 {
		bs = bs[:1]
	}
	n := 0
	for ; n < len(bs); n++ {
		if r.gotCr {
			bs[n] = 10
			r.gotCr = false
			continue
		}
		nr, err := r.Reader.Read(bs[n : n+1])
		if err != nil {
			r.gotCr = false
			return n + nr, err
		}
		if nr == 0 {
			break
		}
		r.gotCr = bs[n] == 13
	}
	return n, nil
}

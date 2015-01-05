package frame

import (
	"bytes"
	"fmt"
	"io"
)

type Frame interface {
	StreamId() StreamId
	Type() Type
	Flags() Flags
	Length() uint32
	readFrom(io.Reader) error
	writeTo(io.Writer) error
}

// A Framer serializes/deserializer frames to/from an io.ReadWriter
type Framer interface {
	// WriteFrame writes the given frame to the underlying transport
	WriteFrame(Frame) error

	// ReadFrame reads the next frame from the underlying transport
	ReadFrame() (Frame, error)
}

type framer struct {
	io.Reader
	io.Writer
	common

	// frames
	Rst
	Data
	WndInc
	GoAway
}

func (fr *framer) WriteFrame(f Frame) error {
	return f.writeTo(fr.Writer)
}

func (fr *framer) ReadFrame() (f Frame, err error) {
	if err := fr.common.readFrom(fr.Reader); err != nil {
		return nil, err
	}
	switch fr.common.ftype {
	case TypeRst:
		f = &fr.Rst
	case TypeData:
		f = &fr.Data
	case TypeWndInc:
		f = &fr.WndInc
	case TypeGoAway:
		f = &fr.GoAway
	default:
		// XXX: ignore unknown frame types instead
		return nil, fmt.Errorf("Illegal frame type: 0x%x", fr.common.ftype)
	}
	return f, f.readFrom(fr)
}

func NewFramer(r io.Reader, w io.Writer) Framer {
	fr := &framer{
		Reader: r,
		Writer: w,
	}
	fr.Rst.common = &fr.common
	fr.Data.common = &fr.common
	fr.WndInc.common = &fr.common
	fr.GoAway.common = &fr.common
	return fr
}

type debugFramer struct {
	debugWr io.Writer
	Framer
}

func (fr *debugFramer) WriteFrame(f Frame) error {
	// each frame knows how to write iteself to the framer
	fmt.Fprintf(fr.debugWr, "Write frame: %s\n", f)

	// print the serialized frame
	var buf bytes.Buffer
	f.writeTo(&buf)
	body := buf.Bytes()[8:]
	fmt.Fprintf(fr.debugWr, "Frame serialized: HEADER: %x\n", buf.Bytes()[:8])
	nbody := len(body)
	if nbody > 0 {
		fmt.Fprintf(fr.debugWr, "BODY:\n")
	}
	for i := 0; i < nbody; i += 16 {
		j := i + 16
		if j > nbody {
			j = nbody
		}
		fmt.Fprintf(fr.debugWr, "\t%x\n", body[i:j])
	}

	// actually write the frame to the real framer
	return fr.Framer.WriteFrame(f)
}

func (fr *debugFramer) ReadFrame() (Frame, error) {
	f, err := fr.Framer.ReadFrame()
	fmt.Fprintf(fr.debugWr, "Read frame (err: %v): %s\n", err, f)
	return f, err
}

func NewDebugFramer(wr io.Writer, fr Framer) Framer {
	return &debugFramer{
		Framer:  fr,
		debugWr: wr,
	}
}

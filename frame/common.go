package frame

import (
	"encoding/binary"
	"fmt"
	"io"
)

var (
	// the byte order of all serialized integers
	order = binary.BigEndian
)

const (
	// masks for packing/unpacking frames
	streamMask = 0x7FFFFFFF
	typeMask   = 0xF0
	flagsMask  = 0x0F
	wndIncMask = 0x7FFFFFFF
	lengthMask = 0x00FFFFFF
)

// StreamId is 31-bit integer uniquely identifying a stream within a session
type StreamId uint32

func (id StreamId) valid() error {
	if id > streamMask {
		return fmt.Errorf("invalid stream id: %d", id)
	}
	return nil
}

// ErrorCode is a 32-bit integer indicating an error condition on a stream or session
type ErrorCode uint32

// Type is a 4-bit integer in the frame header that identifies the type of frame
type Type uint8

const (
	TypeRst    Type = 0x0
	TypeData   Type = 0x1
	TypeWndInc Type = 0x2
	TypeGoAway Type = 0x3
)

func (t Type) String() string {
	switch t {
	case TypeRst:
		return "RST"
	case TypeData:
		return "DATA"
	case TypeWndInc:
		return "WNDINC"
	case TypeGoAway:
		return "GOAWAY"
	}
	return "UNKNOWN"
}

// Flags is a 4-bit integer containing frame-specific flag bits in the frame header
type Flags uint8

const (
	FlagDataFin = 0x1
	FlagDataSyn = 0x2
)

func (f Flags) IsSet(g Flags) bool {
	return (f & g) != 0
}

func (f *Flags) Set(g Flags) {
	*f |= g
}

func (f *Flags) Unset(g Flags) {
	*f = *f &^ g
}

const (
	headerSize       = 8
	maxFixedBodySize = 8 // goaway frame has streamid + errorcode
	maxBufferSize    = headerSize + maxFixedBodySize
)

type common struct {
	streamId StreamId
	length   uint32
	ftype    Type
	flags    Flags
	b        [maxBufferSize]byte
}

func (c *common) StreamId() StreamId {
	return c.streamId
}

func (c *common) Length() uint32 {
	return c.length
}

func (c *common) Type() Type {
	return c.ftype
}

func (c *common) Flags() Flags {
	return c.flags
}

func (c *common) readFrom(r io.Reader) error {
	b := c.b[:headerSize]
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	c.length = (uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2]))
	c.ftype = Type(b[3] >> 4)
	c.flags = Flags(b[3] & flagsMask)
	c.streamId = StreamId(order.Uint32(b[4:]))
	return nil
}

func (c *common) writeTo(w io.Writer, fixedSize int) error {
	_, err := w.Write(c.b[:headerSize+fixedSize])
	return err
}

func (c *common) pack(ftype Type, length int, streamId StreamId, flags Flags) error {
	if err := streamId.valid(); err != nil {
		return err
	}
	if !isValidLength(length) {
		return fmt.Errorf("invalid length: %d", length)
	}
	c.ftype = ftype
	c.streamId = streamId
	c.length = uint32(length)
	c.flags = flags
	_ = append(c.b[:0],
		byte(c.length>>16),
		byte(c.length>>8),
		byte(c.length),
		byte(uint8(c.ftype<<4)|uint8(c.flags&flagsMask)),
		byte(c.streamId>>24),
		byte(c.streamId>>16),
		byte(c.streamId>>8),
		byte(c.streamId),
	)
	return nil
}

func (c *common) body() []byte {
	return c.b[headerSize:]
}

func (c *common) String() string {
	s := fmt.Sprintf(
		"FRAME [TYPE: %s | LENGTH: %d | STREAMID: %x | FLAGS: %d",
		c.Type(), c.Length(), c.StreamId(), c.Flags())
	if c.Type() != TypeData && c.Type() != TypeGoAway {
		s += fmt.Sprintf(" | BODY: %x", c.body()[:c.Length()])
	}
	s += "]"
	return s
}

func isValidLength(length int) bool {
	return length >= 0 && length <= lengthMask
}

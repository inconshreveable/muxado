package muxado

import (
	"github.com/inconshreveable/muxado/proto/frame"
	"net"
	"time"
)

type StreamId frame.StreamId
type StreamPriority frame.StreamPriority
type ErrorCode frame.ErrorCode

// Stream is a full duplex stream-oriented connection that is multiplexed over a Session.
// Stream implement the net.Conn inteface.
type Stream interface {
	Write([]byte) (int, error)
	Read([]byte) (int, error)
	Close() error
	SetDeadline(time.Time) error
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	HalfClose([]byte) (int, error)
	Id() StreamId
	RelatedStreamId() StreamId
	Session() Session
	RemoteAddr() net.Addr
	LocalAddr() net.Addr
}

// Session multiplexes many Streams over a single underlying stream transport.
// Both sides of a muxado session can open new Streams. Sessions can also accept
// new streams from the remote side.
//
// A muxado Session implements the net.Listener interface, returning new Streams from the remote side.
type Session interface {
	Open() (Stream, error)
	OpenStream(StreamPriority, StreamId, bool) (Stream, error)
	Accept() (Stream, error)
	Kill() error
	GoAway(ErrorCode, []byte) error
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	Close() error
	Wait() (ErrorCode, error, []byte)
	NetListener() net.Listener
	NetDial(_, _ string) (net.Conn, error)
}

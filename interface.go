package muxado

import (
	"net"
	"time"

	"github.com/inconshreveable/muxado/proto/frame"
)

type StreamId frame.StreamId

// Stream is a full duplex stream-oriented connection that is multiplexed over
// a Session. Stream implements the net.Conn inteface.
type Stream interface {
	// Write writes the bytes in the given buffer to the stream
	Write([]byte) (int, error)

	// Read reads the next bytes on the stream into the given buffer
	Read([]byte) (int, error)

	// Closes the stream. The semantics of close are implementation dependent.
	// It attempts to behave as Close does for a TCP conn in that it half-closes
	// the stream for sending, and it will send an RST if any more data is
	// received from the remote side.
	Close() error

	// SetDeadline sets a time after which future Read and Write operations will
	// fail.
	//
	// Some implementation may not support this.
	SetDeadline(time.Time) error

	// SetReadDeadline sets a time after which future Read operations will fail.
	//
	// Some implementation may not support this.
	SetReadDeadline(time.Time) error

	// SetWriteDeadline sets a time after which future Write operations will
	// fail.
	//
	// Some implementation may not support this.
	SetWriteDeadline(time.Time) error

	// Id returns the stream's unique identifier.
	Id() StreamId

	// Session returns the session object this stream is running on.
	Session() Session

	// RemoteAddr returns the session transport's remote address.
	RemoteAddr() net.Addr

	// LocalAddr returns the session transport's local address.
	LocalAddr() net.Addr
}

// Session multiplexes many Streams over a single underlying stream transport.
// Both sides of a muxado session can open new Streams. Sessions can also accept
// new streams from the remote side.
//
// A muxado Session implements the net.Listener interface, returning new Streams from the remote side.
type Session interface {

	// Open initiates a new stream on the session. It is equivalent to
	// OpenStream(0, false)
	Open() (net.Conn, error)

	// OpenStream initiates a new stream on the session. A caller can specify an
	// opaque stream type.  Setting fin to true will cause the stream to be
	// half-closed from the local side immediately upon creation.
	OpenStream() (Stream, error)

	// Accept returns the next stream initiated by the remote side
	Accept() (net.Conn, error)

	// Accept returns the next stream initiated by the remote side
	AcceptStream() (Stream, error)

	// Kill closes the underlying transport stream immediately.
	//
	// You should perfer to call Close() for graceful session termination.
	Kill() error

	// Close instructs the session to close cleanly, if possible by the
	// implementation.
	Close() error

	// LocalAddr returns the local address of the transport stream over which
	// the session is running.
	LocalAddr() net.Addr

	// RemoteAddr returns the address of the remote side of the transport stream over which the session is running.
	RemoteAddr() net.Addr

	// Wait blocks until the session has shutdown and returns an error
	// explaining the session termination.
	Wait() error
}

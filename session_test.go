package muxado

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"testing"
	"time"

	"github.com/inconshreveable/muxado/frame"
)

func newFakeStream(sess sessionPrivate, id frame.StreamId, windowSize uint32, fin bool) streamPrivate {
	return &fakeStream{sess, id}
}

type fakeStream struct {
	sess     sessionPrivate
	streamId frame.StreamId
}

func (s *fakeStream) Write([]byte) (int, error)              { return 0, nil }
func (s *fakeStream) Read([]byte) (int, error)               { return 0, nil }
func (s *fakeStream) Close() error                           { return nil }
func (s *fakeStream) SetDeadline(time.Time) error            { return nil }
func (s *fakeStream) SetReadDeadline(time.Time) error        { return nil }
func (s *fakeStream) SetWriteDeadline(time.Time) error       { return nil }
func (s *fakeStream) CloseWrite() error                      { return nil }
func (s *fakeStream) Id() StreamId                           { return StreamId(s.streamId) }
func (s *fakeStream) Session() Session                       { return s.sess }
func (s *fakeStream) RemoteAddr() net.Addr                   { return nil }
func (s *fakeStream) LocalAddr() net.Addr                    { return nil }
func (s *fakeStream) handleStreamData(*frame.Data) error     { return nil }
func (s *fakeStream) handleStreamWndInc(*frame.WndInc) error { return nil }
func (s *fakeStream) handleStreamRst(*frame.Rst) error       { return nil }
func (s *fakeStream) closeWith(error)                        {}

type fakeConn struct {
	in     *io.PipeReader
	out    *io.PipeWriter
	closed bool
}

func (c *fakeConn) SetDeadline(time.Time) error      { return nil }
func (c *fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *fakeConn) LocalAddr() net.Addr              { return nil }
func (c *fakeConn) RemoteAddr() net.Addr             { return nil }
func (c *fakeConn) Close() error                     { c.closed = true; c.in.Close(); return c.out.Close() }
func (c *fakeConn) Read(p []byte) (int, error)       { return c.in.Read(p) }
func (c *fakeConn) Write(p []byte) (int, error)      { return c.out.Write(p) }
func (c *fakeConn) Discard()                         { go io.Copy(ioutil.Discard, c.in) }

func newFakeConnPair() (local *fakeConn, remote *fakeConn) {
	local, remote = new(fakeConn), new(fakeConn)
	local.in, remote.out = io.Pipe()
	remote.in, local.out = io.Pipe()
	return
}

func TestWrongClientParity(t *testing.T) {
	t.Parallel()
	local, remote := newFakeConnPair()
	fmt.Println(local.in, remote.out, remote.in, local.out)
	// don't need the remote output
	remote.Discard()

	// false for a server session
	s := newSession(local, newFakeStream, false)

	// 300 is even, and only servers send even stream ids
	f := frame.NewData()
	f.Pack(300, []byte{}, false, true)

	// send the frame into the session
	fr := frame.NewFramer(remote)
	fr.WriteFrame(f)

	// wait for failure
	err, code, _ := s.Wait()

	if code != ErrorProtocol {
		t.Errorf("Session not terminated with protocol error. Got %d, expected %d. Session error: %v", code, ErrorProtocol, err)
	}

	if !local.closed {
		t.Errorf("Session transport not closed after protocol failure.")
	}
}

func TestWrongServerParity(t *testing.T) {
	t.Parallel()

	local, remote := newFakeConnPair()

	// true for a client session
	s := newSession(local, newFakeStream, true)

	// don't need the remote output
	remote.Discard()

	// 301 is odd, and only clients send even stream ids
	f := frame.NewData()
	f.Pack(301, []byte{}, false, true)

	// send the frame into the session
	fr := frame.NewFramer(remote)
	fr.WriteFrame(f)

	// wait for failure
	err, code, _ := s.Wait()

	if code != ErrorProtocol {
		t.Errorf("Session not terminated with protocol error. Got %d, expected %d. Session error: %v", code, ErrorProtocol, err)
	}

	if !local.closed {
		t.Errorf("Session transport not closed after protocol failure.")
	}
}

func TestAcceptStream(t *testing.T) {
	t.Parallel()

	local, remote := newFakeConnPair()

	// don't need the remote output
	remote.Discard()

	// true for a client session
	s := newSession(local, newFakeStream, true)
	defer s.Close()

	f := frame.NewData()
	f.Pack(300, []byte{}, false, true)

	// send the frame into the session
	fr := frame.NewFramer(remote)
	fr.WriteFrame(f)

	done := make(chan int)
	go func() {
		defer func() { done <- 1 }()

		// wait for accept
		str, err := s.AcceptStream()

		if err != nil {
			t.Errorf("Error accepting stream: %v", err)
			return
		}

		if str.Id() != StreamId(300) {
			t.Errorf("Stream has wrong id. Expected %d, got %d", str.Id(), 300)
		}
	}()

	select {
	case <-time.After(time.Second):
		t.Fatalf("Timed out!")
	case <-done:
	}
}

func TestSynLowId(t *testing.T) {
	t.Parallel()

	local, remote := newFakeConnPair()

	// don't need the remote output
	remote.Discard()

	// true for a client session
	s := newSession(local, newFakeStream, true)

	// Start a stream
	f := frame.NewData()
	f.Pack(302, []byte{}, false, true)

	// send the frame into the session
	fr := frame.NewFramer(remote)
	fr.WriteFrame(f)

	// accept it
	s.Accept()

	// Start a closed stream at a lower id number
	f.Pack(300, []byte{}, false, true)

	// send the frame into the session
	fr.WriteFrame(f)

	err, code, _ := s.Wait()
	if code != ErrorProtocol {
		t.Errorf("Session not terminated with protocol error, got %d expected %d. Error: %v", code, ErrorProtocol, err)
	}
}

// Check that sending a frame of the wrong size responds with FRAME_SIZE_ERROR
func TestFrameSizeError(t *testing.T) {
}

// Check that we get a protocol error for sending STREAM_DATA on a stream id that was never opened
func TestDataOnClosed(t *testing.T) {
}

// Check that we get nothing for sending STREAM_WND_INC on a stream id that was never opened
func TestWndIncOnClosed(t *testing.T) {
}

// Check that we get nothing for sending STREAM_RST on a stream id that was never opened
func TestRstOnClosed(t *testing.T) {
}

func TestGoAway(t *testing.T) {
}

func TestCloseGoAway(t *testing.T) {
}

func TestKill(t *testing.T) {
}

// make sure we get a valid syn frame from opening a new stream
func TestOpen(t *testing.T) {
}

// test opening a new stream that is immediately half-closed
func TestOpenWithFin(t *testing.T) {
}

// validate that a session fulfills the net.Listener interface
// compile-only check
func TestListener(t *testing.T) {
	if false {
		var _ net.Listener = newSession(new(fakeConn), newFakeStream, false)
	}
}

// Test for the Close() behavior
// Close() issues a data frame with the fin flag
// if any further data is received from the remote side, then RST is sent
func TestWriteAfterClose(t *testing.T) {
	t.Parallel()
	local, remote := newFakeConnPair()
	sLocal := newSession(local, newStream, false)
	sRemote := newSession(remote, newStream, true)

	closed := make(chan int)
	go func() {
		stream, err := sRemote.Open()
		if err != nil {
			t.Errorf("Failed to open stream: %v", err)
			return
		}
		defer sRemote.Close()

		<-closed
		// this write should succeed
		if _, err = stream.Write([]byte("test!")); err != nil {
			t.Errorf("Failed to write test data: %v", err)
			return
		}

		// give the remote end some time to send us an RST
		time.Sleep(10 * time.Millisecond)

		// this write should fail
		if _, err = stream.Write([]byte("test!")); err == nil {
			fmt.Println("WROTE FRAME FAILED")
			t.Errorf("expected error, but not did not receive one")
			return
		}
	}()

	stream, err := sLocal.Accept()
	if err != nil {
		t.Fatalf("Failed to accept stream!")
	}

	// tell the other side that we closed so they can write late
	stream.Close()
	closed <- 1

	err, code, debug := sLocal.Wait()
	if err != nil || code != ErrorNone {
		t.Fatalf("Failed to accept second connection, session closed with localErr: %v, remoteErr: %v (code 0x%x)!", err, string(debug), code)
	}
}

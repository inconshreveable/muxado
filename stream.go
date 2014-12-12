package muxado

import (
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/inconshreveable/muxado/buffer"
	"github.com/inconshreveable/muxado/frame"
)

var (
	zeroTime         time.Time
	resetRemoveDelay = 10 * time.Second
	closeError       = fmt.Errorf("Stream closed")
)

type stream struct {
	resetOnce     uint32           // == 1 only if we sent a reset to close this connection
	id            frame.StreamId   // stream id (const)
	session       sessionPrivate   // the parent session (const)
	inBuffer      *buffer.Inbound  // buffer for data coming in from the remote side
	outBuffer     *buffer.Outbound // manages size of the outbound window
	writer        sync.Mutex       // only one writer at a time
	readDeadline  time.Time        // deadline for reads (protected by buffer mutex)
	writeDeadline time.Time        // deadline for writes (protected by writer mutex)
}

// private interface for Streams to call Sessions
type sessionPrivate interface {
	Session
	writeFrame(frame.Frame, time.Time) error
	die(error) error
	removeStream(frame.StreamId)
}

////////////////////////////////
// public interface
////////////////////////////////
func newStream(sess sessionPrivate, id frame.StreamId, windowSize uint32, fin bool) streamPrivate {
	str := &stream{
		id:        id,
		inBuffer:  buffer.NewInbound(int(windowSize)),
		outBuffer: buffer.NewOutbound(int(windowSize)),
		session:   sess,
	}
	if fin {
		str.outBuffer.SetError(fmt.Errorf("Stream closed"))
	}
	return str
}

func (s *stream) Write(buf []byte) (n int, err error) {
	return s.write(buf, false)
}

func (s *stream) Read(buf []byte) (n int, err error) {
	// read from the buffer
	n, err = s.inBuffer.Read(buf)

	// if we read more than zero, we send a window update
	if n > 0 {
		s.sendWindowUpdate(uint32(n))
	}
	return
}

// Close closes the stream in a manner that attempts to emulate a net.Conn's Close():
// - It calls HalfClose() with an empty buffer to half-close the stream on the remote side
// - It calls closeWith() so that all future Read/Write operations will fail
// - If the stream receives another STREAM_DATA frame from the remote side, it will send a STREAM_RST with a CANCELED error code
func (s *stream) Close() error {
	s.CloseWrite()
	s.closeWith(closeError)
	return nil
}

func (s *stream) SetDeadline(deadline time.Time) (err error) {
	if err = s.SetReadDeadline(deadline); err != nil {
		return
	}
	if err = s.SetWriteDeadline(deadline); err != nil {
		return
	}
	return
}

func (s *stream) SetReadDeadline(dl time.Time) error {
	s.inBuffer.SetDeadline(dl)
	return nil
}

func (s *stream) SetWriteDeadline(dl time.Time) error {
	s.writer.Lock()
	s.writeDeadline = dl
	s.writer.Unlock()
	return nil
}

func (s *stream) CloseWrite() error {
	_, err := s.write([]byte{}, true)
	return err
}

func (s *stream) Id() StreamId {
	return StreamId(s.id)
}

func (s *stream) Session() Session {
	return s.session
}

func (s *stream) LocalAddr() net.Addr {
	return s.session.LocalAddr()
}

func (s *stream) RemoteAddr() net.Addr {
	return s.session.RemoteAddr()
}

/////////////////////////////////////
// session's stream interface
/////////////////////////////////////
func (s *stream) handleStreamData(f *frame.Data) error {
	// skip writing for zero-length frames (typically for sending FIN)
	if f.Length() > 0 {
		// write the data into the buffer
		if _, err := s.inBuffer.ReadFrom(f.Reader()); err != nil {
			if err == buffer.FullError {
				s.resetWith(ErrorFlowControl, flowControlViolated)
			} else if err == closeError {
				// We're trying to emulate net.Conn's Close() behavior where we close our side of the connection,
				// and if we get any more frames from the other side, we RST it.
				s.resetWith(ErrorStreamClosed, streamClosed)
			} else if err == buffer.AlreadyClosed {
				// there was already an error set
				s.resetWith(ErrorStreamClosed, streamClosed)
			} else {
				// the transport returned some sort of IO error
				return errTransport(err)
			}
			return nil
		}
	}
	if f.Fin() {
		s.inBuffer.SetError(io.EOF)
		s.maybeRemove()
	}
	return nil
}

func (s *stream) handleStreamRst(f *frame.Rst) error {
	s.closeWith(errStreamReset(fmt.Errorf("Stream reset by peer with error: %s", f.ErrorCode())))
	return nil
}

func (s *stream) handleStreamWndInc(f *frame.WndInc) error {
	s.outBuffer.Increment(int(f.WindowIncrement()))
	return nil
}

func (s *stream) closeWith(err error) {
	s.outBuffer.SetError(err)
	s.inBuffer.SetError(err)
	s.session.removeStream(s.id)
}

////////////////////////////////
// internal methods
////////////////////////////////

func (s *stream) closeWithAndRemoveLater(err error) {
	s.outBuffer.SetError(err)
	s.inBuffer.SetError(err)
	time.AfterFunc(resetRemoveDelay, func() {
		s.session.removeStream(s.id)
	})
}

func (s *stream) maybeRemove() {
	if buffer.BothClosed(s.inBuffer, s.outBuffer) {
		s.session.removeStream(s.id)
	}
}

func (s *stream) resetWith(errorCode ErrorCode, resetErr error) {
	// only ever send one reset
	if !atomic.CompareAndSwapUint32(&s.resetOnce, 0, 1) {
		return
	}

	// close the stream
	s.closeWithAndRemoveLater(resetErr)

	// make the reset frame
	rst := frame.NewRst()
	if err := rst.Pack(s.id, frame.ErrorCode(errorCode)); err != nil {
		s.session.die(errInternal(fmt.Errorf("failed to pack RST frame: %v", err)))
		return
	}

	// need write lock to make sure no data frames get sent after we send the reset
	s.writer.Lock()
	defer s.writer.Unlock()

	// send it
	s.session.writeFrame(rst, zeroTime)
}

func (s *stream) write(buf []byte, fin bool) (n int, err error) {
	// a write call can pass a buffer larger that we can send in a single frame
	// only allow one writer at a time to prevent interleaving frames from concurrent writes
	s.writer.Lock()

	bufSize := len(buf)
	bytesRemaining := bufSize
	for bytesRemaining > 0 || fin {
		// figure out the most we can write in a single frame
		writeReqSize := min(0x3FFF, bytesRemaining)

		// and then reduce that to however much is available in the window
		// this blocks until window is available and may not return all that we asked for
		var writeSize int
		if writeSize, err = s.outBuffer.Decrement(writeReqSize); err != nil {
			s.writer.Unlock()
			return
		}

		// calculate the slice of the buffer we'll write
		start, end := n, n+writeSize

		// only send fin for the last frame
		finBit := fin && end == bufSize

		// make the frame
		data := frame.NewData()
		if err = data.Pack(s.id, buf[start:end], finBit, false); err != nil {
			err = errInternal(fmt.Errorf("failed to pack DATA frame: %v", err))
			s.writer.Unlock()
			return
		}

		// write the frame
		if err = s.session.writeFrame(data, s.writeDeadline); err != nil {
			s.writer.Unlock()
			return
		}

		// update our counts
		n += writeSize
		bytesRemaining -= writeSize

		if finBit {
			s.outBuffer.SetError(streamClosed)
			s.maybeRemove()

			// handles the empty buffer with fin case
			fin = false
		}
	}

	s.writer.Unlock()
	return
}

// sendWindowUpdate sends a window increment frame
// with the given increment
func (s *stream) sendWindowUpdate(inc uint32) {
	// send a window update
	wndinc := frame.NewWndInc()
	if err := wndinc.Pack(s.id, inc); err != nil {
		s.session.die(errInternal(fmt.Errorf("failed to pack WNDINC frame: %v", err)))
		return
	}
	// XXX: write this async? We can only write one at
	// a time if we're not allocating new ones from the heap
	s.session.writeFrame(wndinc, zeroTime)
}

func min(n1, n2 int) int {
	if n1 > n2 {
		return n2
	} else {
		return n1
	}
}

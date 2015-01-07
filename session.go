package muxado

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"sync/atomic"
	"time"

	"github.com/inconshreveable/muxado/frame"
)

const (
	defaultWindowSize       = 0x10000 // 64KB
	defaultAcceptQueueDepth = 128
)

// private interface for Sessions to call Streams
type streamPrivate interface {
	Stream
	handleStreamData(*frame.Data) error
	handleStreamRst(*frame.Rst) error
	handleStreamWndInc(*frame.WndInc) error
	closeWith(error)
}

// factory function that creates new streams
type streamFactory func(sess sessionPrivate, id frame.StreamId, windowSize uint32, fin bool, init bool) streamPrivate

// checks the parity of a stream id (local vs remote, client vs server)
type parityFn func(frame.StreamId) bool

// state for each half of the session (remote and local)
type halfState struct {
	goneAway uint32 // true if that half of the stream has gone away
	lastId   uint32 // last id used/seen from one half of the session
}

// session implements a simple streaming session manager. It has the following characteristics:
//
// - When closing the Session, it does not linger, all pending write operations will fail immediately.
// - It offers no customization of settings like window size/ping time
type session struct {
	dieOnce uint32    // guarantees only one die() call proceeds, first for alignment
	local   halfState // client state
	remote  halfState // server state

	transport         io.ReadWriteCloser // multiplexing over this transport stream
	framer            frame.Framer       // framer
	streams           *streamMap         // all active streams
	accept            chan streamPrivate // new streams opened by the remote
	defaultWindowSize uint32             // window size when creating new streams
	newStream         streamFactory      // factory function to make new streams
	isLocal           parityFn           // determines if a stream id is local or remote
	writeFrames       chan writeReq      // write requests for the framer

	dead   chan struct{} // closed when dead
	dieErr error         // the first error that caused session termination

	// debug information received from the remote end via GOAWAY frame
	remoteError error
	remoteDebug []byte
}

// Client returns a new muxado client-side connection using trans as the transport.
func Client(trans io.ReadWriteCloser) Session {
	return newSession(trans, newStream, true)
}

// Server returns a muxado server session using trans as the transport.
func Server(trans io.ReadWriteCloser) Session {
	return newSession(trans, newStream, false)
}

func newSession(transport io.ReadWriteCloser, newStream streamFactory, isClient bool) Session {
	sess := &session{
		transport:         transport,
		framer:            frame.NewFramer(transport, transport),
		streams:           newStreamMap(),
		accept:            make(chan streamPrivate, defaultAcceptQueueDepth),
		defaultWindowSize: defaultWindowSize,
		newStream:         newStream,
		writeFrames:       make(chan writeReq, 64),
		dead:              make(chan struct{}),
	}
	if isClient {
		sess.isLocal = sess.isClient
		sess.local.lastId += 1
	} else {
		sess.isLocal = sess.isServer
		sess.remote.lastId += 1
	}
	go sess.reader()
	go sess.writer()
	return sess
}

// check if a stream id is for a client stream. client streams are odd
func (s *session) isClient(id frame.StreamId) bool {
	return uint32(id)&1 == 1
}

func (s *session) isServer(id frame.StreamId) bool {
	return !s.isClient(id)
}

////////////////////////////////
// public interface
////////////////////////////////
func (s *session) Open() (net.Conn, error) {
	return s.OpenStream()
}

func (s *session) OpenStream() (Stream, error) {
	// check if the remote has gone away
	if atomic.LoadUint32(&s.remote.goneAway) == 1 {
		return nil, remoteGoneAway
	}

	// get the next id we can use
	nextId := frame.StreamId(atomic.AddUint32(&s.local.lastId, 2))
	if nextId&(1<<31) > 0 {
		return nil, streamsExhausted
	}

	// make the stream and add it to the stream map
	str := s.newStream(s, nextId, s.defaultWindowSize, false, true)
	s.streams.Set(nextId, str)

	return str, nil
}

func (s *session) AcceptStream() (str Stream, err error) {
	str, ok := <-s.accept
	if !ok {
		err, _, _ = s.Wait()
		if err == nil {
			err = fmt.Errorf("session closed by remote peer")
		} else {
			err = fmt.Errorf("session closed with error: %v", err)
		}
		return nil, err
	}
	return str, nil
}

func (s *session) Accept() (net.Conn, error) {
	return s.AcceptStream()
}

func (s *session) Close() error {
	return s.die(nil)
}

func (s *session) GoAway(errCode ErrorCode, debug []byte, dl time.Time) (err error) {
	// mark that we've told the client to go away
	atomic.StoreUint32(&s.local.goneAway, 1)
	f := frame.NewGoAway()
	remoteId := frame.StreamId(atomic.LoadUint32(&s.remote.lastId))
	if err := f.Pack(remoteId, frame.ErrorCode(errCode), debug); err != nil {
		return fromFrameError(err)
	}
	return s.writeFrame(f, dl)
}

type addr struct {
	locality string
}

func (a *addr) Network() string {
	return "muxado"
}

func (a *addr) String() string {
	return "muxado: " + a.locality
}

func (s *session) LocalAddr() net.Addr {
	type localAddr interface {
		LocalAddr() net.Addr
	}
	if a, ok := s.transport.(localAddr); ok {
		return a.LocalAddr()
	} else {
		return &addr{"local"}
	}
}

func (s *session) RemoteAddr() net.Addr {
	type remoteAddr interface {
		RemoteAddr() net.Addr
	}
	if a, ok := s.transport.(remoteAddr); ok {
		return a.RemoteAddr()
	} else {
		return &addr{"remote"}
	}
}

func (s *session) Addr() net.Addr {
	return s.LocalAddr()
}

func (s *session) Wait() (error, error, []byte) {
	<-s.dead
	return s.dieErr, s.remoteError, s.remoteDebug
}

////////////////////////////////
// private interface for streams
////////////////////////////////

// removeStream removes a stream from this session's stream registry
//
// It does not error if the stream is not present
func (s *session) removeStream(id frame.StreamId) {
	s.streams.Delete(id)
}

type writeReq struct {
	f   frame.Frame
	err chan error
}

var pool = make(chan chan error, 1024)

func poolGet() interface{} {
	select {
	case item := <-pool:
		return item
	default:
		return make(chan error)
	}
}
func poolPut(x interface{}) {
	pool <- x.(chan error)
}

// writeFrame writes the given frame to the framer and returns the error from the write operation
func (s *session) writeFrame(f frame.Frame, dl time.Time) error {
	var timeout <-chan time.Time
	if !dl.IsZero() {
		timeout = time.After(dl.Sub(time.Now()))
	}
	var req = writeReq{f: f, err: poolGet().(chan error)}
	select {
	case s.writeFrames <- req:
	case <-s.dead:
		return sessionClosed
	case <-timeout:
		return writeTimeout
	}
	select {
	case err := <-req.err:
		poolPut(req.err)
		return err
	case <-timeout:
		return writeTimeout
	case <-s.dead:
		return sessionClosed
	}
}

// like writeFrame but it returns immediately, do not use with any frame/buffer that will be reused
// or free'd
func (s *session) writeFrameAsync(f frame.Frame) error {
	var req = writeReq{f: f}
	select {
	case s.writeFrames <- req:
		return nil
	case <-s.dead:
		return sessionClosed
	}
}

// die closes the session cleanly with the given error and protocol error code
func (s *session) die(err error) error {
	// only one shutdown ever happens
	if !atomic.CompareAndSwapUint32(&s.dieOnce, 0, 1) {
		return sessionClosed
	}

	errorCode := NoError
	debug := []byte("no error")
	if err != nil {
		errorCode, _ = GetError(err)
		debug = []byte(err.Error())
	}

	// try to send a GOAWAY frame
	_ = s.GoAway(errorCode, debug, time.Now().Add(250*time.Millisecond))

	// yay, we're dead
	s.dieErr = err
	close(s.dead)

	// close the transport
	s.transport.Close()

	// notify all of the streams that we're closing
	s.streams.Each(func(id frame.StreamId, str streamPrivate) {
		str.closeWith(sessionClosed)
	})

	return nil
}

////////////////////////////////
// internal methods
////////////////////////////////

func (s *session) writer() {
	defer s.recoverPanic("writer()")
	for {
		select {
		case req := <-s.writeFrames:
			err := fromFrameError(s.framer.WriteFrame(req.f))
			if req.err != nil {
				req.err <- err
			}
			if err != nil {
				// any write error kills the session
				s.die(err)
			}
		case <-s.dead:
			return
		}
	}
}

// reader() reads frames from the underlying transport and handles passes them to handleFrame
func (s *session) reader() {
	defer s.recoverPanic("reader()")
	defer close(s.accept)
	for {
		f, err := s.framer.ReadFrame()
		if err != nil {
			err = fromFrameError(err)
			if err == io.EOF {
				s.die(nil)
			} else {
				s.die(err)
			}
			return
		}
		// any error encountered while handling a frame must
		// cause the reader to terminate immediately in order
		// to prevent further data on the transport from being processed
		// when the session is now in a possibly illegal state
		if err := s.handleFrame(f); err != nil {
			s.die(err)
			return
		}
		select {
		case <-s.dead:
			return
		default:
		}
	}
}

func (s *session) recoverPanic(prefix string) {
	if r := recover(); r != nil {
		s.die(newErr(InternalError, fmt.Errorf("%s panic: %v", prefix, r)))
	}
}

func (s *session) handleFrame(rf frame.Frame) error {
	switch f := rf.(type) {
	case *frame.Data:
		if f.Syn() {
			// starting a new stream is a sepcial case
			return s.handleSyn(f)
		}

		str := s.getStream(f.StreamId())
		if str == nil {
			// Diverging from the HTTP2 spec here. If we receive a FIN on a
			// a stream that doesn't exist, we'll just ignore it. This allows
			// stream.Close() to fully deallocate a stream without worrying
			// about a buggy implementation on the remote side keeping
			// memory allocated.
			// XXX: maybe remove this by having the stream just dellocate its
			// buffer and then a max open streams cap will take care of the memory attack
			if f.Length() == 0 && f.Fin() {
				return nil
			}

			// if we get a data frame on a non-existent connection, we still
			// need to read out the frame body so that the stream stays in a
			// good state.
			n, err := io.Copy(ioutil.Discard, f.Reader())
			switch {
			case err != nil:
				return err
			case uint32(n) < f.Length():
				return io.ErrUnexpectedEOF
			}

			// DATA frames on closed connections are just stream-level errors
			fRst := frame.NewRst()
			if err := fRst.Pack(f.StreamId(), frame.ErrorCode(StreamClosed)); err != nil {
				return newErr(InternalError, fmt.Errorf("failed to pack data on closed stream RST: %v", err))
			}
			s.writeFrameAsync(fRst)
			return nil
		}
		return str.handleStreamData(f)

	case *frame.Rst:
		// delegate to the stream to handle these frames
		if str := s.getStream(f.StreamId()); str != nil {
			return str.handleStreamRst(f)
		}
	case *frame.WndInc:
		// delegate to the stream to handle these frames
		if str := s.getStream(f.StreamId()); str != nil {
			return str.handleStreamWndInc(f)
		}

	case *frame.GoAway:
		atomic.StoreUint32(&s.remote.goneAway, 1)
		// XXX: this races with shutdown
		s.remoteDebug = f.Debug()
		s.remoteError = &muxadoError{ErrorCode(f.ErrorCode()), errors.New(string(f.Debug()))}
		lastId := f.LastStreamId()
		s.streams.Each(func(id frame.StreamId, str streamPrivate) {
			// close all streams that we opened above the last handled id
			sid := frame.StreamId(str.Id())
			if s.isLocal(sid) && sid > lastId {
				str.closeWith(remoteGoneAway)
			}
		})

	default:
		// unkown frame types ignored
	}
	return nil
}

func (s *session) handleSyn(f *frame.Data) (err error) {
	// if we're going away, refuse new streams
	if atomic.LoadUint32(&s.local.goneAway) == 1 {
		rstF := frame.NewRst()
		if err := rstF.Pack(f.StreamId(), frame.ErrorCode(StreamRefused)); err != nil {
			return newErr(InternalError, fmt.Errorf("failed to pack stream refused RST: %v", err))
		}
		s.writeFrameAsync(rstF)
		return
	}

	if s.isLocal(f.StreamId()) {
		err := fmt.Errorf("initiated stream id has wrong parity for remote endpoint: 0x%x", f.StreamId())
		return newErr(ProtocolError, err)
	}

	// update last remote id
	atomic.StoreUint32(&s.remote.lastId, uint32(f.StreamId()))

	// make the new stream
	str := s.newStream(s, f.StreamId(), s.defaultWindowSize, f.Fin(), false)

	// add it to the stream map
	s.streams.Set(f.StreamId(), str)

	// put the new stream on the accept channel
	var retry bool
RETRY:
	select {
	case s.accept <- str:
	default:
		// The accept channel is full.
		//
		// The Go scheduler can put you into a place where there are goroutines
		// waiting to accept from the full channel but this Goroutine is hogging
		// the CPU and would continuously read new streams and throw them away
		// We tell the runtime to sleep this goroutine for a short amount of time
		// in order to let other goroutines run.
		//
		// The use of time.Sleep + goto instead of using time.After() in the select
		// statement is to avoid a memory alloc in the hot path
		if !retry {
			retry = true
			time.Sleep(time.Millisecond)
			goto RETRY
		}
		// accept queue is full
		rstF := frame.NewRst()
		if err := rstF.Pack(f.StreamId(), frame.ErrorCode(AcceptQueueFull)); err != nil {
			return newErr(InternalError, fmt.Errorf("failed to pack accept overflow RST: %v", err))
		}
		s.writeFrameAsync(rstF)
		// XXX close the stream!
	}

	// handle the stream data
	return str.handleStreamData(f)
}

func (s *session) getStream(id frame.StreamId) streamPrivate {
	// find the stream in the stream map
	str, _ := s.streams.Get(id)
	return str
}

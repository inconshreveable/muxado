package muxado

import "errors"

import "github.com/inconshreveable/muxado/frame"

// ErrorCode is a 32-bit integer indicating an error condition on a stream or session
type ErrorCode uint32

const (
	ErrorNone          ErrorCode = 0x0
	ErrorProtocol      ErrorCode = 0x1
	ErrorInternal      ErrorCode = 0x2
	ErrorFlowControl   ErrorCode = 0x3
	ErrorStreamClosed  ErrorCode = 0x4
	ErrorStreamRefused ErrorCode = 0x5
	ErrorCancel        ErrorCode = 0x6
	ErrorFrameSize     ErrorCode = 0x7
	ErrorTransport     ErrorCode = 0x8
	ErrorAcceptQueue   ErrorCode = 0x9
	ErrorCalm          ErrorCode = 0xA
	ErrorUnspecified   ErrorCode = 0xF
)

type (
	errRemoteGoneAway      error
	errFrameSize           error
	errTransport           error
	errProtocol            error
	errInternal            error
	errStreamReset         error
	errStreamClosed        error
	errSessionClosed       error
	errWriteTimeout        error
	errStreamsExhausted    error
	errFlowControlViolated error
)

var (
	remoteGoneAway      = errRemoteGoneAway(errors.New("remote gone away"))
	streamsExhausted    = errStreamsExhausted(errors.New("streams exhuastated"))
	streamClosed        = errStreamClosed(errors.New("stream closed"))
	writeTimeout        = errWriteTimeout(errors.New("write timed out"))
	flowControlViolated = errFlowControlViolated(errors.New("flow control violated"))
	sessionClosed       = errSessionClosed(errors.New("session closed"))
)

func fromFrameError(err error) error {
	if e, ok := err.(*frame.Error); ok {
		switch e.Type() {
		case frame.ErrorFrameSize:
			return errFrameSize(e.Err())
		case frame.ErrorTransport:
			return errTransport(e.Err())
		case frame.ErrorProtocol:
			return errProtocol(e.Err())
		case frame.ErrorProtocolStream:
			// XXX
			return errProtocol(e.Err())
		}
	}
	return err
}

func codeForError(err error) ErrorCode {
	switch err.(type) {
	case errFrameSize:
		return ErrorFrameSize
	case errProtocol:
		return ErrorProtocol
	case errTransport:
		return ErrorTransport
	case errInternal:
		return ErrorInternal
	default:
		return ErrorInternal
	}
}

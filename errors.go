package muxado

import "errors"

import "github.com/inconshreveable/muxado/frame"

// ErrorCode is a 32-bit integer indicating the type of an error condition
type ErrorCode uint32

const (
	NoError ErrorCode = iota
	ProtocolError
	InternalError
	FlowControlError
	StreamClosed
	StreamRefused
	StreamCancelled
	StreamReset
	FrameSizeError
	AcceptQueueFull
	EnhanceYourCalm
	RemoteGoneAway
	StreamsExhausted
	WriteTimeout
	SessionClosed

	ErrorUnknown ErrorCode = 0xFF
)

var (
	remoteGoneAway      = newErr(StreamClosed, errors.New("remote gone away"))
	streamsExhausted    = newErr(StreamsExhausted, errors.New("streams exhuastated"))
	streamClosed        = newErr(StreamClosed, errors.New("stream closed"))
	writeTimeout        = newErr(WriteTimeout, errors.New("write timed out"))
	flowControlViolated = newErr(FlowControlError, errors.New("flow control violated"))
	sessionClosed       = newErr(SessionClosed, errors.New("session closed"))
)

func fromFrameError(err error) error {
	if e, ok := err.(*frame.Error); ok {
		switch e.Type() {
		case frame.ErrorFrameSize:
			return &muxadoError{FrameSizeError, err}
		case frame.ErrorProtocol, frame.ErrorProtocolStream:
			return &muxadoError{ProtocolError, err}
		}
	}
	return err
}

type muxadoError struct {
	ErrorCode
	error
}

func newErr(code ErrorCode, err error) error {
	return &muxadoError{code, err}
}

func GetError(err error) (ErrorCode, error) {
	if err == nil {
		return NoError, nil
	}
	if e, ok := err.(*muxadoError); ok {
		return e.ErrorCode, e.error
	}
	return ErrorUnknown, err
}

package frame

import "io"

const goAwayFrameLength = 8

type GoAway struct {
	*common
	debug []byte
}

func (f *GoAway) LastStreamId() StreamId {
	return StreamId(order.Uint32(f.body()))
}

func (f *GoAway) ErrorCode() ErrorCode {
	return ErrorCode(order.Uint32(f.body()[4:]))
}

func (f *GoAway) Debug() []byte {
	return f.debug
}

func (f *GoAway) readFrom(rd io.Reader) error {
	if f.length < goAwayFrameLength {
		return frameSizeError(f.length, "GOAWAY")
	}
	if _, err := io.ReadFull(rd, f.body()[:goAwayFrameLength]); err != nil {
		return transportError(err)
	}
	f.debug = make([]byte, f.length-goAwayFrameLength)
	if _, err := io.ReadFull(rd, f.debug); err != nil {
		return transportError(err)
	}
	if f.StreamId() != 0 {
		return protoError("GOAWAY stream id must be zero, not: %d", f.StreamId())
	}
	return nil
}

func (f *GoAway) writeTo(wr io.Writer) (err error) {
	if err = f.common.writeTo(wr, goAwayFrameLength); err != nil {
		return
	}
	if _, err = wr.Write(f.debug); err != nil {
		return transportError(err)
	}
	return
}

func (f *GoAway) Pack(lastStreamId StreamId, errCode ErrorCode, debug []byte) (err error) {
	if err = lastStreamId.valid(); err != nil {
		return
	}
	if err = f.common.pack(TypeGoAway, goAwayFrameLength+len(debug), 0, 0); err != nil {
		return
	}
	order.PutUint32(f.body(), uint32(lastStreamId))
	order.PutUint32(f.body()[4:], uint32(errCode))
	f.debug = debug
	return nil
}

func NewGoAway() (f *GoAway) {
	return &GoAway{common: new(common)}
}

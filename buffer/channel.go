package buffer

import (
	"bytes"
	"io"
	"time"
)

type channelInbound struct {
	bytes.Buffer
	err     error
	maxSize int
	reqs    chan *req
}

func NewChannelInbound(maxSize int) *channelInbound {
	buf := &channelInbound{
		reqs:    make(chan *req),
		maxSize: maxSize,
	}
	// XXX: never shuts down
	go buf.loop()
	return buf
}

type req struct {
	rd   io.Reader
	p    []byte
	n    int
	err  error
	done chan struct{}
}

func (b *channelInbound) ReadFrom(rd io.Reader) (n int, err error) {
	r := req{rd: rd, done: make(chan struct{})}
	b.reqs <- &r
	<-r.done
	return r.n, r.err
}

func (b *channelInbound) Read(p []byte) (n int, err error) {
	r := req{p: p, done: make(chan struct{})}
	b.reqs <- &r
	<-r.done
	return r.n, r.err
}

func (b *channelInbound) SetDeadline(t time.Time) {
}

func (b *channelInbound) SetError(err error) {
	b.reqs <- &req{err: err}
}

func (b *channelInbound) loop() {
	pendingReqs := make([]*req, 0)
	for req := range b.reqs {
		switch {
		case req.err != nil:
			b.err = req.err
		case req.p != nil:
			pendingReqs = append(pendingReqs, req)
		case req.rd != nil:
			if b.err != nil {
				req.err = AlreadyClosed
			} else {
				n, err := b.Buffer.ReadFrom(req.rd)
				req.n, req.err = int(n), err
			}
			if b.Len() > b.maxSize {
				req.err = FullError
			}
			close(req.done)
		}

		for len(pendingReqs) > 0 && (b.Buffer.Len() > 0 || b.err != nil) {
			req = pendingReqs[0]
			if b.err != nil {
				req.err = b.err
			} else {
				req.n, req.err = b.Buffer.Read(req.p)
			}
			close(req.done)
			pendingReqs = pendingReqs[1:]
		}
	}
}

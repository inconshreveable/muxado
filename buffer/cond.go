package buffer

import (
	"bytes"
	"io"
	"io/ioutil"
	"sync"
	"time"
)

type CondInbound struct {
	cond sync.Cond
	mu   sync.Mutex
	bytes.Buffer
	err     error
	maxSize int
}

func (b *CondInbound) Init(maxSize int) {
	b.cond.L = &b.mu
	b.maxSize = maxSize
}

func (b *CondInbound) ReadFrom(rd io.Reader) (n int, err error) {
	var n64 int64
	b.mu.Lock()
	if b.err != nil {
		if _, err = ioutil.ReadAll(rd); err != nil {
			return
		} else {
			err = AlreadyClosed
		}
	} else {
		n64, err = b.Buffer.ReadFrom(rd)
		if b.Buffer.Len() > b.maxSize {
			err = FullError
			b.err = FullError
		}
		b.cond.Broadcast()
	}
	b.mu.Unlock()
	return int(n64), err
}

func (b *CondInbound) Read(p []byte) (n int, err error) {
	b.mu.Lock()
	for {
		if b.Len() != 0 {
			n, err = b.Buffer.Read(p)
			break
		}
		if b.err != nil {
			err = b.err
			break
		}
		b.cond.Wait()
	}
	b.mu.Unlock()
	return
}

func (b *CondInbound) SetError(err error) {
	b.mu.Lock()
	b.err = err
	b.mu.Unlock()
	b.cond.Broadcast()
}

func (b *CondInbound) SetDeadline(t time.Time) {
}

package muxado

import (
	"sync"
	"time"

	"github.com/inconshreveable/muxado/frame"
)

var writeReqPool = sync.Pool{
	New: func() interface{} {
		return &writeReq{err: make(chan error)}
	},
}

type writeReq struct {
	err chan error
	f   frame.Frame
	dl  time.Time
}

func (req *writeReq) release() {
	writeReqPool.Put(req)
}

func getWriteReq() *writeReq {
	return &writeReq{err: make(chan error)}
	//return writeReqPool.Get().(*writeReq)
}

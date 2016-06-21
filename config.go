package muxado

import (
	"io"

	"github.com/inconshreveable/muxado/frame"
)

var defaultConfig = Config{
	MaxWindowSize:        0x40000, // 256KB
	AcceptBacklog:        128,
	NewFramer:            frame.NewFramer,
	newStream:            newStream,
	writeFrameQueueDepth: 64,
}

type Config struct {
	// Maximum size of unread data to receive and buffer (per-stream). Default 256KB.
	MaxWindowSize uint32
	// Maximum number of inbound streams to queue for Accept(). Default 128.
	AcceptBacklog uint32
	// Function creating the Session's framer. Deafult frame.NewFramer()
	NewFramer func(io.Reader, io.Writer) frame.Framer

	// Function to create new streams
	newStream streamFactory

	// Size of writeFrames channel
	writeFrameQueueDepth int
}

func (c *Config) initDefaults() {
	if c.MaxWindowSize == 0 {
		c.MaxWindowSize = defaultConfig.MaxWindowSize
	}
	if c.AcceptBacklog == 0 {
		c.AcceptBacklog = defaultConfig.AcceptBacklog
	}
	if c.NewFramer == nil {
		c.NewFramer = defaultConfig.NewFramer
	}
	if c.newStream == nil {
		c.newStream = defaultConfig.newStream
	}
	if c.writeFrameQueueDepth == 0 {
		c.writeFrameQueueDepth = defaultConfig.writeFrameQueueDepth
	}
}

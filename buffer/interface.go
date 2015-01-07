package buffer

import (
	"io"
	"time"
)

type Inbound interface {
	Read([]byte) (int, error)
	ReadFrom(io.Reader) (int, error)
	SetError(error)
	SetDeadline(time.Time)
}

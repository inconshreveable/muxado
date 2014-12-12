package frame

import (
	"bytes"
	"io/ioutil"
	"reflect"
	"testing"
)

type dataTest struct {
	streamId       StreamId
	data           []byte
	fin            bool
	serialized     []byte
	serializeError bool
}

var dataTests = []dataTest{
	// test a generic data frame
	dataTest{
		streamId:       0x49a1bb00,
		data:           []byte{0x00, 0x01, 0x02, 0x03, 0x04},
		fin:            false,
		serialized:     []byte{0, 0, 0x5, byte(TypeData << 4), 0x49, 0xa1, 0xbb, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04},
		serializeError: false,
	},

	// fin frame
	dataTest{
		streamId:       streamMask,
		data:           []byte{0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00},
		fin:            true,
		serialized:     []byte{0x00, 0x0, 0x10, byte((TypeData << 4) | FlagDataFin), 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00},
		serializeError: false,
	},

	// zero-length frame
	dataTest{
		streamId:       0x1,
		data:           []byte{},
		fin:            false,
		serialized:     []byte{0x0, 0x0, 0x0, byte(TypeData << 4), 0x0, 0x0, 0x0, 0x1},
		serializeError: false,
	},

	// length too long
	dataTest{
		streamId:       0x0,
		data:           make([]byte, lengthMask+1),
		fin:            false,
		serialized:     []byte{},
		serializeError: true,
	},
}

func (dt *dataTest) eq(t *testing.T, f *Data) {
	switch {
	case dt.fin && !f.Fin():
		t.Errorf("expected fin flag but it was not set: %+v", dt)
		return
	case !dt.fin && f.Fin():
		t.Errorf("unexpected fin flag: %+v", dt)
		return
	}
	buf, err := ioutil.ReadAll(f.Reader())
	if err != nil {
		t.Errorf("Failed to read data: %v, %+v", err, dt)
		return
	}
	if !bytes.Equal(dt.data, buf) {
		t.Errorf("data read does not match expected. got: %x, expected: %x", dt.data, buf)
		return
	}
}

func TestData(t *testing.T) {
	t.Parallel()

	// test serialization
	for _, dt := range dataTests {
		buf := new(bytes.Buffer)
		var f *Data = NewData()
		err := f.Pack(dt.streamId, dt.data, dt.fin, false)
		switch {
		case err != nil && !dt.serializeError:
			t.Errorf("failed to pack data frame: %v, %+v!", err, dt)
			continue
		case err == nil && dt.serializeError:
			t.Errorf("expected packing data frame to error but it succeeded: %+v", dt)
			continue
		case dt.serializeError:
			continue
		}
		if err := f.writeTo(buf); err != nil {
			t.Errorf("failed to write data frame: %v, %+v!", err, dt)
			continue
		}
		if !reflect.DeepEqual(dt.serialized, buf.Bytes()) {
			t.Errorf("failed data frame serialization, expected: %v got %v", dt.serialized, buf.Bytes())
			continue
		}
	}

	// test deserialization
	for _, dt := range dataTests {
		if dt.serializeError {
			continue
		}
		buf := bytes.NewReader(dt.serialized)
		var f *Data = NewData()
		if err := f.common.readFrom(buf); err != nil {
			t.Errorf("failed read frame header: %v, %+v", err, dt)
			continue
		}
		err := f.readFrom(buf)
		if err != nil {
			t.Errorf("failed to read data frame: %v, %+v!", err, dt)
		}

		// test for correctness
		dt.eq(t, f)
	}
}

func TestLengthLimitation(t *testing.T) {
	t.Parallel()

	dt := dataTests[0]
	buf := bytes.NewBuffer(dt.serialized)
	buf.Write([]byte("extra data that shouldn't be read"))
	var f *Data = NewData()
	if err := f.common.readFrom(buf); err != nil {
		t.Fatalf("failed read frame header: %v, %+v", err, dt)
	}
	err := f.readFrom(buf)
	if err != nil {
		t.Fatalf("failed to read data frame: %v", err)
	}
	dt.eq(t, f)
}

func TestDataFramer(t *testing.T) {
	t.Parallel()
	buf := new(bytes.Buffer)
	fr := NewFramer(buf)

	for _, dt := range dataTests {
		if dt.serializeError {
			continue
		}
		var f *Data = NewData()
		err := f.Pack(dt.streamId, dt.data, dt.fin, false)
		if err != nil {
			t.Errorf("failed to pack: %v", err)
			continue
		}
		err = fr.WriteFrame(f)
		if err != nil {
			t.Errorf("framer failed to write: %v", err)
			continue
		}
		rf, err := fr.ReadFrame()
		if err != nil {
			t.Errorf("framer failed to read: %v", err)
		}
		dt.eq(t, rf.(*Data))
	}
}

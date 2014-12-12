package muxado

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"sync"
	"testing"
)

type alot struct {
	of      byte
	ofslice []byte
	n       int
	count   int
}

func makealot(of byte, n int) *alot {
	return &alot{of: of, ofslice: bytes.Repeat([]byte{of}, 128000), n: n}
}

func (a *alot) Read(p []byte) (int, error) {
	if a.count > a.n {
		return 0, io.EOF
	}
	max := float64(a.n - a.count) // max we need to read
	a.count += len(p)
	n := int(math.Min(float64(len(p)), max))
	copy(p, a.ofslice)
	return n, nil
}

func (a *alot) Reset() {
	a.count = 0
}

var listener *Listener
var port int

func init() {
	var err error
	listener, err = Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port = listener.Addr().(*net.TCPAddr).Port
}

func testCase(b *testing.B, payloadSize, concurrency int) {
	done := make(chan int)

	go func() {
		sess, err := Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			panic(err)
		}

		go func() {
			err, code, debug := sess.Wait()
			fmt.Printf("'echo' session died with err %v, remote: %v (code 0x%x)\n", err, string(debug), code)
		}()

		for {
			str, err := sess.AcceptStream()
			if err != nil {
				panic(err)
			}

			//fmt.Printf("Accepted new stream from server!\n")
			go func(s Stream) {
				_, err = io.Copy(s, s)
				if err != nil {
					panic(err)
				}
				//fmt.Printf("IO copy finished after %d bytes with error: %v\n",
				//n, err)
				s.Close()
			}(str)
		}
	}()

	sess, err := listener.Accept()
	if err != nil {
		panic(err)
	}

	go func() {
		err, code, debug := sess.Wait()
		fmt.Printf("session died with err %v, remote: %v (code 0x%x)\n", err, string(debug), code)
	}()

	go func() {
		payloads := make([]*alot, concurrency)
		for i := 0; i < concurrency; i++ {
			payloads[i] = makealot('x', payloadSize)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			wg.Add(concurrency)
			start := make(chan int)
			for c := 0; c < concurrency; c++ {
				go func(idx int) {
					<-start
					str, err := sess.OpenStream()
					if err != nil {
						panic(err)
					}
					go func() {
						io.Copy(ioutil.Discard, str)
						wg.Done()
					}()
					payloads[idx].Reset()
					_, err = io.Copy(str, payloads[idx])
					str.CloseWrite()
					//fmt.Printf("Copy finished after %d bytes with
					//error %v\n", n, err)
				}(c)
			}
			close(start)
			wg.Wait()
		}
		close(done)
	}()

	<-done
}

func BenchmarkPayload1BStreams1(b *testing.B) {
	testCase(b, 1, 1)
}

func BenchmarkPayload1KBStreams1(b *testing.B) {
	testCase(b, 1024, 1)
}

func BenchmarkPayload1MBStreams1(b *testing.B) {
	testCase(b, 1024*1024, 1)
}

func BenchmarkPayload64MBStreams1(b *testing.B) {
	testCase(b, 64*1024*1024, 1)
}

func BenchmarkPayload1BStreams8(b *testing.B) {
	testCase(b, 1024, 1)
}

func BenchmarkPayload1KBStreams8(b *testing.B) {
	testCase(b, 1024, 8)
}

func BenchmarkPayload1MBStreams8(b *testing.B) {
	testCase(b, 1024*1024, 8)
}

func BenchmarkPayload64MBStreams8(b *testing.B) {
	testCase(b, 64*1024*1024, 8)
}

func BenchmarkPayload1BStreams64(b *testing.B) {
	testCase(b, 1, 64)
}

func BenchmarkPayload1KBStreams64(b *testing.B) {
	testCase(b, 1024, 64)
}

func BenchmarkPayload1MBStreams64(b *testing.B) {
	testCase(b, 1024*1024, 64)
}

func BenchmarkPayload64MBStreams64(b *testing.B) {
	testCase(b, 64*1024*1024, 64)
}

func BenchmarkPayload1KBStreams256(b *testing.B) {
	testCase(b, 1024, 256)
}

func BenchmarkPayload1MBStreams256(b *testing.B) {
	testCase(b, 1024*1024, 256)
}

func BenchmarkPayload64MBStreams256(b *testing.B) {
	testCase(b, 64*1024*1024, 256)
}

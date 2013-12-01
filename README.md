# muxado - Stream multiplexing for Go

## What is stream multiplexing?
Imagine you have a single, bi-directional stream of bytes like a TCP connection. Stream multiplexing
is a method for enabling the transmission of multiple simultaneous streams over the one underlying transport stream.

## What is muxado?
muxado is an implementation of a stream multiplexing library in Go that can be layered on top of a net.Conn to multiplexing that stream.
muxado's protocol is not currentl documented explicitly, but it is very nearly an implementation of the HTTP2
framing layer with all of the HTTP-specific bits removed. It is heavily inspired by HTTP2, SPDY, and WebMUX.

## How does it work?
Simplying, muxado chunks data sent over each multiplexed stream and transmits each piece
as a "frame" over the transport stream. The remote endpoint then reassembles the frames on the other side into
distinct streams of data which are presented to the application layer.

## What good is it anyways?
A stream multiplexing library is a powerful tool in an application developer's toolbox for a number of purposes:

- Implement aysnchronous/pipelined protocols with ease. Instead of matching requests with responses in your protocols, just open a new stream for each request and communicate over that.
- Don't bother implementing application-level keep-alives, muxado can do that for you transparently.
- Stop building connection pools for your own services. You can open as many independent, concurrent streams as you need without incurring any round-trip latency costs.
- muxado allows the server to initiate new streams to clients which is normally very difficut without NAT-busting trickery.

## What does the API look like?
As much as possible, the muxado library strives to look and feel just like the standard library's net package. Here's how you initate a new client connection:

    sess, err := muxado.DialTLS("tcp", "example.com:1234", tlsConfig)
    
And a server:

    l, err := muxado.ListenTLS("tcp", ":1234", tlsConfig))
    for {
        sess, err := l.Accept()
        go handleSession(sess)
    }

Once you have a session, you can open new streams on it:

    stream, err := sess.Open()

And accept streams opened by the remote side:

    stream, err := sess.Accept()

Streams are satisfy net.Conn, so they're very familiar to work with:
    
    n, err := stream.Write(buf)
    n, err = stream.Read(buf)
    
muxado sessions and streams implement the net.Listener and net.Conn interfaces (with a small shim), so you can use them with existing golang libraries!

    sess, err := muxado.DialTLS("tcp", "example.com:1234", tlsConfig)
    http.Serve(sess.NetListener(), handler)

## How did you build it?
muxado is an modified implementation of the HTTP2 framing protocol with all of the HTTP-specific bits removed. It aims
for simplicity in the protocol by removing everything that is not core to multiplexing streams. The muxado code
is also built with the intention that its performance should be moderatel good within the bounds of working in Go. As a result,
muxado does contain some unidiomatic code which is commeneted on where possible.

## What are its biggest failures?
And stream-multiplexing library over TCP does suffer from head-of-line blocking if the first packet in line gets dropped. muxado is also a poor choice when sending lare payloads and
speed is a priority, It shines best when the application workload needs to quickly open a large number of small-payload streams.

## Status
Most of muxado's features are implemented (and tested!), but there are a few that are notably lacking. (Heartbeating, for instance).
This situation will improve as the project matures.

## License
Apache

// Package mux multiplexes many byte streams over a single websocket, used to
// carry VNC connections between the gateway and a reverse-tunnel agent. The
// gateway opens streams (one per browser VNC session); the agent accepts them,
// dials the requested LAN address, and bridges.
package mux

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var muxDebug = os.Getenv("MUXDEBUG") != ""

func dbg(format string, a ...any) {
	if muxDebug {
		log.Printf("[mux] "+format, a...)
	}
}

const (
	frameOpen  = 1 // payload = destination address string
	frameData  = 2 // payload = stream bytes
	frameClose = 3 // no payload

	maxChunk = 32 * 1024
)

// Session wraps a websocket as a stream multiplexer.
type Session struct {
	conn    *websocket.Conn
	writeMu sync.Mutex

	mu      sync.Mutex
	streams map[uint32]*Stream
	nextID  atomic.Uint32
	accept  chan *Stream
	closed  chan struct{}
	once    sync.Once
}

// NewSession starts the read loop over conn. Call Run (or rely on the internal
// goroutine) to process frames.
func NewSession(conn *websocket.Conn) *Session {
	s := &Session{
		conn:    conn,
		streams: make(map[uint32]*Stream),
		accept:  make(chan *Stream, 16),
		closed:  make(chan struct{}),
	}
	go s.readLoop()
	return s
}

// Open creates a new stream and asks the peer to dial addr.
func (s *Session) Open(addr string) (*Stream, error) {
	id := s.nextID.Add(1)
	st := s.newStream(id)
	s.mu.Lock()
	s.streams[id] = st
	s.mu.Unlock()
	if err := s.writeFrame(frameOpen, id, []byte(addr)); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

// Accept returns the next stream opened by the peer, or nil once the session
// is closed.
func (s *Session) Accept() *Stream {
	select {
	case st := <-s.accept:
		return st
	case <-s.closed:
		return nil
	}
}

// Closed reports the session-closed channel.
func (s *Session) Closed() <-chan struct{} { return s.closed }

// Close tears down the session and all its streams.
func (s *Session) Close() { s.close() }

func (s *Session) newStream(id uint32) *Stream {
	pr, pw := io.Pipe()
	return &Stream{id: id, sess: s, pr: pr, pw: pw, addrCh: make(chan string, 1)}
}

func (s *Session) writeFrame(typ byte, id uint32, payload []byte) error {
	buf := make([]byte, 5+len(payload))
	buf[0] = typ
	binary.BigEndian.PutUint32(buf[1:5], id)
	copy(buf[5:], payload)
	dbg("send type=%d id=%d len=%d", typ, id, len(payload))
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteMessage(websocket.BinaryMessage, buf)
}

func (s *Session) readLoop() {
	defer s.close()
	for {
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			dbg("readLoop exit: %v", err)
			return
		}
		if len(msg) < 5 {
			continue
		}
		typ := msg[0]
		id := binary.BigEndian.Uint32(msg[1:5])
		payload := msg[5:]
		dbg("recv type=%d id=%d len=%d", typ, id, len(payload))

		switch typ {
		case frameOpen:
			st := s.newStream(id)
			s.mu.Lock()
			s.streams[id] = st
			s.mu.Unlock()
			st.addrCh <- string(payload)
			select {
			case s.accept <- st:
			case <-s.closed:
				return
			}
		case frameData:
			s.mu.Lock()
			st := s.streams[id]
			s.mu.Unlock()
			if st != nil {
				st.pw.Write(payload) //nolint:errcheck // reader closure ends the stream
			}
		case frameClose:
			s.mu.Lock()
			st := s.streams[id]
			delete(s.streams, id)
			s.mu.Unlock()
			if st != nil {
				st.pw.CloseWithError(io.EOF)
			}
		}
	}
}

func (s *Session) close() {
	s.once.Do(func() {
		close(s.closed)
		s.mu.Lock()
		for _, st := range s.streams {
			st.pw.CloseWithError(io.EOF)
		}
		s.streams = map[uint32]*Stream{}
		s.mu.Unlock()
		s.conn.Close()
	})
}

// Stream is one multiplexed connection; it satisfies net.Conn.
type Stream struct {
	id     uint32
	sess   *Session
	pr     *io.PipeReader
	pw     *io.PipeWriter
	addrCh chan string
	once   sync.Once
}

// Addr returns the destination address requested by Open (agent side).
func (st *Stream) Addr() string {
	select {
	case a := <-st.addrCh:
		return a
	default:
		return ""
	}
}

func (st *Stream) Read(p []byte) (int, error) { return st.pr.Read(p) }

func (st *Stream) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		n := len(p)
		if n > maxChunk {
			n = maxChunk
		}
		if err := st.sess.writeFrame(frameData, st.id, p[:n]); err != nil {
			return total - len(p), err
		}
		p = p[n:]
	}
	return total, nil
}

func (st *Stream) Close() error {
	st.once.Do(func() {
		st.sess.mu.Lock()
		delete(st.sess.streams, st.id)
		st.sess.mu.Unlock()
		st.sess.writeFrame(frameClose, st.id, nil) //nolint:errcheck // best effort
		st.pr.CloseWithError(io.EOF)
	})
	return nil
}

func (st *Stream) LocalAddr() net.Addr              { return dummyAddr{} }
func (st *Stream) RemoteAddr() net.Addr             { return dummyAddr{} }
func (st *Stream) SetDeadline(time.Time) error      { return nil }
func (st *Stream) SetReadDeadline(time.Time) error  { return nil }
func (st *Stream) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "mux" }
func (dummyAddr) String() string  { return "mux" }

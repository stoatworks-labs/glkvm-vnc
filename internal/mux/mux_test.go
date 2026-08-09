package mux

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestMuxRoundTrip opens a stream from one side, accepts it on the other, and
// verifies bidirectional byte transfer and the propagated address.
func TestMuxRoundTrip(t *testing.T) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverSessCh := make(chan *Session, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		serverSessCh <- NewSession(c)
	}))
	defer ts.Close()

	cli, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	clientSess := NewSession(cli)
	serverSess := <-serverSessCh

	// Client opens a stream targeting an address.
	cs, err := clientSess.Open("192.168.1.50:5900")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Server accepts and echoes.
	ss := serverSess.Accept()
	if ss == nil {
		t.Fatal("accept returned nil")
	}
	if addr := ss.Addr(); addr != "192.168.1.50:5900" {
		t.Fatalf("addr = %q, want 192.168.1.50:5900", addr)
	}
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := ss.Read(buf)
			if n > 0 {
				ss.Write(buf[:n]) // echo
			}
			if err != nil {
				return
			}
		}
	}()

	if _, err := cs.Write([]byte("hello-vnc")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, 9)
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(cs, got)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for echo")
	}
	if string(got) != "hello-vnc" {
		t.Fatalf("echo = %q, want hello-vnc", got)
	}
}

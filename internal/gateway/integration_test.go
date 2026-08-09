package gateway

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stoatworks-labs/glkvm-vnc/internal/config"
	"github.com/stoatworks-labs/glkvm-vnc/internal/mux"
	"github.com/stoatworks-labs/glkvm-vnc/internal/store"
)

// fakeVNCServer accepts one connection and runs a no-auth RFB 3.8 handshake,
// then sends a marker byte after ClientInit so the client can confirm the full
// path is transparent.
func fakeVNCServer(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.Write([]byte("RFB 003.008\n"))
		io.ReadFull(c, make([]byte, 12))
		c.Write([]byte{1, 1}) // count=1, None
		io.ReadFull(c, make([]byte, 1))
		c.Write([]byte{0, 0, 0, 0})     // SecurityResult OK
		io.ReadFull(c, make([]byte, 1)) // ClientInit
		// Emulate a large framebuffer to exercise mux chunking/ordering: a
		// deterministic 2 MiB stream the client verifies byte-for-byte.
		big := make([]byte, 2<<20)
		for i := range big {
			big[i] = byte(i * 7)
		}
		c.Write(big)
		io.Copy(io.Discard, c)
	}()
	return ln
}

// runAgent connects a reverse agent to the gateway and bridges accepted
// streams to their target address (mirrors cmd/glkvm-vnc-agent).
func runAgent(t *testing.T, wsURL, token, id string) *mux.Session {
	t.Helper()
	url := wsURL + "/agent/ws?token=" + token + "&id=" + id
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("agent dial: %v", err)
	}
	sess := mux.NewSession(conn)
	go func() {
		for {
			st := sess.Accept()
			if st == nil {
				return
			}
			go func(st *mux.Stream) {
				defer st.Close()
				addr := st.Addr()
				tcp, err := net.Dial("tcp", addr)
				if err != nil {
					t.Logf("agent dial %q: %v", addr, err)
					return
				}
				defer tcp.Close()
				done := make(chan struct{}, 2)
				go func() { _, e := io.Copy(tcp, st); t.Logf("agent copy browser->server ended: %v", e); done <- struct{}{} }()
				go func() { _, e := io.Copy(st, tcp); t.Logf("agent copy server->browser ended: %v", e); done <- struct{}{} }()
				<-done
			}(st)
		}
	}()
	return sess
}

func TestAgentTunnelEndToEnd(t *testing.T) {
	vnc := fakeVNCServer(t)
	defer vnc.Close()

	dbf := t.TempDir() + "/t.db"
	st, err := store.Open(dbf)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{AdminPassword: "pw", Secret: "pw", AgentToken: "tok", SessionTTL: time.Hour}
	srv := New(cfg, st)
	ts := httptest.NewServer(srv.Handler(http.NotFoundHandler()))
	defer ts.Close()
	wsBase := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Agent-routed endpoint pointing at the fake VNC server.
	id, err := st.Create(t.Context(), &store.Endpoint{Name: "a", Addr: vnc.Addr().String(), Agent: "a1"})
	if err != nil {
		t.Fatal(err)
	}

	agent := runAgent(t, wsBase, "tok", "a1")
	defer agent.Close()

	// Wait for the agent to register.
	deadline := time.Now().Add(2 * time.Second)
	for len(srv.hub.ids()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(srv.hub.ids()) == 0 {
		t.Fatal("agent never registered")
	}

	// Log in for a session cookie.
	resp, err := http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(`{"password":"pw"}`))
	if err != nil {
		t.Fatal(err)
	}
	cookie := resp.Header.Get("Set-Cookie")
	resp.Body.Close()

	// Connect as the browser and run the RFB handshake through the tunnel.
	hdr := http.Header{"Cookie": {cookie}}
	conn, _, err := websocket.DefaultDialer.Dial(wsBase+"/connect-vnc/"+itoa(id), hdr)
	if err != nil {
		t.Fatalf("browser dial: %v", err)
	}
	defer conn.Close()

	br := &wsClient{conn: conn}
	ver := make([]byte, 12)
	mustRead(t, br, ver, "server version")
	if string(ver) != "RFB 003.008\n" {
		t.Fatalf("version = %q", ver)
	}
	conn.WriteMessage(websocket.BinaryMessage, []byte("RFB 003.008\n"))
	sec := make([]byte, 2)
	mustRead(t, br, sec, "security types")
	conn.WriteMessage(websocket.BinaryMessage, []byte{1}) // choose None
	res := make([]byte, 4)
	mustRead(t, br, res, "SecurityResult")
	conn.WriteMessage(websocket.BinaryMessage, []byte{1}) // ClientInit

	// Read the full 2 MiB framebuffer through the tunnel and verify integrity.
	want := make([]byte, 2<<20)
	for i := range want {
		want[i] = byte(i * 7)
	}
	got := make([]byte, len(want))
	mustRead(t, br, got, "framebuffer")
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("framebuffer mismatch at byte %d: got %#x want %#x", i, got[i], want[i])
		}
	}
}

// wsClient buffers binary websocket frames into an io.Reader.
type wsClient struct {
	conn *websocket.Conn
	buf  []byte
}

func (c *wsClient) Read(p []byte) (int, error) {
	for len(c.buf) == 0 {
		typ, data, err := c.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		c.buf = data
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

func mustRead(t *testing.T, r io.Reader, p []byte, what string) {
	t.Helper()
	done := make(chan error, 1)
	go func() { _, err := io.ReadFull(r, p); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read %s: %v", what, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout reading %s", what)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

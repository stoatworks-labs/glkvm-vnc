package gateway

import (
	"io"
	"net"

	"github.com/gorilla/websocket"
	"github.com/stoatworks-labs/glkvm-vnc/internal/rfb"
)

// wsRW adapts a websocket to an io.ReadWriter of binary frames. Read drains
// message payloads (buffering leftover across the RFB handshake into the pump);
// Write emits one binary message. A single goroutine reads and a single
// goroutine writes, so no locking is needed.
type wsRW struct {
	conn *websocket.Conn
	buf  []byte
}

func (r *wsRW) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		typ, data, err := r.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		r.buf = data
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *wsRW) Write(p []byte) (int, error) {
	if err := r.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// bridge relays RFB between the VNC server (server) and the browser (ws). When
// password is set, the RFB auth-proxy runs first so the browser never sees it.
func bridge(server net.Conn, ws *websocket.Conn, password string) error {
	rw := &wsRW{conn: ws}
	if password != "" {
		if err := rfb.AuthProxy(server, rw, password); err != nil {
			return err
		}
	}
	done := make(chan struct{}, 2)
	go func() { io.Copy(server, rw); done <- struct{}{} }() // browser -> server
	go func() { io.Copy(rw, server); done <- struct{}{} }() // server -> browser
	<-done
	server.Close()
	ws.Close()
	<-done
	return nil
}

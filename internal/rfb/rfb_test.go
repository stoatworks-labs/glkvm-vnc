package rfb

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"testing"
	"time"
)

// TestDESCrossCheck pins DESResponse to OpenSSL's DES engine (independent):
//
//	key = bit-reverse(each byte of "secret12") = cea6c64ea62e8c4c
//	printf '0123456789abcdef' | \
//	  openssl enc -des-ecb -K cea6c64ea62e8c4c -nopad -provider legacy
//	=> 5f15f4f0e1684cdc260ea962ab82fa3b
func TestDESCrossCheck(t *testing.T) {
	resp, err := DESResponse("secret12", []byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(resp), "5f15f4f0e1684cdc260ea962ab82fa3b"; got != want {
		t.Fatalf("DES mismatch:\n got %s\nwant %s (OpenSSL)", got, want)
	}
}

// TestAuthProxy drives AuthProxy against an in-memory VNC-Auth server and a
// simulated browser, asserting the browser sees a "None" handshake and the
// server receives the correct DES response.
func TestAuthProxy(t *testing.T) {
	const password = "secret12"
	challenge := []byte("0123456789abcdef")
	wantResp, _ := DESResponse(password, challenge)

	serverPipe, proxyServer := net.Pipe()   // proxyServer = the "server" side given to AuthProxy
	browserPipe, proxyBrowser := net.Pipe() // proxyBrowser = the "browser" side given to AuthProxy

	gotResp := make(chan []byte, 1)
	// Fake VNC server.
	go func() {
		defer serverPipe.Close()
		serverPipe.Write([]byte("RFB 003.008\n"))
		io.ReadFull(serverPipe, make([]byte, 12))
		serverPipe.Write([]byte{1, 2}) // offer VNC Auth
		choice := make([]byte, 1)
		io.ReadFull(serverPipe, choice)
		serverPipe.Write(challenge)
		resp := make([]byte, 16)
		io.ReadFull(serverPipe, resp)
		gotResp <- resp
		serverPipe.Write([]byte{0, 0, 0, 0}) // SecurityResult OK
	}()

	// Simulated browser.
	browserDone := make(chan error, 1)
	go func() {
		ver := make([]byte, 12)
		if _, err := io.ReadFull(browserPipe, ver); err != nil {
			browserDone <- err
			return
		}
		if string(ver) != "RFB 003.008\n" {
			browserDone <- errString("bad version")
			return
		}
		browserPipe.Write([]byte("RFB 003.008\n"))
		sec := make([]byte, 2)
		io.ReadFull(browserPipe, sec)
		if sec[0] != 1 || sec[1] != 1 {
			browserDone <- errString("expected None-only offer")
			return
		}
		browserPipe.Write([]byte{1}) // choose None
		res := make([]byte, 4)
		io.ReadFull(browserPipe, res)
		if binary.BigEndian.Uint32(res) != 0 {
			browserDone <- errString("SecurityResult not OK")
			return
		}
		browserDone <- nil
	}()

	if err := AuthProxy(proxyServer, proxyBrowser, password); err != nil {
		t.Fatalf("AuthProxy: %v", err)
	}

	select {
	case resp := <-gotResp:
		if hex.EncodeToString(resp) != hex.EncodeToString(wantResp) {
			t.Fatalf("server got wrong DES response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received response")
	}
	select {
	case err := <-browserDone:
		if err != nil {
			t.Fatalf("browser side: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("browser handshake did not complete")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Package rfb implements the RFB (VNC) auth-proxy: it completes VNC
// Authentication toward the server on the gateway's behalf and presents
// "None" toward the browser, so the stored password never reaches the client.
// It operates on plain io.ReadWriters, so it is independent of the websocket
// or tunnel transport carrying the two streams.
package rfb

import (
	"crypto/des" //nolint:gosec // VNC Authentication is defined in terms of DES
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
)

// reverseBits reverses the bit order of a byte, as VNC Authentication mangles
// each key byte before use.
func reverseBits(b byte) byte {
	var out byte
	for i := 0; i < 8; i++ {
		out <<= 1
		out |= b & 1
		b >>= 1
	}
	return out
}

// DESResponse computes the 16-byte VNC Authentication response for a 16-byte
// challenge using the (bit-reversed) password key.
func DESResponse(password string, challenge []byte) ([]byte, error) {
	key := make([]byte, 8)
	copy(key, password) // truncated to 8 bytes / zero-padded, per VNC
	for i := range key {
		key[i] = reverseBits(key[i])
	}
	block, err := des.NewCipher(key) //nolint:gosec // required by the VNC spec
	if err != nil {
		return nil, err
	}
	out := make([]byte, 16)
	block.Encrypt(out[0:8], challenge[0:8])
	block.Encrypt(out[8:16], challenge[8:16])
	return out, nil
}

func parseMinor(ver []byte) (int, bool) {
	if len(ver) < 12 || string(ver[0:4]) != "RFB " {
		return 0, false
	}
	minor, err := strconv.Atoi(string(ver[8:11]))
	if err != nil {
		return 0, false
	}
	return minor, true
}

// selectSecurityType prefers VNC Authentication (2), then None (1); returns 0
// when neither is offered.
func selectSecurityType(types []byte) byte {
	hasNone := false
	for _, t := range types {
		if t == 2 {
			return 2
		}
		if t == 1 {
			hasNone = true
		}
	}
	if hasNone {
		return 1
	}
	return 0
}

// AuthProxy runs the RFB handshake on both sides. After it returns nil, both
// peers are positioned at ClientInit and the caller may pump bytes
// transparently. browser must buffer any bytes read past the handshake so the
// caller's subsequent reads see them (a buffered websocket adapter does this).
func AuthProxy(server, browser io.ReadWriter, password string) error {
	// ---- server side ----
	sver := make([]byte, 12)
	if _, err := io.ReadFull(server, sver); err != nil {
		return fmt.Errorf("read server version: %w", err)
	}
	minor, ok := parseMinor(sver)
	if !ok {
		return fmt.Errorf("unexpected server version %q", sver)
	}
	ourMinor := 8
	if minor < 8 {
		ourMinor = minor
	}
	if _, err := server.Write([]byte(fmt.Sprintf("RFB 003.%03d\n", ourMinor))); err != nil {
		return err
	}

	var chosen byte
	if minor >= 7 {
		cnt := make([]byte, 1)
		if _, err := io.ReadFull(server, cnt); err != nil {
			return err
		}
		if cnt[0] == 0 {
			return fmt.Errorf("server offered no security types")
		}
		types := make([]byte, cnt[0])
		if _, err := io.ReadFull(server, types); err != nil {
			return err
		}
		chosen = selectSecurityType(types)
		if chosen == 0 {
			return fmt.Errorf("no supported security type (offered %v)", types)
		}
		if _, err := server.Write([]byte{chosen}); err != nil {
			return err
		}
	} else {
		t := make([]byte, 4)
		if _, err := io.ReadFull(server, t); err != nil {
			return err
		}
		chosen = byte(binary.BigEndian.Uint32(t))
		if chosen == 0 {
			return fmt.Errorf("server rejected connection")
		}
	}

	if chosen == 2 {
		challenge := make([]byte, 16)
		if _, err := io.ReadFull(server, challenge); err != nil {
			return err
		}
		resp, err := DESResponse(password, challenge)
		if err != nil {
			return err
		}
		if _, err := server.Write(resp); err != nil {
			return err
		}
	}

	if minor >= 7 || chosen == 2 {
		res := make([]byte, 4)
		if _, err := io.ReadFull(server, res); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(res) != 0 {
			return fmt.Errorf("server authentication failed")
		}
	}

	// ---- browser side ----
	if _, err := browser.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	cver := make([]byte, 12)
	if _, err := io.ReadFull(browser, cver); err != nil {
		return fmt.Errorf("read browser version: %w", err)
	}
	if _, err := browser.Write([]byte{1, 1}); err != nil { // offer None only
		return err
	}
	choice := make([]byte, 1)
	if _, err := io.ReadFull(browser, choice); err != nil {
		return err
	}
	if _, err := browser.Write([]byte{0, 0, 0, 0}); err != nil { // SecurityResult OK
		return err
	}
	return nil
}

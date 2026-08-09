# AGENTS.md — glkvm-vnc

Onboarding for an LLM or newcomer. The *why* and the load-bearing invariants;
the README is the user-facing quickstart.

## What this is

A standalone, self-hosted VNC gateway: browser (noVNC) ⇄ WebSocket ⇄ gateway ⇄
VNC server. Two ways to reach the server:

- **direct**: the gateway dials `host:port` itself.
- **agent**: a reverse agent dials *out* to the gateway and bridges to VNC
  servers on its own LAN, so NAT/firewalls need no inbound rules.

It is a clean-room re-implementation of the VNC-endpoints feature that was
added to the `glkvm-cloud` fork (see that repo's `feature/vnc-endpoints`),
extracted so it runs without GL.iNet hardware or the rtty device server.

## Mental model / data flow

```
handleConnectVNC (internal/gateway/server.go)
  ├─ direct  → net.Dial                → bridge(serverConn, ws, password)
  └─ agent   → mux.Session.Open(addr)  → bridge(streamConn, ws, password)

bridge (internal/gateway/bridge.go)
  ├─ if password: rfb.AuthProxy(server, ws)   ← VNC Auth to server, "None" to browser
  └─ io.Copy both directions (transparent)
```

The **agent tunnel** is a stream multiplexer (`internal/mux`) over a single
websocket: the gateway `Open`s a stream per browser connection (sends an OPEN
frame with the target address); the agent `Accept`s it, dials the address, and
bridges. Frames are `[type:1][streamID:4][payload]`, DATA chunked at 32 KiB.

## Load-bearing invariants (don't break these)

- **`mux.Stream.Write` must return the total input length**, not `len(p)` after
  the chunk loop reslices `p` to empty. Returning 0 makes `io.Copy` report a
  short write and tears the tunnel down after the first frame. This was the one
  real bug found during bring-up; `TestAgentTunnelEndToEnd` guards it with a
  2 MiB transfer. (The `glkvm-cloud` ancestor has the same shape but hid it by
  ignoring the return value in a hand-written pump.)
- **The RFB server speaks first.** For the tunnel the gateway sends an empty
  OPEN so the agent dials before the browser sends anything; `AuthProxy` and
  the pump both start by *reading* the server.
- **Password never crosses to the browser.** `rfb.AuthProxy` answers the
  server's VNC-Auth challenge and presents *None* to the browser. If you make
  the browser do VNC auth, you've regressed the security model.
- **Direct dials are an SSRF surface.** `netutil.HostAllowed` is enforced both
  at endpoint create/update and again at connect time in `handleConnectVNC`.
  Keep the connect-time check — it's the authoritative one.
- **`web/dist` is `go:embed`-ed.** `go build` fails without it. It is committed
  so a fresh checkout builds; run `make frontend` after changing the UI.

## Layout

- `cmd/glkvm-vnc` — gateway binary. `cmd/glkvm-vnc-agent` — reverse agent.
- `internal/gateway` — HTTP server, auth (single admin password, in-memory
  sessions), endpoint CRUD, the bridge, and the agent hub.
- `internal/mux` — websocket stream multiplexer (agent tunnel).
- `internal/rfb` — RFB auth-proxy + VNC DES. Transport-agnostic (`io.ReadWriter`).
- `internal/vnccrypt` — AES-256-GCM for stored passwords.
- `internal/netutil` — CIDR allowlist helpers.
- `internal/store` — SQLite endpoint store (pure-Go `modernc.org/sqlite`).
- `web/frontend` — Vite + TS + noVNC SPA; builds to `web/dist`.

## Verified vs assumed

**Verified (automated + browser):**
- Direct dial, direct + password-injection, and agent-tunnelled paths all
  render a live RFB test pattern in a real browser via noVNC.
- VNC DES is cross-checked against OpenSSL's engine (`rfb.TestDESCrossCheck`),
  not just self-consistency.
- Agent tunnel carries a 2 MiB payload byte-for-byte through the mux
  (`gateway.TestAgentTunnelEndToEnd`).
- CIDR allowlist rejects out-of-range hosts at the API and connect time.
- Password is stored encrypted and never returned by the API (only
  `hasPassword`).

**Assumed / not yet exercised:**
- Only tested against a **fake** Python RFB server (RFB 3.8, RAW encoding,
  VNC-Auth). Not yet run against a production VNC server (TigerVNC, RealVNC,
  x11vnc) — do that before relying on it.
- RFB 3.3/3.7 handshake branches are coded per spec but only 3.8 is exercised.
- Only *VNC Authentication* (security type 2) injection is supported;
  VeNCrypt/TLS servers pass through without injection.
- No load/concurrency testing of the agent hub beyond single-stream tests.
- The agent's outbound websocket has basic exponential-backoff reconnect but no
  heartbeat/keepalive tuning.

## Build / test

```bash
make frontend   # web/frontend → web/dist (needed before go build)
make test       # go test ./...
make build      # frontend + both binaries → ./bin
MUXDEBUG=1 go test ./internal/gateway -run Tunnel -v   # frame-level mux tracing
```

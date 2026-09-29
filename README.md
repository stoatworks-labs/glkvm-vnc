# glkvm-vnc

> **AI-assisted project.** This codebase was created with [Claude](https://claude.com/claude-code)
> (Anthropic), directed and reviewed by a human author.

> **Archived / secondary.** If you want VNC servers **and** GL.iNet KVM devices
> managed together in one UI, use the VNC-endpoints feature integrated into
> [`glkvm-cloud`](https://github.com/stoatworks-labs/glkvm-cloud) instead — that
> is the primary path. This repo is a standalone, glkvm-cloud-free VNC-only
> gateway, kept for the case where you want *only* VNC without the full stack.

A small, self-hosted **VNC gateway**: a web UI + [noVNC](https://novnc.com) viewer
that bridges a browser to VNC servers — either dialled **directly** by the
gateway, or tunnelled through a lightweight **reverse agent** so you can reach
machines behind NAT with no inbound ports or port-forwarding.

It is a standalone distillation of the VNC-endpoints feature built for
`glkvm-cloud`, with no dependency on GL.iNet hardware or the rtty stack.

```
 browser ──ws──▶ gateway ──▶ VNC server            (direct dial)
 browser ──ws──▶ gateway ──mux over ws──▶ agent ──▶ VNC server on its LAN
```

## Features

- **Web viewer** — noVNC in the browser; the gateway relays RFB byte-for-byte.
- **Direct or tunnelled** — dial VNC servers the gateway can reach, or route
  through a reverse agent that dials *out* to the gateway (NAT traversal).
- **Password injection** — store a VNC password per endpoint (AES-256-GCM at
  rest); an RFB auth-proxy completes VNC Authentication server-side and offers
  *None* to the browser, so **the password never reaches the client**.
- **CIDR allowlist** — constrain which hosts a *direct* endpoint may reach
  (SSRF hardening). Tunnelled endpoints are scoped to the agent's LAN.
- **Single static binary** — the frontend is embedded; SQLite is pure-Go (no
  CGO). A second small binary is the agent.

## Quick start

```bash
# Build (needs Go 1.25+ and Node 18+ for the frontend)
make build

# Run the gateway
GLKVM_VNC_ADMIN_PASSWORD=changeme ./bin/glkvm-vnc
# → open http://localhost:8600
```

Add an endpoint in the UI (e.g. `192.168.1.50:5900`), click **Connect**.

### Reverse agent (NAT traversal)

On a machine that can reach the target VNC servers on its LAN, run the agent —
it dials *out* to the gateway, so the gateway never needs inbound access to
that network:

```bash
GLKVM_VNC_AGENT_TOKEN=shared-secret \
  ./bin/glkvm-vnc-agent \
  -gateway wss://your-gateway.example.com/agent/ws \
  -id lab-agent
```

The gateway must be started with the same `GLKVM_VNC_AGENT_TOKEN`. Once the
agent connects, pick it as the **Route** when creating an endpoint; its address
is then resolved on the agent's LAN.

## Configuration (gateway)

| Env var | Default | Purpose |
| --- | --- | --- |
| `GLKVM_VNC_ADDR` | `:8600` | Listen address |
| `GLKVM_VNC_ADMIN_PASSWORD` | *(generated)* | Web UI password (printed once if unset) |
| `GLKVM_VNC_SECRET` | *(admin password)* | Key for encrypting stored VNC passwords |
| `GLKVM_VNC_AGENT_TOKEN` | *(unset → agents disabled)* | Shared secret reverse agents present |
| `GLKVM_VNC_DIRECT_ALLOWLIST` | *(empty → allow all)* | Comma-separated CIDRs for direct dials |
| `GLKVM_VNC_DB` | `glkvm-vnc.db` | SQLite path |
| `GLKVM_VNC_SESSION_TTL` | `24h` | Web session lifetime |
| `GLKVM_VNC_TLS_CERT` / `GLKVM_VNC_TLS_KEY` | *(none → plain HTTP)* | Serve HTTPS/WSS directly |

Agent env vars: `GLKVM_VNC_GATEWAY`, `GLKVM_VNC_AGENT_TOKEN`,
`GLKVM_VNC_AGENT_ID`, and optional `GLKVM_VNC_AGENT_ALLOWLIST` (CIDRs the agent
will dial). Flags of the same name override the env vars.

## Docker

```bash
docker build -t glkvm-vnc .
docker run -p 8600:8600 -e GLKVM_VNC_ADMIN_PASSWORD=changeme \
  -v glkvm-vnc-data:/data -e GLKVM_VNC_DB=/data/glkvm-vnc.db glkvm-vnc
```

## Security notes

- Put the gateway behind TLS in production (terminate at a proxy, or set
  `GLKVM_VNC_TLS_*`). The browser leg is then WSS.
- **Direct endpoints turn the gateway into a proxy into its own network.**
  Set `GLKVM_VNC_DIRECT_ALLOWLIST`, and prefer agent-routed endpoints so the
  reachable surface is the agent's LAN, not the gateway's.
- VNC's native auth is weak (8-char DES challenge). VeNCrypt/TLS-wrapped VNC
  passes through the bridge untouched; the password-injection path supports the
  common *VNC Authentication* type only.

## Development

```bash
make frontend   # build web/frontend → web/dist
make test       # go test ./...
make build      # frontend + both binaries into ./bin
cd web/frontend && npm run dev   # live frontend against a running gateway
```

## AI disclaimer

The code has been reviewed and tested, but you should read the code and validate it against a
real VNC server for your own use before relying on it. See the tests under
`internal/` — including a cross-check of the VNC DES implementation against
OpenSSL and an end-to-end agent-tunnel test.

## License

[MIT](LICENSE) © Stoatworks Labs

// Command glkvm-vnc-agent is the reverse-tunnel agent. It dials OUT to a
// glkvm-vnc gateway over a websocket and bridges gateway-initiated streams to
// VNC servers on its local network, so the gateway can reach them without any
// inbound ports or port-forwarding (NAT traversal).
package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stoatworks-labs/glkvm-vnc/internal/mux"
	"github.com/stoatworks-labs/glkvm-vnc/internal/netutil"
)

func main() {
	gateway := flag.String("gateway", os.Getenv("GLKVM_VNC_GATEWAY"), "gateway websocket URL, e.g. wss://host:8600/agent/ws")
	token := flag.String("token", os.Getenv("GLKVM_VNC_AGENT_TOKEN"), "shared agent token")
	id := flag.String("id", os.Getenv("GLKVM_VNC_AGENT_ID"), "unique agent id")
	allowEnv := os.Getenv("GLKVM_VNC_AGENT_ALLOWLIST")
	flag.Parse()

	if *gateway == "" || *token == "" || *id == "" {
		log.Fatal("gateway, token and id are required (flags or GLKVM_VNC_GATEWAY / GLKVM_VNC_AGENT_TOKEN / GLKVM_VNC_AGENT_ID)")
	}
	allow := netutil.ParseCIDRs(netutil.SplitCommaList(allowEnv))

	url := *gateway
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	url += sep + "token=" + *token + "&id=" + *id

	backoff := time.Second
	for {
		if err := run(url, allow); err != nil {
			log.Printf("agent: %v; reconnecting in %s", err, backoff)
		}
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func run(url string, allow []*net.IPNet) error {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return err
	}
	log.Printf("agent: connected to gateway")
	sess := mux.NewSession(conn)
	for {
		st := sess.Accept()
		if st == nil {
			return io.EOF
		}
		go handle(st, allow)
	}
}

func handle(st *mux.Stream, allow []*net.IPNet) {
	defer st.Close()
	addr := st.Addr()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("agent: bad addr %q", addr)
		return
	}
	if !netutil.HostAllowed(allow, host) {
		log.Printf("agent: %s blocked by agent allowlist", addr)
		return
	}
	tcp, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		log.Printf("agent: dial %s: %v", addr, err)
		return
	}
	defer tcp.Close()

	done := make(chan struct{}, 2)
	go func() { io.Copy(tcp, st); done <- struct{}{} }()
	go func() { io.Copy(st, tcp); done <- struct{}{} }()
	<-done
	tcp.Close()
	st.Close()
	<-done
}

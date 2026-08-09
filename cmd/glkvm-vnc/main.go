// Command glkvm-vnc is the standalone VNC gateway: a web UI + noVNC viewer
// that bridges browsers to VNC servers, either dialled directly or tunnelled
// through a reverse agent for NAT traversal.
package main

import (
	"log"
	"net/http"

	"github.com/stoatworks-labs/glkvm-vnc/internal/config"
	"github.com/stoatworks-labs/glkvm-vnc/internal/gateway"
	"github.com/stoatworks-labs/glkvm-vnc/internal/store"
	"github.com/stoatworks-labs/glkvm-vnc/web"
)

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer st.Close()

	if cfg.GeneratedAdmin() {
		log.Printf("no GLKVM_VNC_ADMIN_PASSWORD set — generated admin password: %s", cfg.AdminPassword)
	}
	if cfg.AgentToken == "" {
		log.Printf("GLKVM_VNC_AGENT_TOKEN not set — reverse agents are disabled (direct dials only)")
	}

	srv := gateway.New(cfg, st)
	handler := srv.Handler(web.Handler())

	log.Printf("glkvm-vnc listening on %s", cfg.Addr)
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		log.Fatal(http.ListenAndServeTLS(cfg.Addr, cfg.TLSCert, cfg.TLSKey, handler))
	} else {
		log.Fatal(http.ListenAndServe(cfg.Addr, handler))
	}
}

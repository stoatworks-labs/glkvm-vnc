// Package gateway implements the glkvm-vnc HTTP gateway: web UI, endpoint
// management, the browser<->VNC websocket bridge (direct or via a reverse
// agent), and agent registration.
package gateway

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stoatworks-labs/glkvm-vnc/internal/config"
	"github.com/stoatworks-labs/glkvm-vnc/internal/mux"
	"github.com/stoatworks-labs/glkvm-vnc/internal/netutil"
	"github.com/stoatworks-labs/glkvm-vnc/internal/store"
	"github.com/stoatworks-labs/glkvm-vnc/internal/vnccrypt"
)

// Server is the gateway.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	hub      *agentHub
	allow    []*net.IPNet
	upgrader websocket.Upgrader

	mu       sync.Mutex
	sessions map[string]time.Time // sid -> expiry
}

// New builds a Server.
func New(cfg *config.Config, st *store.Store) *Server {
	return &Server{
		cfg:      cfg,
		store:    st,
		hub:      newAgentHub(),
		allow:    netutil.ParseCIDRs(cfg.DirectAllow),
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		sessions: make(map[string]time.Time),
	}
}

// Handler returns the gateway's HTTP handler with the frontend mounted.
func (s *Server) Handler(frontend http.Handler) http.Handler {
	m := http.NewServeMux()

	m.HandleFunc("POST /api/login", s.handleLogin)
	m.HandleFunc("POST /api/logout", s.handleLogout)
	m.HandleFunc("GET /api/session", s.handleSession)

	m.HandleFunc("GET /api/endpoints", s.auth(s.handleList))
	m.HandleFunc("POST /api/endpoints", s.auth(s.handleCreate))
	m.HandleFunc("PUT /api/endpoints/{id}", s.auth(s.handleUpdate))
	m.HandleFunc("DELETE /api/endpoints/{id}", s.auth(s.handleDelete))
	m.HandleFunc("GET /api/agents", s.auth(s.handleAgents))

	m.HandleFunc("GET /connect-vnc/{id}", s.handleConnectVNC)
	m.HandleFunc("GET /agent/ws", s.handleAgentWS)

	m.Handle("/", frontend)
	return m
}

// ---- auth ----

func (s *Server) newSession() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	sid := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[sid] = time.Now().Add(s.cfg.SessionTTL)
	s.mu.Unlock()
	return sid
}

func (s *Server) validSession(sid string) bool {
	if sid == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[sid]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, sid)
		return false
	}
	return true
}

func (s *Server) authed(r *http.Request) bool {
	c, err := r.Cookie("sid")
	if err != nil {
		return false
	}
	return s.validSession(c.Value)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(s.cfg.AdminPassword)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid password"})
		return
	}
	sid := s.newSession()
	http.SetCookie(w, &http.Cookie{
		Name: "sid", Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("sid"); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": s.authed(r)})
}

// ---- endpoints ----

type endpointReq struct {
	Name        string  `json:"name"`
	Addr        string  `json:"addr"`
	Agent       string  `json:"agent"`
	Description string  `json:"description"`
	Password    *string `json:"password"` // nil=keep, ""=clear, value=set
}

func (s *Server) validateAddr(addr, agent string) string {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || host == "" {
		return "addr must be host:port"
	}
	if p, err := strconv.Atoi(portStr); err != nil || p < 1 || p > 65535 {
		return "invalid port"
	}
	if agent != "" {
		return "" // agent-routed endpoints are resolved on the agent's LAN
	}
	if !netutil.HostAllowed(s.allow, host) {
		return "address is outside the permitted range (GLKVM_VNC_DIRECT_ALLOWLIST)"
	}
	return ""
}

func (s *Server) encodePassword(p *string, existing string) (string, string) {
	if p == nil {
		return existing, ""
	}
	if *p == "" {
		return "", ""
	}
	enc, err := vnccrypt.Encrypt(s.cfg.Secret, *p)
	if err != nil {
		return "", "server has no secret configured; cannot store a password"
	}
	return enc, ""
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req endpointReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name required"})
		return
	}
	if msg := s.validateAddr(req.Addr, req.Agent); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg})
		return
	}
	enc, perr := s.encodePassword(req.Password, "")
	if perr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": perr})
		return
	}
	id, err := s.store.Create(r.Context(), &store.Endpoint{
		Name: req.Name, Addr: strings.TrimSpace(req.Addr), Agent: strings.TrimSpace(req.Agent),
		Description: req.Description, PasswordEnc: enc,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad id"})
		return
	}
	var req endpointReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name required"})
		return
	}
	if msg := s.validateAddr(req.Addr, req.Agent); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg})
		return
	}
	existing, err := s.store.Get(r.Context(), id)
	if err != nil || existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	enc, perr := s.encodePassword(req.Password, existing.PasswordEnc)
	if perr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": perr})
		return
	}
	err = s.store.Update(r.Context(), &store.Endpoint{
		ID: id, Name: req.Name, Addr: strings.TrimSpace(req.Addr), Agent: strings.TrimSpace(req.Agent),
		Description: req.Description, PasswordEnc: enc,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad id"})
		return
	}
	if err := s.store.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.hub.ids()})
}

// ---- VNC bridge ----

func (s *Server) handleConnectVNC(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	ep, err := s.store.Get(r.Context(), id)
	if err != nil || ep == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Resolve transport before upgrading so failures are HTTP statuses.
	var agentSess *mux.Session
	if ep.Agent != "" {
		agentSess = s.hub.get(ep.Agent)
		if agentSess == nil {
			http.Error(w, "agent offline", http.StatusBadGateway)
			return
		}
	} else {
		host, _, _ := net.SplitHostPort(ep.Addr)
		if !netutil.HostAllowed(s.allow, host) {
			http.Error(w, "blocked by allowlist", http.StatusForbidden)
			return
		}
	}

	password, derr := vnccrypt.Decrypt(s.cfg.Secret, ep.PasswordEnc)
	if derr != nil {
		log.Printf("vnc: decrypt password for endpoint %d: %v", ep.ID, derr)
		password = ""
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var server net.Conn
	if agentSess != nil {
		st, oerr := agentSess.Open(ep.Addr)
		if oerr != nil {
			log.Printf("vnc: agent open %s: %v", ep.Addr, oerr)
			return
		}
		server = st
	} else {
		tcp, derr := net.DialTimeout("tcp", ep.Addr, 10*time.Second)
		if derr != nil {
			log.Printf("vnc: dial %s: %v", ep.Addr, derr)
			return
		}
		server = tcp
	}
	defer server.Close()

	if err := bridge(server, conn, password); err != nil {
		log.Printf("vnc: bridge %s: %v", ep.Addr, err)
	}
}

func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AgentToken == "" {
		http.Error(w, "agents disabled", http.StatusForbidden)
		return
	}
	token := r.URL.Query().Get("token")
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AgentToken)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	sess := mux.NewSession(conn)
	if old := s.hub.add(id, sess); old != nil {
		old.Close()
	}
	log.Printf("agent %q connected", id)
	<-sess.Closed()
	s.hub.remove(id, sess)
	log.Printf("agent %q disconnected", id)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

package gateway

import (
	"sort"
	"sync"

	"github.com/stoatworks-labs/glkvm-vnc/internal/mux"
)

// agentHub tracks connected reverse-tunnel agents by id. Each agent holds one
// mux.Session; the gateway opens a stream on it per browser VNC connection.
type agentHub struct {
	mu    sync.RWMutex
	byID  map[string]*mux.Session
}

func newAgentHub() *agentHub {
	return &agentHub{byID: make(map[string]*mux.Session)}
}

// add registers an agent session, replacing any previous session with the
// same id (returns the old one so the caller can close it).
func (h *agentHub) add(id string, s *mux.Session) *mux.Session {
	h.mu.Lock()
	defer h.mu.Unlock()
	old := h.byID[id]
	h.byID[id] = s
	return old
}

// remove deregisters an agent session if it is still the current one.
func (h *agentHub) remove(id string, s *mux.Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byID[id] == s {
		delete(h.byID, id)
	}
}

// get returns the session for id, or nil.
func (h *agentHub) get(id string) *mux.Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.byID[id]
}

// ids returns the sorted list of connected agent ids.
func (h *agentHub) ids() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.byID))
	for id := range h.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

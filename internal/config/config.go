// Package config loads gateway configuration from the environment.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"time"

	"github.com/stoatworks-labs/glkvm-vnc/internal/netutil"
)

// Config holds gateway settings.
type Config struct {
	Addr           string        // listen address, e.g. ":8600"
	AdminPassword  string        // required; password for the web UI
	Secret         string        // key for encrypting stored VNC passwords
	AgentToken     string        // shared secret reverse agents present
	DirectAllow    []string      // CIDR allowlist for direct dials ("" = all)
	DBPath         string        // sqlite path
	SessionTTL     time.Duration // web session lifetime
	TLSCert        string        // optional TLS cert path
	TLSKey         string        // optional TLS key path
	generatedAdmin bool
}

// GeneratedAdmin reports whether AdminPassword was auto-generated (so the
// caller can log it once).
func (c *Config) GeneratedAdmin() bool { return c.generatedAdmin }

// Load builds a Config from environment variables, applying defaults.
func Load() *Config {
	c := &Config{
		Addr:          envOr("GLKVM_VNC_ADDR", ":8600"),
		AdminPassword: os.Getenv("GLKVM_VNC_ADMIN_PASSWORD"),
		Secret:        os.Getenv("GLKVM_VNC_SECRET"),
		AgentToken:    os.Getenv("GLKVM_VNC_AGENT_TOKEN"),
		DBPath:        envOr("GLKVM_VNC_DB", "glkvm-vnc.db"),
		TLSCert:       os.Getenv("GLKVM_VNC_TLS_CERT"),
		TLSKey:        os.Getenv("GLKVM_VNC_TLS_KEY"),
		SessionTTL:    24 * time.Hour,
	}
	if v := strings.TrimSpace(os.Getenv("GLKVM_VNC_DIRECT_ALLOWLIST")); v != "" {
		c.DirectAllow = netutil.SplitCommaList(v)
	}
	if d := os.Getenv("GLKVM_VNC_SESSION_TTL"); d != "" {
		if parsed, err := time.ParseDuration(d); err == nil {
			c.SessionTTL = parsed
		}
	}
	if c.AdminPassword == "" {
		c.AdminPassword = randHex(12)
		c.generatedAdmin = true
	}
	// The password-encryption secret falls back to the admin password so a
	// single-variable deployment still encrypts stored VNC passwords.
	if c.Secret == "" {
		c.Secret = c.AdminPassword
	}
	return c
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

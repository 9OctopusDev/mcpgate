package legacy

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/9OctopusDev/mcpgate/internal/contracts"
)

var ErrSessionCapacity = errors.New("legacy session capacity reached")

type Session struct {
	ID           string
	ServerID     string
	Principal    contracts.Principal
	Capabilities map[string]bool
	Initialized  bool
	LastSeen     time.Time
}

type Sessions struct {
	mu      sync.Mutex
	maximum int
	idle    time.Duration
	now     func() time.Time
	entries map[string]*Session
}

func NewSessions(maximum int, idle time.Duration, now func() time.Time) *Sessions {
	if now == nil {
		now = time.Now
	}
	return &Sessions{maximum: maximum, idle: idle, now: now, entries: make(map[string]*Session)}
}

func (sessions *Sessions) Create(server string, principal contracts.Principal, capabilities map[string]bool) (Session, error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.expireLocked()
	if sessions.maximum <= 0 || len(sessions.entries) >= sessions.maximum {
		return Session{}, ErrSessionCapacity
	}
	created := Session{ID: sessionID(), ServerID: server, Principal: clonePrincipal(principal), Capabilities: cloneCapabilities(capabilities), LastSeen: sessions.now()}
	sessions.entries[created.ID] = &created
	return cloneSession(created), nil
}

func (sessions *Sessions) Get(id, server string, principal contracts.Principal) (Session, bool) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.expireLocked()
	session := sessions.entries[id]
	if session == nil || session.ServerID != server || !samePrincipal(session.Principal, principal) {
		return Session{}, false
	}
	session.LastSeen = sessions.now()
	return cloneSession(*session), true
}

func (sessions *Sessions) MarkInitialized(id string) bool {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	session := sessions.entries[id]
	if session == nil {
		return false
	}
	session.Initialized = true
	session.LastSeen = sessions.now()
	return true
}
func (sessions *Sessions) Delete(id string) {
	sessions.mu.Lock()
	delete(sessions.entries, id)
	sessions.mu.Unlock()
}
func (sessions *Sessions) Len() int {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.expireLocked()
	return len(sessions.entries)
}

func (sessions *Sessions) expireLocked() {
	now := sessions.now()
	for id, session := range sessions.entries {
		if now.Sub(session.LastSeen) >= sessions.idle {
			delete(sessions.entries, id)
		}
	}
}
func sessionID() string {
	var value [24]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}
func samePrincipal(a, b contracts.Principal) bool {
	return a.Issuer == b.Issuer && a.Subject == b.Subject && a.Tenant == b.Tenant && a.ClientID == b.ClientID && a.PartitionDigest == b.PartitionDigest
}
func clonePrincipal(value contracts.Principal) contracts.Principal {
	value.Scopes = append([]string(nil), value.Scopes...)
	return value
}
func cloneCapabilities(value map[string]bool) map[string]bool {
	result := make(map[string]bool, len(value))
	for key, enabled := range value {
		result[key] = enabled
	}
	return result
}
func cloneSession(value Session) Session {
	value.Principal = clonePrincipal(value.Principal)
	value.Capabilities = cloneCapabilities(value.Capabilities)
	return value
}

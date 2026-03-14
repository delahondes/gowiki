package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	CookieName        = "gowiki_session"
	DefaultSessionTTL = 24 * time.Hour
)

type Session struct {
	Username string    `json:"username"`
	Expiry   time.Time `json:"expiry"`
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]Session
	path     string
	ttl      time.Duration
}

func NewSessionStore(metaRoot string, ttl time.Duration) (*SessionStore, error) {
	path := filepath.Join(metaRoot, "sessions.json")
	s := &SessionStore{
		sessions: make(map[string]Session),
		path:     path,
		ttl:      ttl,
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	go s.cleanupLoop()
	return s, nil
}

func (s *SessionStore) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil // no sessions yet
	}
	if err != nil {
		return fmt.Errorf("read sessions file: %w", err)
	}
	var sessions map[string]Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return fmt.Errorf("parse sessions file: %w", err)
	}
	// Only load non-expired sessions.
	now := time.Now()
	for id, sess := range sessions {
		if now.Before(sess.Expiry) {
			s.sessions[id] = sess
		}
	}
	log.Printf("Loaded %d active sessions", len(s.sessions))
	return nil
}

func (s *SessionStore) saveLocked() {
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		log.Printf("WARNING: failed to marshal sessions: %v", err)
		return
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("WARNING: failed to create sessions dir: %v", err)
		return
	}

	tmp, err := os.CreateTemp(dir, "sessions-*.json.tmp")
	if err != nil {
		log.Printf("WARNING: failed to create sessions temp file: %v", err)
		return
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		log.Printf("WARNING: failed to write sessions temp file: %v", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		log.Printf("WARNING: failed to close sessions temp file: %v", err)
		return
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		log.Printf("WARNING: failed to rename sessions temp file: %v", err)
	}
}

func (s *SessionStore) Create(username string) string {
	id := generateSessionID()
	s.mu.Lock()
	s.sessions[id] = Session{
		Username: username,
		Expiry:   time.Now().Add(s.ttl),
	}
	s.saveLocked()
	s.mu.Unlock()
	return id
}

func (s *SessionStore) Get(sessionID string) (Session, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return Session{}, false
	}
	if time.Now().After(sess.Expiry) {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.saveLocked()
		s.mu.Unlock()
		return Session{}, false
	}
	return sess, true
}

func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.saveLocked()
	s.mu.Unlock()
}

func (s *SessionStore) cleanupLoop() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		changed := false
		for id, sess := range s.sessions {
			if now.After(sess.Expiry) {
				delete(s.sessions, id)
				changed = true
			}
		}
		if changed {
			s.saveLocked()
		}
		s.mu.Unlock()
	}
}

func (s *SessionStore) SetSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.ttl.Seconds()),
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

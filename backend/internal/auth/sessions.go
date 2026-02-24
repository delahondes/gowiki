package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	CookieName = "gowiki_session"
	SessionTTL = 24 * time.Hour
)

type Session struct {
	Username string
	Expiry   time.Time
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func NewSessionStore() *SessionStore {
	s := &SessionStore{sessions: make(map[string]Session)}
	go s.cleanupLoop()
	return s
}

func (s *SessionStore) Create(username string) string {
	id := generateSessionID()
	s.mu.Lock()
	s.sessions[id] = Session{
		Username: username,
		Expiry:   time.Now().Add(SessionTTL),
	}
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
		s.mu.Unlock()
		return Session{}, false
	}
	return sess, true
}

func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

func (s *SessionStore) cleanupLoop() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, sess := range s.sessions {
			if now.After(sess.Expiry) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

func SetSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionTTL.Seconds()),
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

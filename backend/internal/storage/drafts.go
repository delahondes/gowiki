package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LockInfo describes an active draft lock for admin listing.
type LockInfo struct {
	Page  string `json:"page"`
	Owner string `json:"owner"`
	Since string `json:"since"`
}

var (
	ErrPageLocked      = errors.New("page is locked by another user")
	ErrEditSuperseded  = errors.New("edit session superseded")
	ErrNotDraftOwner   = errors.New("not the draft owner")
	ErrNoDraft         = errors.New("no draft exists")
)

// DraftLock is stored in page metadata to track who is editing.
type DraftLock struct {
	Owner     string `json:"draft_owner,omitempty"`
	Since     string `json:"draft_since,omitempty"`
	EditToken string `json:"draft_edit_token,omitempty"`
}

// DraftStore manages draft files and edit locking.
type DraftStore struct {
	mu       sync.RWMutex
	dataDir  string // data/ root
	metaRoot string
}

func NewDraftStore(dataDir, metaRoot string) *DraftStore {
	return &DraftStore{dataDir: dataDir, metaRoot: metaRoot}
}

func (d *DraftStore) draftPath(username, pagePath string) string {
	return filepath.Join(d.dataDir, "drafts", username, filepath.FromSlash(pagePath)+".md")
}

// EnterEditMode creates or resumes a draft for the given page.
// Returns the draft markdown and a new edit token.
// If another user holds the lock, returns ErrPageLocked.
// If the same user already owns the lock, a new token is issued (seamless resume).
func (d *DraftStore) EnterEditMode(pagePath, username string, force bool, currentPublished string) (markdown string, editToken string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	lock, err := d.readLock(pagePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("read lock: %w", err)
	}

	if lock.Owner != "" && lock.Owner != username {
		return "", "", fmt.Errorf("%w: locked by %s", ErrPageLocked, lock.Owner)
	}

	// If the same user already owns the lock, allow resuming with a new token.
	// This handles server restarts (old session dead, lock file persists)
	// and "resume editing" from view mode.

	// Generate new edit token.
	editToken = generateEditToken()

	// Write lock metadata.
	newLock := DraftLock{
		Owner:     username,
		Since:     time.Now().UTC().Format(time.RFC3339),
		EditToken: editToken,
	}
	if err := d.writeLock(pagePath, newLock); err != nil {
		return "", "", err
	}

	// Try to read existing draft.
	draftFile := d.draftPath(username, pagePath)
	if data, err := os.ReadFile(draftFile); err == nil {
		return string(data), editToken, nil
	}

	// No existing draft — create from published content.
	if err := os.MkdirAll(filepath.Dir(draftFile), 0o755); err != nil {
		return "", "", fmt.Errorf("create draft dir: %w", err)
	}
	if err := writeFileAtomic(draftFile, []byte(currentPublished)); err != nil {
		return "", "", fmt.Errorf("write initial draft: %w", err)
	}
	return currentPublished, editToken, nil
}

// SaveDraft writes the draft content, validating the edit token.
func (d *DraftStore) SaveDraft(pagePath, username, editToken, markdown string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.validateToken(pagePath, username, editToken); err != nil {
		return err
	}

	draftFile := d.draftPath(username, pagePath)
	if err := os.MkdirAll(filepath.Dir(draftFile), 0o755); err != nil {
		return fmt.Errorf("create draft dir: %w", err)
	}
	return writeFileAtomic(draftFile, []byte(markdown))
}

// ReadDraft reads the draft for a user/page without modifying state.
func (d *DraftStore) ReadDraft(pagePath, username string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	data, err := os.ReadFile(d.draftPath(username, pagePath))
	if err != nil {
		return "", ErrNoDraft
	}
	return string(data), nil
}

// DiscardDraft removes the draft file and clears the lock.
func (d *DraftStore) DiscardDraft(pagePath, username string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	lock, _ := d.readLock(pagePath)
	if lock.Owner != "" && lock.Owner != username {
		return ErrNotDraftOwner
	}

	os.Remove(d.draftPath(username, pagePath))
	return d.clearLock(pagePath)
}

// Publish returns the draft content and clears the draft + lock.
// Caller is responsible for writing to the page store.
func (d *DraftStore) Publish(pagePath, username, editToken string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.validateToken(pagePath, username, editToken); err != nil {
		return "", err
	}

	draftFile := d.draftPath(username, pagePath)
	data, err := os.ReadFile(draftFile)
	if err != nil {
		return "", ErrNoDraft
	}

	os.Remove(draftFile)
	if err := d.clearLock(pagePath); err != nil {
		return "", err
	}

	return string(data), nil
}

// GetLock returns the current lock state for a page.
func (d *DraftStore) GetLock(pagePath string) DraftLock {
	d.mu.RLock()
	defer d.mu.RUnlock()
	lock, _ := d.readLock(pagePath)
	return lock
}

func (d *DraftStore) validateToken(pagePath, username, editToken string) error {
	lock, err := d.readLock(pagePath)
	if err != nil {
		return ErrNoDraft
	}
	if lock.Owner != username {
		if lock.Owner == "" {
			return ErrNoDraft
		}
		return ErrPageLocked
	}
	if lock.EditToken != editToken {
		return ErrEditSuperseded
	}
	return nil
}

func (d *DraftStore) lockPath(pagePath string) string {
	return filepath.Join(d.metaRoot, filepath.FromSlash(pagePath)+".lock.json")
}

func (d *DraftStore) readLock(pagePath string) (DraftLock, error) {
	data, err := os.ReadFile(d.lockPath(pagePath))
	if err != nil {
		return DraftLock{}, err
	}
	var lock DraftLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return DraftLock{}, err
	}
	return lock, nil
}

func (d *DraftStore) writeLock(pagePath string, lock DraftLock) error {
	p := d.lockPath(pagePath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(lock, "", "  ")
	data = append(data, '\n')
	return writeFileAtomic(p, data)
}

func (d *DraftStore) clearLock(pagePath string) error {
	p := d.lockPath(pagePath)
	err := os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// Also try to clean up empty parent directories.
	dir := filepath.Dir(p)
	for dir != d.metaRoot && dir != "." {
		if rmErr := os.Remove(dir); rmErr != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	return err
}

// CleanStaleLocks removes lock files where no corresponding draft exists.
// This handles crash recovery where the server died between operations.
func (d *DraftStore) CleanStaleLocks() error {
	return filepath.Walk(d.metaRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".lock.json") {
			return nil
		}

		lock, readErr := func() (DraftLock, error) {
			data, e := os.ReadFile(path)
			if e != nil {
				return DraftLock{}, e
			}
			var l DraftLock
			e = json.Unmarshal(data, &l)
			return l, e
		}()
		if readErr != nil {
			return nil
		}

		// If no draft file exists for this lock, the lock is stale.
		rel, _ := filepath.Rel(d.metaRoot, path)
		pagePath := strings.TrimSuffix(filepath.ToSlash(rel), ".lock.json")
		if lock.Owner != "" {
			draftFile := d.draftPath(lock.Owner, pagePath)
			if !fileExists(draftFile) {
				os.Remove(path)
			}
		}
		return nil
	})
}

// ListLocks returns all current draft locks across the wiki.
func (d *DraftStore) ListLocks() []LockInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var locks []LockInfo
	_ = filepath.Walk(d.metaRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".lock.json") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var lock DraftLock
		if json.Unmarshal(data, &lock) != nil {
			return nil
		}
		if lock.Owner == "" {
			return nil
		}

		// Derive page path from file path: strip metaRoot prefix and .lock.json suffix.
		rel, relErr := filepath.Rel(d.metaRoot, path)
		if relErr != nil {
			return nil
		}
		pagePath := strings.TrimSuffix(filepath.ToSlash(rel), ".lock.json")

		locks = append(locks, LockInfo{
			Page:  pagePath,
			Owner: lock.Owner,
			Since: lock.Since,
		})
		return nil
	})

	sort.Slice(locks, func(i, j int) bool {
		return locks[i].Page < locks[j].Page
	})

	return locks
}

// AdminDiscardDraft forcefully discards any user's draft, regardless of ownership.
// This is used for admin override — it does not check that the caller is the draft owner.
func (d *DraftStore) AdminDiscardDraft(pagePath, draftOwner string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Remove the draft file.
	os.Remove(d.draftPath(draftOwner, pagePath))

	// Clear the lock file.
	return d.clearLock(pagePath)
}

func generateEditToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

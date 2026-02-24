package storage

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Changelog manages the append-only global changes log.
type Changelog struct {
	mu   sync.Mutex
	path string
}

func NewChangelog(dataDir string) *Changelog {
	return &Changelog{path: filepath.Join(dataDir, "changes.log")}
}

// Append writes a line to the changes log.
// Format: timestamp\tpage\tversion\tauthor\tsummary\ttype
func (c *Changelog) Append(pagePath string, version int64, author, summary, changeType string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	line := fmt.Sprintf("%s\t%s\t%d\t%s\t%s\t%s\n",
		time.Now().UTC().Format(time.RFC3339),
		pagePath,
		version,
		author,
		summary,
		changeType,
	)

	f, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return // best effort
	}
	defer f.Close()
	f.WriteString(line)
}

func md5sum(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

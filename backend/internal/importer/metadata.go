package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// PageMetadata matches the Gowiki backend PageMetadata struct.
type PageMetadata struct {
	ID        string         `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Version   int64          `json:"version"`
	Author    string         `json:"author,omitempty"`
	CreatedBy string         `json:"created_by,omitempty"`
	MediaRefs map[string]int64 `json:"media_refs,omitempty"`
}

// ParseDokuWikiMeta reads a DokuWiki .meta file (PHP serialized) and extracts
// relevant metadata fields.
func ParseDokuWikiMeta(data []byte) (*PageMetadata, error) {
	meta := &PageMetadata{
		Version: 1,
	}

	// Parse the PHP serialized data
	parsed, err := phpUnserialize(data)
	if err != nil {
		return meta, fmt.Errorf("php unserialize: %w", err)
	}

	obj, ok := parsed.(map[string]any)
	if !ok {
		return meta, nil
	}

	// Extract date.created and date.modified
	if dateMap, ok := getMap(obj, "date"); ok {
		if created, ok := getInt(dateMap, "created"); ok {
			meta.CreatedAt = time.Unix(created, 0)
		}
		if modified, ok := getInt(dateMap, "modified"); ok {
			meta.UpdatedAt = time.Unix(modified, 0)
		}
	}

	// Extract creator
	if creator, ok := getString(obj, "creator"); ok {
		meta.CreatedBy = creator
	}

	// Extract last_change.user
	if lc, ok := getMap(obj, "last_change"); ok {
		if user, ok := getString(lc, "user"); ok {
			meta.Author = user
		}
	}

	return meta, nil
}

// ParseDokuWikiChangelog reads a .changes file (TSV) and extracts the creation
// info from the first entry and author from the last entry.
func ParseDokuWikiChangelog(data []byte) (createdAt time.Time, createdBy string, updatedAt time.Time, updatedBy string) {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 5 {
			continue
		}

		var ts int64
		fmt.Sscanf(fields[0], "%d", &ts)
		t := time.Unix(ts, 0)
		user := fields[4]

		if createdAt.IsZero() {
			createdAt = t
			createdBy = user
		}
		updatedAt = t
		updatedBy = user
	}
	return
}

// ReadMetaFile reads a .meta file if it exists.
func ReadMetaFile(path string) (*PageMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return ParseDokuWikiMeta(data)
}

// WriteMetaJSON writes a PageMetadata to a JSON file.
func WriteMetaJSON(path string, meta *PageMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Helper functions for navigating parsed PHP arrays.

func getMap(obj map[string]any, key string) (map[string]any, bool) {
	v, ok := obj[key]
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

func getString(obj map[string]any, key string) (string, bool) {
	v, ok := obj[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getInt(obj map[string]any, key string) (int64, bool) {
	v, ok := obj[key]
	if !ok {
		return 0, false
	}
	switch i := v.(type) {
	case int64:
		return i, true
	case int:
		return int64(i), true
	case float64:
		return int64(i), true
	default:
		return 0, false
	}
}

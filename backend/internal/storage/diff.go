package storage

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// DiffHunk represents one piece of a diff result.
type DiffHunk struct {
	Op      string `json:"op"`      // "equal", "insert", "delete"
	Content string `json:"content"` // the text
}

// DiffLines produces a line-based diff between two strings.
func DiffLines(textA, textB string) []DiffHunk {
	dmp := diffmatchpatch.New()

	// Use line-level diffing for cleaner output.
	a, b, lines := dmp.DiffLinesToChars(textA, textB)
	diffs := dmp.DiffMain(a, b, false)
	diffs = dmp.DiffCharsToLines(diffs, lines)
	diffs = dmp.DiffCleanupSemantic(diffs)

	hunks := make([]DiffHunk, 0, len(diffs))
	for _, d := range diffs {
		var op string
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			op = "equal"
		case diffmatchpatch.DiffInsert:
			op = "insert"
		case diffmatchpatch.DiffDelete:
			op = "delete"
		}
		// Split into individual lines for finer-grained rendering.
		text := d.Text
		if text == "" {
			continue
		}
		lines := splitKeepNewlines(text)
		for _, line := range lines {
			hunks = append(hunks, DiffHunk{Op: op, Content: line})
		}
	}
	return hunks
}

// splitKeepNewlines splits text into lines, keeping trailing newlines.
func splitKeepNewlines(text string) []string {
	if text == "" {
		return nil
	}
	raw := strings.Split(text, "\n")
	var result []string
	for i, line := range raw {
		if i < len(raw)-1 {
			result = append(result, line+"\n")
		} else if line != "" {
			result = append(result, line)
		}
	}
	return result
}

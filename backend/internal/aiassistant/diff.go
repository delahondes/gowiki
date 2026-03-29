package aiassistant

import "strings"

// Edit represents a single text replacement with an explanatory comment.
type Edit struct {
	OldStart int    `json:"old_start"` // line number (0-based) in original
	OldEnd   int    `json:"old_end"`   // exclusive end line in original
	OldText  string `json:"old_text"`  // original text of the region
	NewText  string `json:"new_text"`  // replacement text
	Comment  string `json:"comment"`   // AI-generated explanation
}

// ComputeEdits compares original and modified markdown and returns
// a list of minimal edits. Uses a simple line-based diff that groups
// consecutive changed lines into single edits.
func ComputeEdits(original, modified string) []Edit {
	oldLines := strings.Split(original, "\n")
	newLines := strings.Split(modified, "\n")

	// Compute LCS-based diff using a simple O(nm) DP approach.
	// For typical wiki pages (< 1000 lines) this is fast enough.
	lcs := lcsTable(oldLines, newLines)
	ops := backtrack(lcs, oldLines, newLines, len(oldLines), len(newLines))

	// Group consecutive changes into edits.
	return groupEdits(ops, oldLines, newLines)
}

type diffOp struct {
	kind byte // '=' keep, '-' delete, '+' insert
	oldIdx int // line index in original (-1 for inserts)
	newIdx int // line index in modified (-1 for deletes)
}

func lcsTable(a, b []string) [][]int {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return dp
}

func backtrack(dp [][]int, a, b []string, i, j int) []diffOp {
	var ops []diffOp
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			ops = append(ops, diffOp{'=', i - 1, j - 1})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			ops = append(ops, diffOp{'+', -1, j - 1})
			j--
		} else {
			ops = append(ops, diffOp{'-', i - 1, -1})
			i--
		}
	}
	// Reverse to get forward order.
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	return ops
}

func groupEdits(ops []diffOp, oldLines, newLines []string) []Edit {
	var edits []Edit
	i := 0
	for i < len(ops) {
		if ops[i].kind == '=' {
			i++
			continue
		}

		// Start of a changed region.
		var oldStart, oldEnd int
		var deletedLines, insertedLines []string
		oldStart = -1

		for i < len(ops) && ops[i].kind != '=' {
			op := ops[i]
			if op.kind == '-' {
				if oldStart == -1 {
					oldStart = op.oldIdx
				}
				oldEnd = op.oldIdx + 1
				deletedLines = append(deletedLines, oldLines[op.oldIdx])
			} else { // '+'
				if oldStart == -1 {
					// Pure insertion — position it at the next old line.
					if i+1 < len(ops) && ops[i+1].kind == '=' {
						oldStart = ops[i+1].oldIdx
					} else {
						oldStart = len(oldLines)
					}
					oldEnd = oldStart
				}
				insertedLines = append(insertedLines, newLines[op.newIdx])
			}
			i++
		}

		if oldStart == -1 {
			continue
		}

		edit := Edit{
			OldStart: oldStart,
			OldEnd:   oldEnd,
			OldText:  strings.Join(deletedLines, "\n"),
			NewText:  strings.Join(insertedLines, "\n"),
		}
		edits = append(edits, edit)
	}
	return edits
}

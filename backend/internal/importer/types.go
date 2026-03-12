package importer

import "strings"

// ConvertImageSize converts a DokuWiki image size to Gowiki format.
// DokuWiki: "200" or "200x100"
// Gowiki:   "200px" or "200px;100px"
func ConvertImageSize(dwSize string) string {
	if dwSize == "" {
		return ""
	}
	if strings.Contains(dwSize, "x") {
		parts := strings.SplitN(dwSize, "x", 2)
		w := strings.TrimSpace(parts[0])
		h := strings.TrimSpace(parts[1])
		if h == "" {
			return w + "px"
		}
		return w + "px;" + h + "px"
	}
	return dwSize + "px"
}

// FlaggedLine records a line that could not be fully converted.
type FlaggedLine struct {
	LineNum int
	Reason  string
	Content string
}

// ConvertResult is the output of converting a single page.
type ConvertResult struct {
	Markdown     string
	Flagged      []FlaggedLine
	TotalLines   int
	ConvertLines int
}

// PageReport collects per-page statistics.
type PageReport struct {
	SourcePath   string
	DestPath     string
	TotalLines   int
	ConvertLines int
	Flagged      []FlaggedLine
}

// Report aggregates statistics for the whole import.
type Report struct {
	TotalPages   int
	TotalLines   int
	ConvertLines int
	FlaggedLines int
	Pages        []PageReport
	Features     map[string]int // feature name -> count of unconverted occurrences
	MediaCopied  int
	MediaMissing []string
}

func NewReport() *Report {
	return &Report{Features: make(map[string]int)}
}

func (r *Report) AddPage(pr PageReport) {
	r.TotalPages++
	r.TotalLines += pr.TotalLines
	r.ConvertLines += pr.ConvertLines
	r.FlaggedLines += len(pr.Flagged)
	r.Pages = append(r.Pages, pr)
}

func (r *Report) Flag(feature string) {
	r.Features[feature]++
}

package importer

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

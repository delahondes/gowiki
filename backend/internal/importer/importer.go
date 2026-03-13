package importer

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Options configures the importer.
type Options struct {
	SrcDir  string // DokuWiki import root (contains data/ and conf/)
	DestDir string // Gowiki data root (will contain content/, meta/)
	DryRun  bool
	Verbose bool
}

// SrcDataDir returns the DokuWiki data directory (SrcDir/data/).
func (o Options) SrcDataDir() string {
	return filepath.Join(o.SrcDir, "data")
}

// convertedPage holds the result of converting a single page,
// collected before writing to disk so namespace conflicts can be resolved.
type convertedPage struct {
	relPath  string // source relative path
	destPath string // destination relative path
	markdown string // converted markdown content
	meta     *PageMetadata
	metaDest string // destination meta relative path
}

// Run executes the full import.
func Run(opts Options) (*Report, error) {
	report := NewReport()

	dataDir := opts.SrcDataDir()
	pagesDir := filepath.Join(dataDir, "pages")
	mediaDir := filepath.Join(dataDir, "media")
	metaDir := filepath.Join(dataDir, "meta")
	contentDir := filepath.Join(opts.DestDir, "content")
	destMetaDir := filepath.Join(opts.DestDir, "meta")

	// Phase 1a: Convert all pages (collect in memory)
	log.Println("Phase 1: Converting pages...")
	var pages []convertedPage

	err := filepath.Walk(pagesDir, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".txt") {
			return nil
		}

		// Get relative path from pages/
		relPath, _ := filepath.Rel(pagesDir, srcPath)
		relPath = filepath.ToSlash(relPath)

		// Convert path
		var destRelPath string
		if IsTemplatePath(relPath) {
			destRelPath = TemplateSourceToTarget(relPath)
		} else {
			destRelPath = PageSourceToTarget(relPath)
		}

		// Read source
		content, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcPath, err)
		}

		// Skip empty pages
		if len(strings.TrimSpace(string(content))) == 0 {
			if opts.Verbose {
				log.Printf("  skip (empty): %s", relPath)
			}
			return nil
		}

		// Convert
		result := ConvertPage(string(content), relPath, pagesDir)

		// Build page report
		pr := PageReport{
			SourcePath:   relPath,
			DestPath:     destRelPath,
			TotalLines:   result.TotalLines,
			ConvertLines: result.ConvertLines,
			Flagged:      result.Flagged,
		}
		report.AddPage(pr)

		for _, f := range result.Flagged {
			report.Flag(f.Reason)
		}

		if opts.Verbose {
			pct := float64(0)
			if result.TotalLines > 0 {
				pct = float64(result.ConvertLines) / float64(result.TotalLines) * 100
			}
			log.Printf("  %s -> %s (%d lines, %.0f%% converted, %d flagged)",
				relPath, destRelPath, result.TotalLines, pct, len(result.Flagged))
		}

		// Extract metadata
		metaRelPath := strings.TrimSuffix(relPath, ".txt") + ".meta"
		metaSrcPath := filepath.Join(metaDir, metaRelPath)
		meta := extractMetadata(metaSrcPath, metaDir, relPath, result.Markdown)
		metaDestRelPath := strings.TrimSuffix(destRelPath, ".md") + ".json"

		pages = append(pages, convertedPage{
			relPath:  relPath,
			destPath: destRelPath,
			markdown: result.Markdown,
			meta:     meta,
			metaDest: metaDestRelPath,
		})

		return nil
	})
	if err != nil {
		return report, fmt.Errorf("page walk: %w", err)
	}

	// Phase 1b: Resolve namespace conflicts.
	// In DokuWiki, both ns:page and ns:page:start can exist.
	// In Gowiki, page.md and page/index.md cannot coexist.
	// When both exist, prepend page.md content to page/index.md.
	pages = resolveNamespaceConflicts(pages, opts.Verbose)

	// Phase 1c: Write pages to disk
	if !opts.DryRun {
		for _, p := range pages {
			destPath := filepath.Join(contentDir, p.destPath)
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return report, fmt.Errorf("mkdir %s: %w", filepath.Dir(destPath), err)
			}
			if err := os.WriteFile(destPath, []byte(p.markdown), 0644); err != nil {
				return report, fmt.Errorf("write %s: %w", destPath, err)
			}

			metaDestPath := filepath.Join(destMetaDir, p.metaDest)
			if err := os.MkdirAll(filepath.Dir(metaDestPath), 0755); err != nil {
				return report, fmt.Errorf("mkdir meta %s: %w", filepath.Dir(metaDestPath), err)
			}
			if err := WriteMetaJSON(metaDestPath, p.meta); err != nil {
				return report, fmt.Errorf("write meta %s: %w", metaDestPath, err)
			}
		}
	}

	// Phase 2: Copy media files
	log.Println("Phase 2: Copying media files...")
	if _, err := os.Stat(mediaDir); err == nil {
		err = filepath.Walk(mediaDir, func(srcPath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}

			relPath, _ := filepath.Rel(mediaDir, srcPath)
			relPath = filepath.ToSlash(relPath)
			destPath := filepath.Join(contentDir, relPath)

			if opts.Verbose {
				log.Printf("  media: %s", relPath)
			}

			if opts.DryRun {
				report.MediaCopied++
				return nil
			}

			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return err
			}

			if err := copyFile(srcPath, destPath); err != nil {
				return fmt.Errorf("copy media %s: %w", relPath, err)
			}
			report.MediaCopied++
			return nil
		})
		if err != nil {
			return report, fmt.Errorf("media walk: %w", err)
		}
	}

	// Phase 3: Write report
	if !opts.DryRun {
		reportPath := filepath.Join(contentDir, "import_report.md")
		reportContent := report.Markdown()
		if err := os.WriteFile(reportPath, []byte(reportContent), 0644); err != nil {
			return report, fmt.Errorf("write report: %w", err)
		}
		log.Printf("Report written to %s", reportPath)
	}

	return report, nil
}

// resolveNamespaceConflicts handles two cases:
// 1. Both page.md and page/index.md exist → merge page.md into page/index.md
// 2. page.md exists and page/*.md exists (no index) → rename page.md to page/index.md
func resolveNamespaceConflicts(pages []convertedPage, verbose bool) []convertedPage {
	// Build index: destPath -> slice index
	byDest := make(map[string]int, len(pages))
	for i, p := range pages {
		byDest[p.destPath] = i
	}

	// Collect all namespace prefixes (directories implied by paths)
	namespaces := make(map[string]bool)
	for _, p := range pages {
		dir := filepath.Dir(p.destPath)
		for dir != "" && dir != "." {
			namespaces[dir] = true
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	toRemove := make(map[int]bool)
	for i, p := range pages {
		if strings.HasSuffix(p.destPath, "/index.md") {
			continue
		}
		nsPrefix := strings.TrimSuffix(p.destPath, ".md")
		nsIndex := nsPrefix + "/index.md"

		if j, ok := byDest[nsIndex]; ok {
			// Case 1: Both page.md and page/index.md exist → merge
			if verbose {
				log.Printf("  namespace conflict: %s merged into %s", p.destPath, pages[j].destPath)
			}
			pages[j].markdown = p.markdown + "\n\n---\n\n" + pages[j].markdown
			toRemove[i] = true
		} else if namespaces[nsPrefix] {
			// Case 2: page.md exists and page/ has subpages (no index) → rename to page/index.md
			if verbose {
				log.Printf("  namespace conflict: %s renamed to %s", p.destPath, nsIndex)
			}
			pages[i].destPath = nsIndex
			pages[i].metaDest = strings.TrimSuffix(nsIndex, ".md") + ".json"
		}
	}

	if len(toRemove) == 0 {
		return pages
	}

	// Filter out merged pages
	result := make([]convertedPage, 0, len(pages)-len(toRemove))
	for i, p := range pages {
		if !toRemove[i] {
			result = append(result, p)
		}
	}
	return result
}

// extractMetadata tries to read DokuWiki metadata and build a PageMetadata.
func extractMetadata(metaSrcPath, metaDir, relPath, markdown string) *PageMetadata {
	meta := &PageMetadata{
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Generate ID from path
	h := sha1.New()
	h.Write([]byte(relPath))
	meta.ID = hex.EncodeToString(h.Sum(nil))[:12]

	// Try .meta file (PHP serialized)
	if m, err := ReadMetaFile(metaSrcPath); err == nil && m != nil {
		if !m.CreatedAt.IsZero() {
			meta.CreatedAt = m.CreatedAt
		}
		if !m.UpdatedAt.IsZero() {
			meta.UpdatedAt = m.UpdatedAt
		}
		if m.CreatedBy != "" {
			meta.CreatedBy = m.CreatedBy
		}
		if m.Author != "" {
			meta.Author = m.Author
		}
	}

	// Try .changes file (TSV changelog)
	changesRelPath := strings.TrimSuffix(relPath, ".txt") + ".changes"
	changesSrcPath := filepath.Join(metaDir, changesRelPath)
	if data, err := os.ReadFile(changesSrcPath); err == nil {
		createdAt, createdBy, updatedAt, updatedBy := ParseDokuWikiChangelog(data)
		if meta.CreatedAt.Equal(time.Now()) && !createdAt.IsZero() {
			meta.CreatedAt = createdAt
		}
		if meta.CreatedBy == "" && createdBy != "" {
			meta.CreatedBy = createdBy
		}
		if !updatedAt.IsZero() {
			meta.UpdatedAt = updatedAt
		}
		if updatedBy != "" {
			meta.Author = updatedBy
		}
	}

	return meta
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

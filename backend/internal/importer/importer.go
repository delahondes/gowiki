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
	SrcDir  string // olddata root (contains pages/, media/, meta/)
	DestDir string // data root (will contain content/, meta/)
	DryRun  bool
	Verbose bool
}

// Run executes the full import.
func Run(opts Options) (*Report, error) {
	report := NewReport()

	pagesDir := filepath.Join(opts.SrcDir, "pages")
	mediaDir := filepath.Join(opts.SrcDir, "media")
	metaDir := filepath.Join(opts.SrcDir, "meta")
	contentDir := filepath.Join(opts.DestDir, "content")
	destMetaDir := filepath.Join(opts.DestDir, "meta")

	// Phase 1: Convert pages
	log.Println("Phase 1: Converting pages...")
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

		// Track flagged features
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

		if opts.DryRun {
			return nil
		}

		// Write converted page
		destPath := filepath.Join(contentDir, destRelPath)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(destPath), err)
		}
		if err := os.WriteFile(destPath, []byte(result.Markdown), 0644); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}

		// Try to extract and write metadata
		metaRelPath := strings.TrimSuffix(relPath, ".txt") + ".meta"
		metaSrcPath := filepath.Join(metaDir, metaRelPath)
		meta := extractMetadata(metaSrcPath, metaDir, relPath, result.Markdown)

		metaDestRelPath := strings.TrimSuffix(destRelPath, ".md") + ".json"
		metaDestPath := filepath.Join(destMetaDir, metaDestRelPath)
		if err := os.MkdirAll(filepath.Dir(metaDestPath), 0755); err != nil {
			return fmt.Errorf("mkdir meta %s: %w", filepath.Dir(metaDestPath), err)
		}
		if err := WriteMetaJSON(metaDestPath, meta); err != nil {
			return fmt.Errorf("write meta %s: %w", metaDestPath, err)
		}

		return nil
	})
	if err != nil {
		return report, fmt.Errorf("page walk: %w", err)
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

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"gowiki/backend/internal/importer"
)

func main() {
	var (
		srcDir        = flag.String("src", "", "DokuWiki import root (contains data/ and conf/ subdirectories)")
		destDir       = flag.String("dest", "", "Gowiki data directory (will contain content/, meta/)")
		dryRun        = flag.Bool("dry-run", false, "Analyze and report without writing files")
		verbose       = flag.Bool("verbose", false, "Log each file being processed")
		fallbackAdmin = flag.Bool("fallback-admin", false, "Create an admin/admin user with full permissions (useful when conf/ is missing)")
	)
	flag.Parse()

	if *srcDir == "" || *destDir == "" {
		fmt.Fprintln(os.Stderr, "Both -src and -dest are required")
		fmt.Fprintln(os.Stderr, "Usage: import -src ./import -dest ./backend/data")
		os.Exit(1)
	}

	opts := importer.Options{
		SrcDir:        *srcDir,
		DestDir:       *destDir,
		DryRun:        *dryRun,
		Verbose:       *verbose,
		FallbackAdmin: *fallbackAdmin,
	}

	// Verify source data directory exists
	if _, err := os.Stat(opts.SrcDataDir()); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Source data directory does not exist: %s\n", opts.SrcDataDir())
		os.Exit(1)
	}

	log.Printf("DokuWiki importer")
	log.Printf("  Source: %s", opts.SrcDir)
	log.Printf("  Destination: %s", opts.DestDir)
	if opts.DryRun {
		log.Printf("  Mode: DRY RUN (no files will be written)")
	}

	report, err := importer.Run(opts)
	if err != nil {
		log.Fatalf("Import failed: %v", err)
	}

	// Phase 4: Import DokuWiki version history (attic).
	if err := importer.ImportAttic(opts); err != nil {
		log.Printf("WARNING: attic import failed: %v", err)
	}

	// Phase 5: Import DokuWiki users and ACL rules (from conf/).
	if err := importer.ImportAuth(opts); err != nil {
		log.Printf("WARNING: auth import failed: %v", err)
	}

	// Print summary
	convPct := float64(0)
	if report.TotalLines > 0 {
		convPct = float64(report.ConvertLines) / float64(report.TotalLines) * 100
	}

	fmt.Println()
	fmt.Println("=== Import Summary ===")
	fmt.Printf("Pages:           %d\n", report.TotalPages)
	fmt.Printf("Total lines:     %d\n", report.TotalLines)
	fmt.Printf("Converted lines: %d\n", report.ConvertLines)
	fmt.Printf("Flagged lines:   %d\n", report.FlaggedLines)
	fmt.Printf("Conversion rate: %.1f%%\n", convPct)
	fmt.Printf("Media copied:    %d\n", report.MediaCopied)
	fmt.Printf("Media missing:   %d\n", len(report.MediaMissing))

	if len(report.Features) > 0 {
		fmt.Println("\nFlagged features:")
		for feature, count := range report.Features {
			fmt.Printf("  %-25s %d\n", feature, count)
		}
	}

	if convPct < 90 {
		fmt.Printf("\nWARNING: Conversion rate %.1f%% is below 90%% target\n", convPct)
	}
}

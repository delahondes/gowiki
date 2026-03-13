package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var legacyMap = map[string]string{
	"1st_row": "1r",
	"2_rows":  "2r",
	"1st_col": "1c",
	"2_cols":  "2c",
	"both":    "1r1c",
}

var reHeaders = regexp.MustCompile(`(headers=)(1st_row|2_rows|1st_col|2_cols|both)`)

func migrate(content string) (string, int) {
	count := 0
	result := reHeaders.ReplaceAllStringFunc(content, func(m string) string {
		sub := reHeaders.FindStringSubmatch(m)
		if newVal, ok := legacyMap[sub[2]]; ok {
			count++
			return sub[1] + newVal
		}
		return m
	})
	return result, count
}

func main() {
	contentDir := flag.String("dir", "./data/content", "content directory to scan")
	dryRun := flag.Bool("dry-run", false, "show what would be changed without writing")
	flag.Parse()

	totalFiles := 0
	totalReplacements := 0

	err := filepath.Walk(*contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		content := string(data)
		if !reHeaders.MatchString(content) {
			return nil
		}

		newContent, count := migrate(content)
		if count == 0 {
			return nil
		}

		rel, _ := filepath.Rel(*contentDir, path)
		if *dryRun {
			fmt.Printf("  [dry-run] %s: %d replacement(s)\n", rel, count)
		} else {
			if err := os.WriteFile(path, []byte(newContent), info.Mode()); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			fmt.Printf("  migrated %s: %d replacement(s)\n", rel, count)
		}
		totalFiles++
		totalReplacements += count
		return nil
	})

	if err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	if *dryRun {
		fmt.Printf("\nDry run complete: %d file(s), %d replacement(s) would be made\n", totalFiles, totalReplacements)
	} else {
		fmt.Printf("\nMigration complete: %d file(s), %d replacement(s)\n", totalFiles, totalReplacements)
	}
}

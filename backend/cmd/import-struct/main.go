package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"gowiki/backend/internal/database"
)

// DokuWiki struct JSON schema
type DWStruct struct {
	Schema  string     `json:"schema"`
	Columns []DWColumn `json:"columns"`
}

type DWColumn struct {
	ColRef    int      `json:"colref"`
	IsMulti   bool     `json:"ismulti"`
	IsEnabled bool     `json:"isenabled"`
	Sort      int      `json:"sort"`
	Label     string   `json:"label"`
	Class     string   `json:"class"`
	Config    DWConfig `json:"config"`
}

type DWConfig struct {
	Values string `json:"values"`
	Schema string `json:"schema"` // for Status/Lookup fields
	Format string `json:"format"`
}

func main() {
	var (
		structDir = flag.String("dir", "", "path to import/struct directory")
		dsn       = flag.String("dsn", "", "PostgreSQL connection string")
		dryRun    = flag.Bool("dry-run", false, "show what would be imported without writing")
	)
	flag.Parse()

	if *structDir == "" || *dsn == "" {
		fmt.Fprintln(os.Stderr, "Usage: import-struct -dir ./import/struct -dsn 'postgres://...'")
		os.Exit(1)
	}

	// Connect to database.
	pool := database.NewPool()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := pool.Connect(ctx, *dsn); err != nil {
		log.Fatalf("database connect: %v", err)
	}
	log.Printf("database: connected")

	if err := database.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	schemaStore := database.NewSchemaStore(pool)
	dataStore := database.NewDataStore(pool, schemaStore)

	// List subdirectories in struct dir.
	entries, err := os.ReadDir(*structDir)
	if err != nil {
		log.Fatalf("read struct dir: %v", err)
	}

	// Reference/status tables that must be imported first (depended on by others).
	referenceFirst := []string{
		"incident_status",
		"server_monitor_status",
		"server_provider",
		"smqkpi_status",
	}

	// Build ordered list: reference tables first, then the rest alphabetically.
	refSet := make(map[string]bool)
	for _, r := range referenceFirst {
		refSet[r] = true
	}
	var ordered []string
	ordered = append(ordered, referenceFirst...)
	for _, entry := range entries {
		if !entry.IsDir() || refSet[entry.Name()] {
			continue
		}
		ordered = append(ordered, entry.Name())
	}

	for _, name := range ordered {
		dir := filepath.Join(*structDir, name)

		// Find struct JSON and CSV files.
		jsonFile := filepath.Join(dir, name+".struct.json")
		csvFile := filepath.Join(dir, name+".csv")

		if _, err := os.Stat(jsonFile); err != nil {
			log.Printf("SKIP %s: no %s.struct.json", name, name)
			continue
		}

		log.Printf("─── %s ───", name)

		if err := importTable(ctx, schemaStore, dataStore, jsonFile, csvFile, *dryRun); err != nil {
			log.Printf("ERROR %s: %v", name, err)
		}
	}

	log.Printf("done")
}

func importTable(ctx context.Context, schemaStore *database.SchemaStore, dataStore *database.DataStore, jsonFile, csvFile string, dryRun bool) error {
	// Parse struct definition.
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return fmt.Errorf("read json: %w", err)
	}
	var dw DWStruct
	if err := json.Unmarshal(data, &dw); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}

	tableName := sanitizeName(dw.Schema)

	// Collect enabled columns.
	type fieldMapping struct {
		dwCol      DWColumn
		gowikiName string
		gowikiType string
		enumValues []string
		foreignKey string
	}

	var fields []fieldMapping
	for _, col := range dw.Columns {
		if !col.IsEnabled {
			continue
		}
		gType, enumVals, fk := mapFieldType(col)
		gName := sanitizeName(col.Label)
		if gName == "" {
			continue
		}
		fields = append(fields, fieldMapping{
			dwCol:      col,
			gowikiName: gName,
			gowikiType: gType,
			enumValues: enumVals,
			foreignKey: fk,
		})
		log.Printf("  field %s (%s) → %s %s", col.Label, col.Class, gName, gType)
		if len(enumVals) > 0 {
			log.Printf("    values: %v", enumVals)
		}
		if fk != "" {
			log.Printf("    foreign_key: %s", fk)
		}
	}

	if dryRun {
		log.Printf("  [dry-run] would create table %q with %d fields", tableName, len(fields))
		return nil
	}

	// Check if table already exists.
	existing, _ := schemaStore.GetTableByName(ctx, tableName)
	if existing != nil {
		log.Printf("  table %q already exists (id=%d), skipping schema creation", tableName, existing.ID)
	} else {
		// Create table.
		table := &database.TableDef{
			Name:  tableName,
			Label: dw.Schema,
		}
		if err := schemaStore.CreateTable(ctx, table, "import"); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
		log.Printf("  created table %q (id=%d)", tableName, table.ID)

		// Create fields.
		for i, fm := range fields {
			f := &database.FieldDef{
				TableID:      table.ID,
				Name:         fm.gowikiName,
				Label:        fm.dwCol.Label,
				Type:         fm.gowikiType,
				DisplayOrder: (i + 1) * 10,
				ForeignKey:   fm.foreignKey,
				EnumValues:   fm.enumValues,
			}
			if err := schemaStore.CreateField(ctx, f, "import"); err != nil {
				return fmt.Errorf("create field %q: %w", fm.gowikiName, err)
			}
			log.Printf("  created field %q (%s) id=%d", fm.gowikiName, fm.gowikiType, f.ID)
		}
	}

	// Import CSV data.
	if _, err := os.Stat(csvFile); err != nil {
		log.Printf("  no CSV file, skipping data import")
		return nil
	}

	csvData, err := os.ReadFile(csvFile)
	if err != nil {
		return fmt.Errorf("read csv: %w", err)
	}
	reader := csv.NewReader(strings.NewReader(string(csvData)))
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("parse csv: %w", err)
	}
	if len(records) < 2 {
		log.Printf("  CSV empty, no rows to import")
		return nil
	}

	header := records[0]
	// Build column index: CSV header label → index.
	headerIdx := make(map[string]int)
	for i, h := range header {
		headerIdx[h] = i
	}

	rowCount := 0
	for _, record := range records[1:] {
		// Get page path from pid column.
		pagePath := ""
		if pidIdx, ok := headerIdx["pid"]; ok && pidIdx < len(record) {
			pagePath = dokuPathToGowiki(record[pidIdx])
		}

		rowFields := make(map[string]any)
		for _, fm := range fields {
			csvIdx, ok := headerIdx[fm.dwCol.Label]
			if !ok || csvIdx >= len(record) {
				continue
			}
			raw := strings.TrimSpace(record[csvIdx])
			if raw == "" {
				continue
			}

			val := convertCSVValue(raw, fm.gowikiType, fm.dwCol)
			if val != nil {
				rowFields[fm.gowikiName] = val
			}
		}

		row := &database.Row{
			PagePath: pagePath,
			Fields:   rowFields,
		}
		if err := dataStore.InsertRow(ctx, tableName, row); err != nil {
			log.Printf("  row error (page=%s): %v", pagePath, err)
			continue
		}
		rowCount++
	}

	log.Printf("  imported %d rows", rowCount)
	return nil
}

// mapFieldType converts DokuWiki struct class to Gowiki field type.
func mapFieldType(col DWColumn) (gowikiType string, enumValues []string, foreignKey string) {
	switch col.Class {
	case "Text", "LongText":
		return "text", nil, ""
	case "Date":
		return "date", nil, ""
	case "DateTime":
		return "datetime", nil, ""
	case "Decimal":
		return "integer", nil, ""
	case "Dropdown":
		vals := parseCommaSeparated(col.Config.Values)
		if col.IsMulti {
			return "multi_enum", vals, ""
		}
		return "enum", vals, ""
	case "Checkbox":
		if col.IsMulti {
			vals := parseCommaSeparated(col.Config.Values)
			return "multi_enum", vals, ""
		}
		vals := parseCommaSeparated(col.Config.Values)
		if len(vals) > 0 {
			return "enum", vals, ""
		}
		return "boolean", nil, ""
	case "User":
		return "user", nil, ""
	case "Page":
		return "page_link", nil, ""
	case "Color":
		return "color", nil, ""
	case "Status":
		fk := col.Config.Schema
		return "tag", nil, fk
	case "Lookup":
		fk := col.Config.Schema
		return "lookup", nil, fk
	default:
		return "text", nil, ""
	}
}

// convertCSVValue converts a raw CSV string to the appropriate Go value.
func convertCSVValue(raw string, fieldType string, _ DWColumn) any {
	switch fieldType {
	case "text", "color", "user", "page_link":
		if fieldType == "page_link" {
			return dokuPathToGowiki(raw)
		}
		return raw
	case "integer":
		// Strip decimals for integer fields (DokuWiki Decimal with trimzeros).
		raw = strings.TrimRight(raw, "0")
		raw = strings.TrimRight(raw, ".")
		if raw == "" {
			return 0
		}
		var v int
		fmt.Sscanf(raw, "%d", &v)
		return v
	case "date":
		return raw // already YYYY-MM-DD
	case "datetime":
		return raw
	case "boolean":
		lower := strings.ToLower(raw)
		return lower == "1" || lower == "true" || lower == "yes" || lower == "oui"
	case "enum":
		return raw
	case "multi_enum":
		// Multi-values in DokuWiki CSV are comma-separated or JSON arrays.
		if strings.HasPrefix(raw, "[") {
			var vals []string
			if err := json.Unmarshal([]byte(raw), &vals); err == nil {
				return vals
			}
		}
		return parseCommaSeparated(raw)
	case "tag":
		// Status fields in DokuWiki CSV are stored as ["", rowid] JSON.
		// Extract the row ID.
		if strings.HasPrefix(raw, "[") {
			var arr []any
			if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) >= 2 {
				switch v := arr[1].(type) {
				case float64:
					return int(v)
				case string:
					var id int
					fmt.Sscanf(v, "%d", &id)
					return id
				}
			}
		}
		var v int
		fmt.Sscanf(raw, "%d", &v)
		return v
	case "lookup":
		// Same as tag — foreign key reference by row ID.
		if strings.HasPrefix(raw, "[") {
			var arr []any
			if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) >= 2 {
				switch v := arr[1].(type) {
				case float64:
					return int(v)
				case string:
					var id int
					fmt.Sscanf(v, "%d", &id)
					return id
				}
			}
		}
		var v int
		fmt.Sscanf(raw, "%d", &v)
		return v
	default:
		return raw
	}
}

// sanitizeName converts a label to a valid Gowiki field/table name.
var nonAlphaRe = regexp.MustCompile(`[^a-z0-9]+`)

func sanitizeName(label string) string {
	// Transliterate common accented characters.
	label = transliterate(label)
	lower := strings.ToLower(label)
	clean := nonAlphaRe.ReplaceAllString(lower, "_")
	clean = strings.Trim(clean, "_")
	if clean == "" {
		return ""
	}
	// Must start with a letter.
	if clean[0] >= '0' && clean[0] <= '9' {
		clean = "f_" + clean
	}
	return clean
}

// transliterate replaces common accented characters with ASCII equivalents.
func transliterate(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == 'é' || r == 'è' || r == 'ê' || r == 'ë' || r == 'É' || r == 'È' || r == 'Ê' || r == 'Ë':
			b.WriteByte('e')
		case r == 'à' || r == 'â' || r == 'ä' || r == 'À' || r == 'Â' || r == 'Ä':
			b.WriteByte('a')
		case r == 'ô' || r == 'ö' || r == 'Ô' || r == 'Ö':
			b.WriteByte('o')
		case r == 'ù' || r == 'û' || r == 'ü' || r == 'Ù' || r == 'Û' || r == 'Ü':
			b.WriteByte('u')
		case r == 'î' || r == 'ï' || r == 'Î' || r == 'Ï':
			b.WriteByte('i')
		case r == 'ç' || r == 'Ç':
			b.WriteByte('c')
		case r == 'ñ' || r == 'Ñ':
			b.WriteByte('n')
		case r < 128:
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Namespace renaming: DokuWiki ps0X → new Gowiki names.
var namespaceRenames = map[string]string{
	"ps01": "dir",
	"ps02": "qara",
	"ps03": "soft",
	"ps04": "cpm",
	"ps05": "soft",
	"ps06": "res",
	"ps07": "res",
}

// dokuPathToGowiki converts a DokuWiki page path to Gowiki format.
func dokuPathToGowiki(dokuPath string) string {
	if dokuPath == "" {
		return ""
	}
	p := strings.ReplaceAll(dokuPath, ":", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// DokuWiki "start" → Gowiki "index"
	if strings.HasSuffix(p, "/start") {
		p = p[:len(p)-5] + "index"
	}
	// Apply namespace renames (e.g., /regulatory/smq/ps02/... → /regulatory/smq/qara/...)
	for old, renamed := range namespaceRenames {
		oldSeg := "/" + old + "/"
		newSeg := "/" + renamed + "/"
		p = strings.Replace(p, oldSeg, newSeg, 1)
	}
	return p
}

// parseCommaSeparated splits a comma-separated string, trimming spaces.
func parseCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

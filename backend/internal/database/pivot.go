package database

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ── Types ─────────────────────────────────────────────────────────────────

// PivotParams captures the pivot-mode inputs. Unset ColsMax defaults to 40.
// Agg defaults to "single".
type PivotParams struct {
	Rows     string
	Cols     string
	Cell     string
	Agg      string
	Empty    string
	ColsSort string
	ColsMax  int
}

// PivotAxisValue is one distinct value on the row or column axis, with the
// label the UI should show and a page_path when the value resolves to a
// page-bound row.
type PivotAxisValue struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	PagePath string `json:"page_path,omitempty"`
}

// PivotCell is one cell of the pivot matrix. The exact fields populated
// depend on Agg: single/first/last populate Value; list populates Values;
// count populates Count; a collision under Agg=single populates Values AND
// Collision=true.
type PivotCell struct {
	Value     string   `json:"value,omitempty"`
	Values    []string `json:"values,omitempty"`
	Collision bool     `json:"collision,omitempty"`
	PagePath  string   `json:"page_path,omitempty"`
	Count     int      `json:"count,omitempty"`
	Empty     bool     `json:"empty,omitempty"`
}

// PivotResult is the response shape for a pivot query.
type PivotResult struct {
	Rows          []PivotAxisValue `json:"rows"`
	Cols          []PivotAxisValue `json:"cols"`
	Cells         [][]PivotCell    `json:"cells"`
	RowField      string           `json:"row_field"`
	ColField      string           `json:"col_field"`
	CellField     string           `json:"cell_field,omitempty"`
	RowFieldType  string           `json:"row_field_type,omitempty"`
	ColFieldType  string           `json:"col_field_type,omitempty"`
	CellFieldType string           `json:"cell_field_type,omitempty"`
	Agg           string           `json:"agg"`
	Empty         string           `json:"empty,omitempty"`
	TotalRows     int              `json:"total_rows"`
}

// ── Errors ────────────────────────────────────────────────────────────────

// PivotError describes a validation failure in pivot mode. The HTTP + MCP
// layers turn this into a 400/refusal with the caller-facing Message and
// Field, so the agent can act on the exact problem instead of guessing.
type PivotError struct {
	Kind    string // "unknown_field", "too_many_columns", "invalid_agg"
	Field   string
	Message string
}

func (e *PivotError) Error() string { return e.Message }

const defaultPivotColsMax = 40

// ── Entry point ───────────────────────────────────────────────────────────

// PivotRows runs the standard filter/sort pipeline, groups the survivors by
// (Rows, Cols), and returns a matrix. Any misconfiguration is loud: unknown
// fields refuse with PivotError, too many distinct columns refuse with
// PivotError before any expensive resolution work.
func (ds *DataStore) PivotRows(ctx context.Context, tableName string, query QueryParams, pivot PivotParams) (*PivotResult, error) {
	table, err := ds.schemaStore.GetTableByName(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("get table schema: %w", err)
	}
	active := activeFieldMap(table.Fields)
	labelToName := make(map[string]string)
	for _, fd := range table.Fields {
		if fd.ArchivedAt == nil && fd.Label != "" {
			labelToName[strings.ToLower(fd.Label)] = fd.Name
		}
	}
	resolve := func(nameOrLabel string) (string, FieldDef, bool) {
		trimmed := strings.TrimSpace(nameOrLabel)
		if trimmed == "" {
			return "", FieldDef{}, false
		}
		if fd, ok := active[trimmed]; ok {
			return trimmed, fd, true
		}
		if name, found := labelToName[strings.ToLower(trimmed)]; found {
			return name, active[name], true
		}
		return "", FieldDef{}, false
	}

	rowName, rowFD, rok := resolve(pivot.Rows)
	if !rok {
		return nil, &PivotError{
			Kind:    "unknown_field",
			Field:   pivot.Rows,
			Message: fmt.Sprintf("pivot: unknown row field %q on table %q", pivot.Rows, tableName),
		}
	}
	colName, colFD, cok := resolve(pivot.Cols)
	if !cok {
		return nil, &PivotError{
			Kind:    "unknown_field",
			Field:   pivot.Cols,
			Message: fmt.Sprintf("pivot: unknown column field %q on table %q", pivot.Cols, tableName),
		}
	}

	agg := strings.ToLower(strings.TrimSpace(pivot.Agg))
	if agg == "" {
		agg = "single"
	}
	switch agg {
	case "single", "list", "count", "first", "last":
	default:
		return nil, &PivotError{
			Kind:    "invalid_agg",
			Field:   pivot.Agg,
			Message: fmt.Sprintf("pivot: invalid agg %q (want single|list|count|first|last)", pivot.Agg),
		}
	}

	var cellName string
	var cellFD FieldDef
	if agg != "count" {
		var ok bool
		cellName, cellFD, ok = resolve(pivot.Cell)
		if !ok {
			return nil, &PivotError{
				Kind:    "unknown_field",
				Field:   pivot.Cell,
				Message: fmt.Sprintf("pivot: unknown cell field %q on table %q", pivot.Cell, tableName),
			}
		}
	}

	// Run the standard row query. Pivot mode ignores caller-supplied Limit
	// (the row axis is the "page size"); Sort/Order are applied on the row
	// axis after grouping.
	q := query
	q.Limit = 0
	q.Offset = 0
	q.Sort = ""
	q.Order = ""

	rows, total, err := ds.QueryRows(ctx, tableName, q)
	if err != nil {
		return nil, fmt.Errorf("pivot: underlying query: %w", err)
	}

	// Group.
	type cellAccum struct {
		values   []string
		pagePath string // only meaningful for single/first/last on page-bound cell types
		count    int
	}
	rowKeys := make([]string, 0)
	rowSeen := make(map[string]struct{})
	colKeys := make([]string, 0)
	colSeen := make(map[string]struct{})
	cellsMap := make(map[string]map[string]*cellAccum)

	for _, r := range rows {
		rowKey := stringifyFieldValue(r.Fields[rowName], rowFD.Type)
		colKey := stringifyFieldValue(r.Fields[colName], colFD.Type)
		if rowKey == "" || colKey == "" {
			// Rows missing an axis value silently drop — a matrix with a
			// blank axis line reads as noise; the caller's filter should
			// exclude them.
			continue
		}
		if _, seen := rowSeen[rowKey]; !seen {
			rowSeen[rowKey] = struct{}{}
			rowKeys = append(rowKeys, rowKey)
		}
		if _, seen := colSeen[colKey]; !seen {
			colSeen[colKey] = struct{}{}
			colKeys = append(colKeys, colKey)
		}

		inner, ok := cellsMap[rowKey]
		if !ok {
			inner = make(map[string]*cellAccum)
			cellsMap[rowKey] = inner
		}
		acc, ok := inner[colKey]
		if !ok {
			acc = &cellAccum{}
			inner[colKey] = acc
		}
		acc.count++
		if agg != "count" {
			v := stringifyFieldValue(r.Fields[cellName], cellFD.Type)
			acc.values = append(acc.values, v)
		}
	}

	// Column-count guard BEFORE resolving axis labels (cheaper).
	colsMax := pivot.ColsMax
	if colsMax <= 0 {
		colsMax = defaultPivotColsMax
	}
	if len(colKeys) > colsMax {
		return nil, &PivotError{
			Kind:    "too_many_columns",
			Message: fmt.Sprintf("pivot: %d distinct column values exceeds pivot_cols_max=%d — filter first or raise pivot_cols_max", len(colKeys), colsMax),
		}
	}

	// Resolve row axis labels + page_paths.
	rowValues, err := ds.resolveAxisValues(ctx, rowFD, rowKeys)
	if err != nil {
		return nil, fmt.Errorf("pivot: resolve row axis: %w", err)
	}
	colValues, err := ds.resolveAxisValues(ctx, colFD, colKeys)
	if err != nil {
		return nil, fmt.Errorf("pivot: resolve column axis: %w", err)
	}

	// Sort row axis: caller-provided Sort takes priority; otherwise the
	// underlying table's own default; otherwise the axis label alphabetical.
	sortAxis(rowValues, rowFD.Type, query.Sort, rowName, query.Order)
	// Column axis order: pivot_cols_sort overrides; otherwise target table's
	// default sort when lookup/tag; otherwise alphabetical by label.
	colSort := pivot.ColsSort
	if colSort == "" && (colFD.Type == FieldTypeLookup || colFD.Type == FieldTypeTag) && colFD.ForeignKey != "" {
		if target, err := ds.schemaStore.GetTableByName(ctx, colFD.ForeignKey); err == nil {
			colSort = target.DefaultSortField
		}
	}
	sortAxis(colValues, colFD.Type, colSort, colName, "")

	// Resolve cell page_paths for page-bound lookup/tag cells (single/first/
	// last). For simplicity we resolve after grouping — the values slice for
	// each accumulator holds ids, resolve once per unique id via a batch.
	cellPathMap := map[string]string{}
	if agg != "count" && (cellFD.Type == FieldTypeLookup || cellFD.Type == FieldTypeTag) && cellFD.ForeignKey != "" {
		uniq := map[string]struct{}{}
		for _, m := range cellsMap {
			for _, acc := range m {
				for _, v := range acc.values {
					if v != "" {
						uniq[v] = struct{}{}
					}
				}
			}
		}
		ids := make([]string, 0, len(uniq))
		for k := range uniq {
			ids = append(ids, k)
		}
		labels, paths, err := ds.batchResolveLookupTargets(ctx, cellFD.ForeignKey, cellFD.DisplayColumn, ids)
		if err != nil {
			return nil, fmt.Errorf("pivot: resolve cell targets: %w", err)
		}
		// Replace raw ids with display labels so the frontend renders the
		// human-readable value directly, mirroring the %field% convention.
		for _, m := range cellsMap {
			for _, acc := range m {
				for i, v := range acc.values {
					if lbl, ok := labels[v]; ok && lbl != "" {
						acc.values[i] = lbl
					}
					if p, ok := paths[v]; ok && p != "" && acc.pagePath == "" {
						acc.pagePath = p
					}
				}
			}
		}
		for k, v := range paths {
			cellPathMap[k] = v
		}
	}
	_ = cellPathMap

	// Materialize the matrix in axis order.
	matrix := make([][]PivotCell, len(rowValues))
	for i, rv := range rowValues {
		line := make([]PivotCell, len(colValues))
		inner := cellsMap[rv.Key]
		for j, cv := range colValues {
			acc, ok := inner[cv.Key]
			if !ok {
				line[j] = PivotCell{Empty: true}
				continue
			}
			cell := PivotCell{}
			switch agg {
			case "count":
				cell.Count = acc.count
				cell.Value = strconv.Itoa(acc.count)
			case "list":
				cell.Values = dedupPreserveOrder(acc.values)
				if len(cell.Values) == 1 {
					cell.Value = cell.Values[0]
					cell.Values = nil
				}
				cell.PagePath = acc.pagePath
			case "first":
				if len(acc.values) > 0 {
					cell.Value = acc.values[0]
				}
				cell.PagePath = acc.pagePath
			case "last":
				if len(acc.values) > 0 {
					cell.Value = acc.values[len(acc.values)-1]
				}
				cell.PagePath = acc.pagePath
			default: // single
				dedup := dedupPreserveOrder(acc.values)
				if len(dedup) == 1 {
					cell.Value = dedup[0]
				} else if len(dedup) > 1 {
					cell.Values = dedup
					cell.Collision = true
				}
				cell.PagePath = acc.pagePath
			}
			line[j] = cell
		}
		matrix[i] = line
	}

	res := &PivotResult{
		Rows:         rowValues,
		Cols:         colValues,
		Cells:        matrix,
		RowField:     rowName,
		ColField:     colName,
		CellField:    cellName,
		RowFieldType: rowFD.Type,
		ColFieldType: colFD.Type,
		CellFieldType: func() string {
			if agg == "count" {
				return ""
			}
			return cellFD.Type
		}(),
		Agg:       agg,
		Empty:     pivot.Empty,
		TotalRows: total,
	}
	return res, nil
}

// stringifyFieldValue canonicalizes a field value into the string form used
// as an axis key or a cell display. The result mirrors what the row-block
// renderer would write inline in Markdown, so the pivot backend stays
// consistent with what a reader sees elsewhere.
func stringifyFieldValue(v any, fieldType string) string {
	if v == nil {
		return ""
	}
	switch fieldType {
	case FieldTypeDate:
		if t, ok := v.(time.Time); ok {
			return t.Format("2006-01-02")
		}
		s := fmt.Sprintf("%v", v)
		if len(s) >= 10 {
			return s[:10]
		}
		return s
	case FieldTypeDatetime:
		if t, ok := v.(time.Time); ok {
			return t.Format("2006-01-02T15:04:05Z")
		}
		return fmt.Sprintf("%v", v)
	case FieldTypeMultiEnum:
		switch arr := v.(type) {
		case []string:
			return strings.Join(arr, ", ")
		case []any:
			parts := make([]string, 0, len(arr))
			for _, item := range arr {
				parts = append(parts, fmt.Sprintf("%v", item))
			}
			return strings.Join(parts, ", ")
		}
		return fmt.Sprintf("%v", v)
	}
	return fmt.Sprintf("%v", v)
}

// resolveAxisValues turns a slice of axis keys into PivotAxisValue entries
// with display labels and (for page-bound lookup/tag targets) page paths.
// For non-lookup axes the key is used as both key and label.
func (ds *DataStore) resolveAxisValues(ctx context.Context, fd FieldDef, keys []string) ([]PivotAxisValue, error) {
	out := make([]PivotAxisValue, 0, len(keys))
	if fd.Type == FieldTypeLookup || fd.Type == FieldTypeTag {
		if fd.ForeignKey == "" {
			// No FK — fall through to raw keys (defensive; the schema should
			// have caught this at field creation).
			for _, k := range keys {
				out = append(out, PivotAxisValue{Key: k, Label: k})
			}
			return out, nil
		}
		labels, paths, err := ds.batchResolveLookupTargets(ctx, fd.ForeignKey, fd.DisplayColumn, keys)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			label := labels[k]
			if label == "" {
				label = k
			}
			out = append(out, PivotAxisValue{
				Key:      k,
				Label:    label,
				PagePath: paths[k],
			})
		}
		return out, nil
	}
	for _, k := range keys {
		out = append(out, PivotAxisValue{Key: k, Label: k})
	}
	return out, nil
}

// batchResolveLookupTargets fetches (id → displayLabel, id → page_path) for
// the given id strings, from the referenced table. Returns two maps keyed
// by the original string ids (leaves malformed ids out).
func (ds *DataStore) batchResolveLookupTargets(ctx context.Context, targetTable, displayColumn string, ids []string) (map[string]string, map[string]string, error) {
	labels := make(map[string]string)
	paths := make(map[string]string)
	if len(ids) == 0 {
		return labels, paths, nil
	}
	target, err := ds.schemaStore.GetTableByName(ctx, targetTable)
	if err != nil {
		return labels, paths, nil // silently degrade — axis stays keyed by id
	}

	p := ds.pool.GetPool()
	if p == nil {
		return labels, paths, fmt.Errorf("database not connected")
	}

	// Only lookup + one active field (displayColumn) — falls back to
	// page_path if no valid displayColumn.
	dispCol := ""
	if displayColumn != "" {
		for _, fd := range target.Fields {
			if fd.ArchivedAt == nil && fd.Name == displayColumn {
				dispCol = displayColumn
				break
			}
		}
	}
	// Coerce ids to int64s for the ANY($1) predicate.
	intIDs := make([]int64, 0, len(ids))
	for _, s := range ids {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			intIDs = append(intIDs, n)
		}
	}
	if len(intIDs) == 0 {
		return labels, paths, nil
	}

	dt := dataTableName(targetTable)
	selectCols := "id, page_path"
	if dispCol != "" {
		selectCols += ", " + quoteIdent(dispCol)
	}
	sql := fmt.Sprintf("SELECT %s FROM %s WHERE id = ANY($1)", selectCols, quoteIdent(dt))
	rows, err := p.Query(ctx, sql, intIDs)
	if err != nil {
		return labels, paths, fmt.Errorf("lookup axis targets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var pagePath string
		if dispCol != "" {
			var label any
			if err := rows.Scan(&id, &pagePath, &label); err != nil {
				return labels, paths, err
			}
			key := strconv.FormatInt(id, 10)
			if label != nil {
				labels[key] = fmt.Sprintf("%v", label)
			}
			paths[key] = pagePath
		} else {
			if err := rows.Scan(&id, &pagePath); err != nil {
				return labels, paths, err
			}
			key := strconv.FormatInt(id, 10)
			paths[key] = pagePath
			// Fall back to page_path or id as the label.
			if pagePath != "" {
				labels[key] = pagePath
			} else {
				labels[key] = key
			}
		}
	}
	return labels, paths, rows.Err()
}

// sortAxis sorts an axis slice in place. Preference order: an explicit sort
// field name (if it matches the field itself or the axis's own field name,
// we sort by label; otherwise leave discovery order), then alphabetical.
// The order argument only matters for the row axis; the column axis is
// always ascending.
func sortAxis(values []PivotAxisValue, fieldType, sortField, axisName, order string) {
	desc := strings.EqualFold(order, "desc")
	// If the sortField matches the axis field itself, sort by label; that's
	// the common case (`sort=release order=desc` on a pivot with
	// pivot_rows=release).
	if sortField == "" || strings.EqualFold(sortField, axisName) {
		sort.SliceStable(values, func(i, j int) bool {
			return compareAxisValues(values[i], values[j], fieldType, desc)
		})
		return
	}
	// Otherwise leave discovery order — the caller passed a sort field that
	// doesn't map onto this axis, so quietly ignore it rather than surprising
	// them with a fake result. A future extension could re-sort by joining
	// on the target table's field.
}

func compareAxisValues(a, b PivotAxisValue, fieldType string, desc bool) bool {
	// For numeric types compare the raw key as a number to avoid "10" < "2".
	switch fieldType {
	case FieldTypeInteger, FieldTypeFloat, FieldTypeAutoIncrement,
		FieldTypeLookup, FieldTypeTag:
		na, aErr := strconv.ParseFloat(a.Key, 64)
		nb, bErr := strconv.ParseFloat(b.Key, 64)
		if aErr == nil && bErr == nil {
			// For lookup/tag axes the key is the target id, but the reader
			// cares about the label. Prefer label ordering; fall back to id
			// order only when labels tie.
			if fieldType == FieldTypeLookup || fieldType == FieldTypeTag {
				la, lb := strings.ToLower(a.Label), strings.ToLower(b.Label)
				if la != lb {
					if desc {
						return la > lb
					}
					return la < lb
				}
			}
			if desc {
				return na > nb
			}
			return na < nb
		}
	}
	la, lb := strings.ToLower(a.Label), strings.ToLower(b.Label)
	if desc {
		return la > lb
	}
	return la < lb
}

func dedupPreserveOrder(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

package database

import "time"

// FieldType enumerates supported column types.
const (
	FieldTypeText          = "text"
	FieldTypeInteger       = "integer"
	FieldTypeFloat         = "float"
	FieldTypeBoolean       = "boolean"
	FieldTypeDate          = "date"
	FieldTypeDatetime      = "datetime"
	FieldTypePageLink      = "page_link"
	FieldTypeEnum          = "enum"
	FieldTypeMultiEnum     = "multi_enum"
	FieldTypeAutoIncrement = "auto_increment"
	FieldTypeImage         = "image"
	FieldTypeColor         = "color"
	FieldTypeTag           = "tag"
)

// ValidFieldTypes is the set of valid field type strings.
var ValidFieldTypes = map[string]bool{
	FieldTypeText:          true,
	FieldTypeInteger:       true,
	FieldTypeFloat:         true,
	FieldTypeBoolean:       true,
	FieldTypeDate:          true,
	FieldTypeDatetime:      true,
	FieldTypePageLink:      true,
	FieldTypeEnum:          true,
	FieldTypeMultiEnum:     true,
	FieldTypeAutoIncrement: true,
	FieldTypeImage:         true,
	FieldTypeColor:         true,
	FieldTypeTag:           true,
}

// TableDef represents a structured data table definition.
type TableDef struct {
	ID                int       `json:"id"`
	Name              string    `json:"name"`
	Label             string    `json:"label"`
	ScopeRegexp       string    `json:"scope_regexp"`
	PageFolder        string    `json:"page_folder"`
	IndexField        string    `json:"index_field"`
	DefaultSortField  string    `json:"default_sort_field"`
	DefaultSortOrder  string    `json:"default_sort_order"`
	PageTemplatePath  string    `json:"page_template_path"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Fields            []FieldDef `json:"fields,omitempty"`
}

// FieldDef represents a column definition in a structured data table.
type FieldDef struct {
	ID           int        `json:"id"`
	TableID      int        `json:"table_id"`
	Name         string     `json:"name"`
	Label        string     `json:"label"`
	Type         string     `json:"type"`
	Required     bool       `json:"required"`
	DefaultValue string     `json:"default_value"`
	DisplayOrder int        `json:"display_order"`
	Placeholder  string     `json:"placeholder"`
	ForeignKey   string     `json:"foreign_key"`
	CreatedAt    time.Time  `json:"created_at"`
	ArchivedAt   *time.Time `json:"archived_at,omitempty"`
	EnumValues   []string   `json:"enum_values,omitempty"`
}

// SchemaHistoryEntry records a schema change.
type SchemaHistoryEntry struct {
	ID         int       `json:"id"`
	TableID    int       `json:"table_id"`
	ChangedAt  time.Time `json:"changed_at"`
	ChangedBy  string    `json:"changed_by"`
	ChangeType string    `json:"change_type"`
	FieldName  string    `json:"field_name"`
	FieldType  string    `json:"field_type"`
	Detail     string    `json:"detail"`
}

// Row represents a data row in a dynamic table.
type Row struct {
	ID        int            `json:"id"`
	PagePath  string         `json:"page_path"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Fields    map[string]any `json:"fields"`
}

// QueryParams defines filtering, sorting, and pagination for row queries.
type QueryParams struct {
	Filters []Filter
	Sort    string
	Order   string // "asc" or "desc"
	Limit   int
	Offset  int
}

// Filter defines a single field filter.
type Filter struct {
	Field    string
	Operator string // =, !=, <, >, <=, >=, ~ (contains)
	Value    string
}

// SQLTypeForField returns the PostgreSQL column type for a given field type.
func SQLTypeForField(fieldType string) string {
	switch fieldType {
	case FieldTypeText, FieldTypePageLink, FieldTypeEnum, FieldTypeImage, FieldTypeColor:
		return "TEXT"
	case FieldTypeInteger, FieldTypeAutoIncrement, FieldTypeTag:
		return "BIGINT"
	case FieldTypeFloat:
		return "DOUBLE PRECISION"
	case FieldTypeBoolean:
		return "BOOLEAN"
	case FieldTypeDate:
		return "DATE"
	case FieldTypeDatetime:
		return "TIMESTAMPTZ"
	case FieldTypeMultiEnum:
		return "" // handled via junction table
	default:
		return "TEXT"
	}
}

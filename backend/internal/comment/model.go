package comment

import "time"

// Anchor describes the text fragment a comment is attached to.
type Anchor struct {
	Selected string `json:"selected"`
	Before   string `json:"before"`
	After    string `json:"after"`
}

// Comment is a single margin comment on a page.
type Comment struct {
	ID        string    `json:"id"`
	Anchor    Anchor    `json:"anchor"`
	Text      string    `json:"text"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Resolved  bool      `json:"resolved"`
	AI        bool      `json:"ai,omitempty"`
}

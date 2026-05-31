package comment

import (
	"encoding/json"
	"time"
)

// Anchor describes the text fragment a comment is attached to.
//
// Address is an opaque structural anchor over the document model, defined by
// the frontend (see frontend/compiler/anchor.ts). The backend stores it
// verbatim. When present, it lets the frontend resolve comments without
// relying on text-quote search — robust against mermaid drift and async
// rendering. Legacy comments (pre-v0.95) have no Address and fall back to
// {Selected, Before, After} matching.
type Anchor struct {
	Selected string          `json:"selected"`
	Before   string          `json:"before"`
	After    string          `json:"after"`
	Address  json.RawMessage `json:"address,omitempty"`
}

// Comment is a single margin comment on a page.
//
// ParentID is set when this comment is a reply to another. Replies inherit
// the parent's anchor, do not carry their own Resolved flag (resolution is
// per thread), and cannot themselves be replied to (no nested threads).
type Comment struct {
	ID        string    `json:"id"`
	Anchor    Anchor    `json:"anchor"`
	Text      string    `json:"text"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Resolved  bool      `json:"resolved"`
	AI        bool      `json:"ai,omitempty"`
	ParentID  string    `json:"parent_id,omitempty"`
}

package comment

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// PageContentReader provides read access to page markdown for orphan detection.
type PageContentReader interface {
	GetMarkdown(pagePath string) (string, error)
}

// Service implements comment business logic.
type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// List returns all comments for a page.
func (svc *Service) List(pagePath string) ([]Comment, error) {
	comments, err := svc.store.Load(pagePath)
	if err != nil {
		return nil, err
	}
	if comments == nil {
		comments = []Comment{}
	}
	return comments, nil
}

// Create adds a new comment to a page.
func (svc *Service) Create(pagePath string, anchor Anchor, text, author string, ai bool) (Comment, error) {
	if strings.TrimSpace(text) == "" {
		return Comment{}, fmt.Errorf("comment text is required")
	}
	if strings.TrimSpace(anchor.Selected) == "" {
		return Comment{}, fmt.Errorf("anchor selection is required")
	}

	// Truncate anchor.Selected to 200 chars for storage.
	selected := anchor.Selected
	if len(selected) > 200 {
		selected = selected[:200]
	}
	anchor.Selected = selected

	// Truncate context to 40 chars each.
	if len(anchor.Before) > 40 {
		anchor.Before = anchor.Before[:40]
	}
	if len(anchor.After) > 40 {
		anchor.After = anchor.After[:40]
	}

	now := time.Now().UTC()
	id := generateID(anchor.Selected, text, now)

	c := Comment{
		ID:        id,
		Anchor:    anchor,
		Text:      text,
		Author:    author,
		CreatedAt: now,
		UpdatedAt: now,
		AI:        ai,
	}

	comments, err := svc.store.Load(pagePath)
	if err != nil {
		return Comment{}, err
	}
	comments = append(comments, c)
	if err := svc.store.Save(pagePath, comments); err != nil {
		return Comment{}, err
	}
	return c, nil
}

// Update modifies the text of an existing comment.
func (svc *Service) Update(pagePath, commentID, newText, user string, isAdmin bool) error {
	if strings.TrimSpace(newText) == "" {
		return fmt.Errorf("comment text is required")
	}

	comments, err := svc.store.Load(pagePath)
	if err != nil {
		return err
	}
	for i := range comments {
		if comments[i].ID == commentID {
			if comments[i].Author != user && !isAdmin {
				return fmt.Errorf("only the author or an admin can edit this comment")
			}
			comments[i].Text = newText
			comments[i].UpdatedAt = time.Now().UTC()
			return svc.store.Save(pagePath, comments)
		}
	}
	return fmt.Errorf("comment %s not found", commentID)
}

// Resolve toggles the resolved flag on a comment.
func (svc *Service) Resolve(pagePath, commentID, user string) error {
	comments, err := svc.store.Load(pagePath)
	if err != nil {
		return err
	}
	for i := range comments {
		if comments[i].ID == commentID {
			comments[i].Resolved = !comments[i].Resolved
			comments[i].UpdatedAt = time.Now().UTC()
			return svc.store.Save(pagePath, comments)
		}
	}
	return fmt.Errorf("comment %s not found", commentID)
}

// Delete removes a comment.
func (svc *Service) Delete(pagePath, commentID, user string, isAdmin bool) error {
	comments, err := svc.store.Load(pagePath)
	if err != nil {
		return err
	}
	for i := range comments {
		if comments[i].ID == commentID {
			if comments[i].Author != user && !isAdmin {
				return fmt.Errorf("only the author or an admin can delete this comment")
			}
			comments = append(comments[:i], comments[i+1:]...)
			return svc.store.Save(pagePath, comments)
		}
	}
	return fmt.Errorf("comment %s not found", commentID)
}

// ToggleAI flips the AI flag on a comment.
func (svc *Service) ToggleAI(pagePath, commentID string) error {
	comments, err := svc.store.Load(pagePath)
	if err != nil {
		return err
	}
	for i := range comments {
		if comments[i].ID == commentID {
			comments[i].AI = !comments[i].AI
			comments[i].UpdatedAt = time.Now().UTC()
			return svc.store.Save(pagePath, comments)
		}
	}
	return fmt.Errorf("comment %s not found", commentID)
}

// DeleteAIComments removes all comments with AI=true for a page.
func (svc *Service) DeleteAIComments(pagePath string) (int, error) {
	comments, err := svc.store.Load(pagePath)
	if err != nil {
		return 0, err
	}
	kept := comments[:0]
	removed := 0
	for _, c := range comments {
		if c.AI {
			removed++
		} else {
			kept = append(kept, c)
		}
	}
	if removed > 0 {
		if err := svc.store.Save(pagePath, kept); err != nil {
			return 0, err
		}
	}
	return removed, nil
}

func generateID(selected, text string, t time.Time) string {
	h := sha1.Sum([]byte(selected + text + t.Format(time.RFC3339Nano)))
	return "c_" + hex.EncodeToString(h[:4])
}

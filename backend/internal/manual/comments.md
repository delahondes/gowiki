# Comments

Comments allow users to annotate specific parts of a page without modifying the page content.

## 1. Adding a comment

1. **Select text** in the page you want to comment on — the Comment button in the action bar becomes active
2. Click the **Comment** button (or it will show "Comment (select text first)" if no text is selected)
3. A sidebar panel opens with a text field anchored to your selection
4. Type your comment and submit

The selected text is highlighted in yellow to show the anchor.

## 1. Viewing comments

Comments appear in a sidebar panel on the right. Each comment shows:
- The highlighted anchor text
- The comment body
- Author and timestamp

Click a comment in the sidebar to scroll to and highlight its anchor in the page.

## 1. Resolving comments

Comments can be marked as resolved. Resolved comments are hidden by default but can be shown via a toggle.

## 1. Storage

Comments are stored as JSON sidecar files in `data/meta/`, separate from the page content. They do not appear in the page's markdown or version history, and they persist across page edits.

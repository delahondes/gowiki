# Integrated AI Assistant

The integrated AI assistant lets you interact with an AI directly from the wiki editor. No external tool, no API token, no setup — if your admin has enabled it, you'll see an **AI** button in the editor toolbar.

## 1. Opening the panel

- Click the **AI** button at the end of the editor toolbar, or press **Ctrl+L** (Cmd+L on macOS)
- The AI panel opens on the right side of the editor
- Click **Wide** in the panel header to expand it for reviewing longer proposals

## 1. Action mode (direct edit)

Type an instruction in the text field and press **Ctrl+Enter** or click the send button (arrow). The AI modifies the page directly.

Examples:
- "Translate section 3 to English"
- "Add a mermaid diagram showing the approval workflow"
- "Fix the code blocks in this page"
- "Add a table of contents at the top"

After the AI applies changes:
- Modified regions are marked with **AI comments** (blue badges in the comment sidebar)
- The page is saved as a draft — you can undo with Ctrl+Z
- You publish when you're satisfied, like any other edit

## 1. Review mode (structured proposals)

Type what you want reviewed and click the **Review** button. The AI analyzes the page and returns a numbered list of proposals.

Examples:
- "Review the English quality"
- "Check for inconsistent terminology"
- "Suggest improvements to the structure"

Each proposal shows:
- The original text and the proposed replacement, with per-character highlighting
- A brief rationale

For each proposal, you can:
- **Accept** — mark for batch application
- **Reject** — skip this proposal
- **Clarify** — provide feedback; click **Refine clarifications** to get revised proposals

Proposals are **verified** against the actual document. If the AI misquoted the original text, the proposal is greyed out and cannot be accepted — preventing broken edits.

Click **Apply accepted** to apply all accepted proposals at once.

## 1. AI comments

When the AI modifies the page, it creates inline comments marked with a blue **AI** badge. These comments:
- Show what the AI changed and why
- Are visible in the comment sidebar alongside regular comments
- Are **automatically removed when you publish** — they don't end up in the published page

To keep an AI comment after publishing, click **Keep on publish** on the comment — this converts it to a regular comment.

To remove all AI comments at once, click **Clear AI notes** in the panel header.

## 1. Access control

The admin controls who can use the AI assistant:
- The feature must be enabled in Admin > Configuration > Integrated AI Assistant
- The **Allowed groups** setting restricts access to specific groups (empty = all authenticated users)
- The **@ai** ACL subject controls which pages the AI can read — pages without `@ai` access are never sent to the AI provider

## 1. Cost and rate limits

The admin can configure:
- **Hourly and daily request limits** per user
- **Monthly budget cap** across all users
- **Max tokens per request**

When you hit a rate limit, the panel shows a clear message with when you can try again.

## 1. Privacy

When you use the AI assistant, the page content is sent to the configured AI provider (e.g. Anthropic's Claude API). The AI provider's data retention policy applies. Pages that don't have `@ai` ACL access are never sent.

# Collaborative Editing

Gowiki supports real-time collaborative editing. Multiple users can work on the same page simultaneously, seeing each other's changes as they type.

## 1. How it works

1. A first user opens a page for editing — they own the draft and the lock
2. Other users navigating to the same page see a **Join** button (or are prompted when pressing Cmd+E / Ctrl+E)
3. Clicking Join enters the editing session — the joining user sees the draft owner's current content
4. Both users can edit simultaneously — changes propagate in real time
5. Colored block indicators show where other users are working

## 1. Draft ownership

The user who first enters edit mode **owns the draft**. This has specific implications:

| Action | Draft owner | Guest |
| --- | --- | --- |
| Edit content | Yes | Yes |
| Save draft | Yes | No |
| Publish | Yes | No |
| Discard draft | Yes | No |
| Leave session | Yes (cancel/escape) | Yes (escape) |

The guest contributes to the draft owner's work. The draft owner retains full control: they alone decide whether to publish or discard the changes. When the guest leaves, the draft owner's content includes all contributions.

{blockquote class=note}
> A guest cannot save independently. If the guest needs to preserve work, the draft owner should save or publish before the guest leaves.

## 1. Presence indicators

### Banner dots

Small colored circles appear in the top banner showing who is currently on the same page. Each user gets a consistent color based on their username. The first letter of their display name appears inside the dot.

### Block indicators

When co-editing, colored bars appear on the left edge of the block where each remote user's cursor is. The user's name appears as a small label above the bar. This helps you avoid editing the same block as someone else.

- **Visual mode**: the indicator appears as a ProseMirror decoration (colored border + background tint)
- **Raw mode**: the indicator appears as a colored bar alongside the textarea

## 1. Editing modes

Both users can independently choose visual or raw mode. Changes synchronize regardless of which mode each user is in:

| Combination | Synchronization |
| --- | --- |
| Visual + Visual | Real-time, block-level cursor preservation |
| Visual + Raw | Real-time, content syncs through markdown |
| Raw + Raw | Real-time, cursor position preserved |

Switching modes during a session (Cmd+E / Ctrl+E) works normally — the remote indicators update after the switch.

## 1. Limitations

- **Same-block editing**: if two users edit the same paragraph simultaneously, the last keystroke wins. The block indicators help you avoid this — work in different sections of the page.
- **No character-level merge**: unlike Google Docs, individual characters are not tracked across users. The system works at the block level.
- **Guest cannot save**: only the draft owner can save or publish. The guest's contributions are part of the owner's draft.
- **Session persistence**: the collaborative session lives in memory. If both users disconnect, the draft is preserved on disk (for the owner), but the real-time connection must be re-established.

## 1. Best practices

- **Work in different sections** of the page to avoid conflicts
- **Watch the block indicators** — if someone's bar is near your cursor, move to a different area
- **The draft owner should save regularly** (Cmd+S / Ctrl+S) to preserve everyone's work
- **Communicate** — the presence indicators show who is on the page, but brief messages in a chat help coordinate larger edits

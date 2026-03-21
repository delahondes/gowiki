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

## 1. Ownership transfer

Draft ownership transfers automatically when the owner leaves:

### Owner saves to draft and exits

The system detects that the owner stopped editing. The guest is automatically promoted to draft owner and gains save/publish permissions. The former owner sees a notification that their draft was taken over.

### Owner disconnects (browser closed, network loss)

The WebSocket connection detects the disconnection. The guest is automatically promoted, same as above.

### Joining a stale draft

If a user saved a draft and left without another user being present, the draft remains locked. When a new user tries to edit, they are prompted to join the session. If the original owner is not online, the new user is automatically promoted to draft owner within a few seconds.

### What the former owner sees

When ownership transfers, the former owner receives a notification: "Your draft was taken over by [user]. Click Join to continue editing." The former owner's edit button updates to show a Join option. If they try to resume editing, the system verifies their edit token is still valid — if the draft was reclaimed, they enter a fresh session or join the new owner's session.

{blockquote class=note}
> Ownership transfer preserves all content. No edits are lost during the transfer — the new owner receives the exact content the previous owner had.

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
- **Session persistence**: the collaborative session lives in memory. If both users disconnect, the draft is preserved on disk (for the owner), but the real-time connection must be re-established.

## 1. Best practices

- **Work in different sections** of the page to avoid conflicts
- **Watch the block indicators** — if someone's bar is near your cursor, move to a different area
- **Save regularly** (Cmd+S / Ctrl+S) — the draft owner should save frequently to preserve everyone's work
- **Communicate** — the presence indicators show who is on the page, but brief messages in a chat help coordinate larger edits

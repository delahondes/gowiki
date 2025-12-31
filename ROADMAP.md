# Gowiki – Editor / Rendering Roadmap

This document captures the _intended big-picture steps_ for implementing the Gowiki editor, rendering pipeline, and extensibility model. It is not a task list, but a conceptual guide to keep the architecture coherent over time.

## 0. Core principles (keep in mind at all times)

- **Markdown is the storage format** (human-readable, VCS-friendly, editable without JS)
- **EditorIR is the authoritative in-memory model** for editing
- **ProseMirror is a WYSIWYM view/controller**, not the data model
- **Goldmark is the Markdown front-end**, not the logic core
- **Round-trips must be stable** (Markdown ↔ EditorIR ↔ Markdown)
- **Plugins extend grammar and rendering, not the core engine**
- **Single-writer semantics by default** (lock-based editing, not real-time collaboration)


## 1. EditorIR (canonical document model)

Status: **IN PROGRESS**


### Goals

- Define a minimal, opinionated document model
- Independent of Markdown, HTML, ProseMirror
- Expressive enough to support extensions


### Current state

- Block nodes: paragraph, heading, lists, code blocks, block quotes, HR
- Inline nodes: text, emphasis, strong, code, link, image, hard break


### Next steps

- Add comments to EditorIR types (semantic meaning)
- Decide how much normalization EditorIR enforces
- Consider explicit attributes for future extensions
    

## 2. Markdown → EditorIR (Goldmark front-end)

Status: **IN PROGRESS**

  
### Goals

- Parse Markdown into EditorIR reliably
- Avoid lossy or ambiguous conversions
  

### Current state

- Goldmark AST traversal implemented
- Inline breaks handled via ast.Text flags
  

### Next steps

- Add more tests (headings, lists, nesting)
- Decide policy for soft breaks (space vs newline)
- Prepare hooks for Goldmark extensions
    

## 3. EditorIR → Markdown (serializer)

Status: **TODO**

  
### Goals

- Serialize EditorIR back to Markdown
- Produce clean, deterministic Markdown
  

### Notes

- This mirrors prosemirror-markdown/to_markdown.ts
- EditorIR controls formatting decisions
    

### Next steps

- Implement Markdown serializer
- Add round-trip tests:
    

```
md → IR → md
```

  
## 4. EditorIR ↔ ProseMirror (WYSIWYM editor)

Status: **TODO**

  
### Goals

- Convert EditorIR → ProseMirror document
- Convert ProseMirror transactions → EditorIR
    

### Notes

- ProseMirror state is _ephemeral_
- EditorIR remains authoritative
- Use schema aligned with EditorIR, not Markdown directly
    

### Next steps

- Define ProseMirror schema derived from EditorIR
- Implement IR → PM conversion
- Implement PM → IR update path
    

## 5. Markdown / WYSIWYM switching

Status: **DESIGNED, NOT IMPLEMENTED**

  
### Goals

- Allow switching between:
    - Plain Markdown editor
    - WYSIWYM ProseMirror editor
        
    
### How it works

- Markdown editor edits Markdown text directly
- WYSIWYM editor edits EditorIR
- Switching direction:
    - Markdown → WYSIWYM: parse Markdown → IR → PM
    - WYSIWYM → Markdown: IR → Markdown

### Locking model

- Editing is **lock-based**, similar to DokuWiki
- By default, a whole page is locked when edited
- Conflicts are explicit and human-resolvable
- No real-time collaborative merging is assumed


## 6. Plugin architecture

Status: **DESIGN PHASE**

  
### Goals

- Allow plugins to extend syntax and rendering
- Keep core small and opinionated
    

### Plugin capabilities

A plugin may provide:
- New Markdown grammar (Goldmark extension)
- New EditorIR node(s)
- Markdown ↔ IR conversion rules
- IR ↔ ProseMirror conversion rules
- Rendering rules (HTML, WYSIWYM)
    

### Design constraints

- Plugins must _declare_ their IR extensions
- Core does not special-case plugin logic
- Conflicts resolved at registration time
    

## 6a. Locking and section-level editing

Status: **DESIGN PHASE**

### Goals

- Support simple, predictable concurrency
- Avoid CRDT/OT complexity
- Enable finer-grained editing without real-time collaboration

### Model

- Page-level locks are the default
- Optional **section-level locks** may be added later
- A section corresponds to a heading and its subtree in EditorIR
- Locks apply to semantic blocks, not text ranges

### Notes

- EditorIR structure makes section locking natural
- No merging logic is required
- This aligns with wiki-style workflows and long-term maintainability


## 7. Rendering (read-only HTML)

Status: **TODO**

  
### Goals

- Render EditorIR to HTML for viewing
- Share logic with WYSIWYM rendering when possible
    

### Notes

- Avoid HTML as a primary data model
- HTML is a view, not a source of truth
    

## 8. Testing strategy

Status: **IN PROGRESS**

  
### Types of tests

- Unit tests: Markdown → IR
- Golden tests: expected IR structures
- Round-trip tests: md ↔ IR ↔ md
- Later: IR ↔ ProseMirror
    

## 9. Deferred / future topics

- Comments / annotations (plugin)
- Access control metadata (sure, dokuwiki system is perfect as far as I am concerned)
- Real-time collaborative editing (explicitly out of scope)


## Final note

This roadmap is deliberately conservative.

If something feels complex, it probably belongs _outside_ the core.
If something feels magical, it probably needs to be made explicit.

When in doubt: **EditorIR first, everything else second**.
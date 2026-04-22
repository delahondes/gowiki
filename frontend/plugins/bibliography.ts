import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

// ── Types shared with the backend resolve endpoint ──────────────────────

interface Author {
  family: string
  given?: string
}

interface PublicationEntry {
  identifier_type: "pmid" | "doi"
  identifier: string
  title?: string
  authors?: Author[]
  year?: number
  journal?: string
  volume?: string
  issue?: string
  pages?: string
  url: string
  fetched_at?: string
  source?: string
}

// ── Resolver with in-memory cache + singleflight ────────────────────────

const API_BASE = "/api/plugin/bibliography/v1"

type ResolverState =
  | { status: "resolved"; entry: PublicationEntry }
  | { status: "loading"; promise: Promise<void> }
  | { status: "unresolved"; error: string }
  | { status: "invalid"; error: string }

const resolverCache = new Map<string, ResolverState>()
const resolverSubscribers = new Map<string, Set<() => void>>()

function cacheKey(type: "pmid" | "doi", id: string): string {
  return type + ":" + id
}

function notify(key: string) {
  const subs = resolverSubscribers.get(key)
  if (!subs) return
  for (const cb of subs) {
    try { cb() } catch { /* ignore */ }
  }
}

function subscribe(key: string, cb: () => void): () => void {
  let subs = resolverSubscribers.get(key)
  if (!subs) {
    subs = new Set()
    resolverSubscribers.set(key, subs)
  }
  subs.add(cb)
  return () => { subs?.delete(cb) }
}

async function doFetch(type: "pmid" | "doi", id: string): Promise<void> {
  const key = cacheKey(type, id)
  try {
    const url = `${API_BASE}/resolve?${type}=${encodeURIComponent(id)}`
    const resp = await fetch(url)
    if (resp.status === 404) {
      resolverCache.set(key, { status: "unresolved", error: "not found" })
    } else if (resp.status === 503) {
      resolverCache.set(key, { status: "unresolved", error: "source unreachable — try again later" })
    } else if (resp.status === 400) {
      const body = await resp.json().catch(() => ({}))
      resolverCache.set(key, { status: "invalid", error: body.error || "invalid identifier" })
    } else if (!resp.ok) {
      resolverCache.set(key, { status: "unresolved", error: `error ${resp.status}` })
    } else {
      const entry = (await resp.json()) as PublicationEntry
      resolverCache.set(key, { status: "resolved", entry })
    }
  } catch (err) {
    resolverCache.set(key, { status: "unresolved", error: String(err) })
  } finally {
    notify(key)
  }
}

function getOrFetch(type: "pmid" | "doi", id: string): ResolverState {
  const key = cacheKey(type, id)
  const existing = resolverCache.get(key)
  if (existing) return existing
  const promise = doFetch(type, id)
  const loading: ResolverState = { status: "loading", promise }
  resolverCache.set(key, loading)
  return loading
}

// ── Display helpers ─────────────────────────────────────────────────────

function shortAuthorList(authors: Author[] | undefined): string {
  if (!authors || authors.length === 0) return "Anonymous"
  if (authors.length === 1) return authors[0].family || "Anonymous"
  if (authors.length === 2) {
    return `${authors[0].family} & ${authors[1].family}`
  }
  return `${authors[0].family} et al.`
}

function formatInline(entry: PublicationEntry): string {
  const authors = shortAuthorList(entry.authors)
  const year = entry.year ? String(entry.year) : "n.d."
  return `[${authors}, ${year}]`
}

function formatFullAuthors(authors: Author[] | undefined): string {
  if (!authors || authors.length === 0) return ""
  const parts = authors.slice(0, 6).map(a => {
    const fam = a.family || ""
    const initials = (a.given || "").split(/\s+/).filter(Boolean).map(s => s[0]).join("")
    return initials ? `${fam} ${initials}` : fam
  })
  if (authors.length > 6) parts.push("et al.")
  return parts.join(", ")
}

function formatReferenceLine(entry: PublicationEntry): string {
  const authors = formatFullAuthors(entry.authors)
  const pieces: string[] = []
  if (authors) pieces.push(authors + ".")
  if (entry.title) pieces.push(entry.title + ".")
  const venue: string[] = []
  if (entry.journal) venue.push(entry.journal + ".")
  if (entry.year || entry.volume || entry.issue || entry.pages) {
    let segment = ""
    if (entry.year) segment += String(entry.year)
    if (entry.volume) segment += `;${entry.volume}`
    if (entry.issue) segment += `(${entry.issue})`
    if (entry.pages) segment += `:${entry.pages}`
    venue.push(segment + ".")
  }
  pieces.push(venue.join(" "))
  const id = entry.identifier_type === "pmid" ? `PMID: ${entry.identifier}.` : `DOI: ${entry.identifier}.`
  pieces.push(id)
  return pieces.filter(Boolean).join(" ")
}

function popupContent(entry: PublicationEntry): HTMLElement {
  const box = document.createElement("div")
  box.className = "gowiki-cite-popup"

  if (entry.title) {
    const t = document.createElement("div")
    t.className = "gowiki-cite-popup-title"
    t.textContent = entry.title
    box.appendChild(t)
  }
  const meta = document.createElement("div")
  meta.className = "gowiki-cite-popup-meta"
  const authors = formatFullAuthors(entry.authors)
  const year = entry.year ? ` (${entry.year})` : ""
  meta.textContent = authors + year
  box.appendChild(meta)
  if (entry.journal) {
    const j = document.createElement("div")
    j.className = "gowiki-cite-popup-journal"
    j.textContent = entry.journal
    box.appendChild(j)
  }
  return box
}

// ── Inline `publication` NodeView ───────────────────────────────────────

class PublicationNodeView {
  dom: HTMLElement
  private node: PMNode
  private unsubscribe: (() => void) | null = null

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("span")
    this.dom.className = "gowiki-cite"
    this.dom.contentEditable = "false"
    this.render()
    this.subscribe()
  }

  private idType(): "pmid" | "doi" | null {
    if ((this.node.attrs.pmid || "").trim()) return "pmid"
    if ((this.node.attrs.doi || "").trim()) return "doi"
    return null
  }

  private idValue(): string {
    const t = this.idType()
    if (!t) return ""
    return String(this.node.attrs[t] || "").trim()
  }

  private subscribe() {
    const t = this.idType()
    if (!t) return
    const key = cacheKey(t, this.idValue())
    this.unsubscribe = subscribe(key, () => this.render())
  }

  private render() {
    this.dom.innerHTML = ""
    const type = this.idType()
    if (!type) {
      const span = document.createElement("span")
      span.className = "gowiki-cite-error"
      span.textContent = "[citation: missing identifier]"
      this.dom.appendChild(span)
      return
    }
    const id = this.idValue()
    const state = getOrFetch(type, id)

    if (state.status === "loading") {
      const span = document.createElement("span")
      span.className = "gowiki-cite-loading"
      span.textContent = "[loading]"
      this.dom.appendChild(span)
      return
    }
    if (state.status === "invalid") {
      const span = document.createElement("span")
      span.className = "gowiki-cite-error"
      span.textContent = `[citation: ${state.error}]`
      this.dom.appendChild(span)
      return
    }
    if (state.status === "unresolved") {
      const span = document.createElement("span")
      span.className = "gowiki-cite-warn"
      span.textContent = `[${type}:${id} ⚠]`
      span.title = state.error
      this.dom.appendChild(span)
      return
    }

    const entry = state.entry
    const a = document.createElement("a")
    a.className = "gowiki-cite-link"
    a.href = entry.url
    a.target = "_blank"
    a.rel = "noopener noreferrer"
    a.textContent = formatInline(entry)
    this.dom.appendChild(a)

    // Hover popup (same pattern as the footnote popup).
    const popup = popupContent(entry)
    popup.classList.add("gowiki-cite-popup-hidden")
    this.dom.appendChild(popup)
    a.addEventListener("mouseenter", () => { popup.classList.remove("gowiki-cite-popup-hidden") })
    a.addEventListener("mouseleave", () => { popup.classList.add("gowiki-cite-popup-hidden") })
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    const changed =
      node.attrs.pmid !== this.node.attrs.pmid ||
      node.attrs.doi !== this.node.attrs.doi
    this.node = node
    if (changed) {
      if (this.unsubscribe) { this.unsubscribe(); this.unsubscribe = null }
      this.render()
      this.subscribe()
    }
    return true
  }

  stopEvent(event: Event): boolean {
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click") return false
    return true
  }

  ignoreMutation(): boolean { return true }

  destroy() {
    if (this.unsubscribe) this.unsubscribe()
  }
}

// ── Block `references` NodeView ─────────────────────────────────────────

class ReferencesNodeView {
  dom: HTMLElement
  private node: PMNode
  private view: EditorView
  private disposers: (() => void)[] = []

  constructor(node: PMNode, view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-references"
    this.dom.contentEditable = "false"
    this.render()
  }

  private collectPublications(): { type: "pmid" | "doi"; id: string }[] {
    const seen = new Set<string>()
    const out: { type: "pmid" | "doi"; id: string }[] = []
    this.view.state.doc.descendants((n) => {
      if (n.type.name !== "publication") return
      const pmid = String(n.attrs.pmid || "").trim()
      const doi = String(n.attrs.doi || "").trim()
      const type: "pmid" | "doi" | null = pmid ? "pmid" : doi ? "doi" : null
      if (!type) return
      const id = type === "pmid" ? pmid : doi
      const key = cacheKey(type, id)
      if (seen.has(key)) return
      seen.add(key)
      out.push({ type, id })
    })
    return out
  }

  private render() {
    for (const d of this.disposers) d()
    this.disposers = []
    this.dom.innerHTML = ""

    const title = String(this.node.attrs.title || "References")
    const showHeading = String(this.node.attrs.show_heading || "true") !== "false"

    if (showHeading) {
      const h = document.createElement("h2")
      h.className = "gowiki-references-heading"
      h.textContent = title
      this.dom.appendChild(h)
    }

    const publications = this.collectPublications()
    if (publications.length === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-references-empty"
      empty.textContent = "No citations on this page yet."
      this.dom.appendChild(empty)
      return
    }

    const list = document.createElement("ol")
    list.className = "gowiki-references-list"
    this.dom.appendChild(list)

    const resolvedItems: { entry: PublicationEntry; li: HTMLLIElement }[] = []
    const pendingItems: { type: "pmid" | "doi"; id: string; li: HTMLLIElement }[] = []

    for (const pub of publications) {
      const state = getOrFetch(pub.type, pub.id)
      const li = document.createElement("li")
      li.className = "gowiki-references-item"

      if (state.status === "resolved") {
        resolvedItems.push({ entry: state.entry, li })
      } else {
        pendingItems.push({ type: pub.type, id: pub.id, li })
        if (state.status === "loading") {
          li.textContent = "Loading…"
        } else if (state.status === "invalid") {
          li.textContent = `Invalid ${pub.type}: ${pub.id}`
        } else {
          li.textContent = `Unresolved ${pub.type}: ${pub.id}`
        }
        list.appendChild(li)
        // Only subscribe while still loading — see AutoReferencesController.render.
        if (state.status === "loading") {
          const key = cacheKey(pub.type, pub.id)
          const off = subscribe(key, () => this.render())
          this.disposers.push(off)
        }
      }
    }

    // Sort resolved items alphabetically by first-author family name.
    resolvedItems.sort((a, b) => {
      const af = (a.entry.authors?.[0]?.family || "").toLowerCase()
      const bf = (b.entry.authors?.[0]?.family || "").toLowerCase()
      if (af === bf) return (a.entry.year || 0) - (b.entry.year || 0)
      return af < bf ? -1 : af > bf ? 1 : 0
    })

    for (const { entry, li } of resolvedItems) {
      const a = document.createElement("a")
      a.className = "gowiki-references-link"
      a.href = entry.url
      a.target = "_blank"
      a.rel = "noopener noreferrer"
      a.textContent = formatReferenceLine(entry)
      li.appendChild(a)
      list.appendChild(li)
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.render()
    return true
  }

  stopEvent(event: Event): boolean {
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click") return false
    return true
  }

  ignoreMutation(): boolean { return true }

  destroy() {
    for (const d of this.disposers) d()
    this.disposers = []
  }

  // Re-render whenever the document changes (new or removed publications).
  docChanged() {
    this.render()
  }
}

// ── Auto-append controller ──────────────────────────────────────────────
//
// When a page contains at least one {publication} but no explicit {references}
// directive, we append a sidecar DOM block at the bottom of the editor root
// that renders the same references list. This mirrors the footnote behaviour
// (automatic list at the end) and reuses the same styling as the explicit
// {references} node.

class AutoReferencesController {
  private view: EditorView
  private dom: HTMLDivElement | null = null
  private disposers: (() => void)[] = []

  constructor(view: EditorView) {
    this.view = view
    this.render()
  }

  private hasExplicitReferences(): boolean {
    let found = false
    this.view.state.doc.descendants((n) => {
      if (n.type.name === "references") found = true
      if (found) return false
      return true
    })
    return found
  }

  private collect(): { type: "pmid" | "doi"; id: string }[] {
    const seen = new Set<string>()
    const out: { type: "pmid" | "doi"; id: string }[] = []
    this.view.state.doc.descendants((n) => {
      if (n.type.name !== "publication") return
      const pmid = String(n.attrs.pmid || "").trim()
      const doi = String(n.attrs.doi || "").trim()
      const type: "pmid" | "doi" | null = pmid ? "pmid" : doi ? "doi" : null
      if (!type) return
      const id = type === "pmid" ? pmid : doi
      const key = cacheKey(type, id)
      if (seen.has(key)) return
      seen.add(key)
      out.push({ type, id })
    })
    return out
  }

  update() { this.render() }

  private render() {
    // Clean up subscriptions from the previous render pass.
    for (const d of this.disposers) d()
    this.disposers = []

    const pubs = this.collect()
    const explicit = this.hasExplicitReferences()

    if (pubs.length === 0 || explicit) {
      this.detach()
      return
    }

    this.ensureDom()
    if (!this.dom) return
    this.dom.innerHTML = ""
    const h = document.createElement("h2")
    h.className = "gowiki-references-heading"
    h.textContent = "References"
    this.dom.appendChild(h)

    const list = document.createElement("ol")
    list.className = "gowiki-references-list"
    this.dom.appendChild(list)

    const resolved: { entry: PublicationEntry; li: HTMLLIElement }[] = []

    for (const pub of pubs) {
      const state = getOrFetch(pub.type, pub.id)
      const li = document.createElement("li")
      li.className = "gowiki-references-item"
      if (state.status === "resolved") {
        resolved.push({ entry: state.entry, li })
      } else {
        if (state.status === "loading") li.textContent = "Loading…"
        else if (state.status === "invalid") li.textContent = `Invalid ${pub.type}: ${pub.id}`
        else li.textContent = `Unresolved ${pub.type}: ${pub.id}`
        list.appendChild(li)
        // Only subscribe while the fetch is in flight. Re-subscribing for
        // terminal states (unresolved/invalid) creates an infinite loop:
        // notify() iterates the subscriber Set, render() unsubs+resubs the
        // same key, and JS Set iteration picks up the freshly-added callback.
        if (state.status === "loading") {
          const key = cacheKey(pub.type, pub.id)
          const off = subscribe(key, () => this.render())
          this.disposers.push(off)
        }
      }
    }

    resolved.sort((a, b) => {
      const af = (a.entry.authors?.[0]?.family || "").toLowerCase()
      const bf = (b.entry.authors?.[0]?.family || "").toLowerCase()
      if (af === bf) return (a.entry.year || 0) - (b.entry.year || 0)
      return af < bf ? -1 : af > bf ? 1 : 0
    })

    for (const { entry, li } of resolved) {
      const a = document.createElement("a")
      a.className = "gowiki-references-link"
      a.href = entry.url
      a.target = "_blank"
      a.rel = "noopener noreferrer"
      a.textContent = formatReferenceLine(entry)
      li.appendChild(a)
      list.appendChild(li)
    }
  }

  private ensureDom() {
    if (this.dom) return
    const container = this.view.dom.parentElement
    if (!container) return
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-references gowiki-references-auto"
    this.dom.contentEditable = "false"
    container.appendChild(this.dom)
  }

  private detach() {
    if (!this.dom) return
    if (this.dom.parentElement) this.dom.parentElement.removeChild(this.dom)
    this.dom = null
    for (const d of this.disposers) d()
    this.disposers = []
  }

  destroy() {
    this.detach()
  }
}

// ── Styles ──────────────────────────────────────────────────────────────

const bibliographyStyles = `
.gowiki-cite {
  position: relative;
  display: inline;
}

.gowiki-cite-link {
  color: var(--gw-color-link);
  text-decoration: none;
  white-space: nowrap;
}

.gowiki-cite-link:hover {
  text-decoration: underline;
}

.gowiki-cite-loading {
  color: #999;
  font-style: italic;
}

.gowiki-cite-warn {
  color: var(--gw-color-error);
  border-bottom: 2px dotted var(--gw-color-error);
  cursor: help;
}

.gowiki-cite-error {
  color: var(--gw-color-error);
  background: var(--gw-color-error-bg);
  padding: 1px 4px;
  border-radius: 3px;
  font-size: 13px;
}

.gowiki-cite-popup {
  position: absolute;
  left: 0;
  top: 100%;
  z-index: 500;
  margin-top: 4px;
  min-width: 260px;
  max-width: 420px;
  background: var(--gw-color-bg);
  border: 1px solid var(--gw-color-border);
  border-radius: var(--gw-radius-sm);
  box-shadow: var(--gw-shadow-md);
  padding: 8px 10px;
  color: var(--gw-color-text);
  font-size: 13px;
  line-height: 1.4;
  white-space: normal;
}

.gowiki-cite-popup-hidden {
  display: none;
}

.gowiki-cite-popup-title {
  font-weight: 600;
  margin-bottom: 4px;
  color: var(--gw-color-text);
}

.gowiki-cite-popup-meta {
  color: var(--gw-color-text);
}

.gowiki-cite-popup-journal {
  color: var(--gw-color-muted);
  font-style: italic;
  margin-top: 2px;
}

.gowiki-references {
  margin-top: 32px;
  padding-top: 16px;
  border-top: 1px solid var(--gw-color-border);
}

.gowiki-references-auto {
  /* Rendered outside the editor doc, below the page content. */
  margin-left: 0;
  margin-right: 0;
}

.gowiki-references-heading {
  font-size: 1.2em;
  font-weight: 600;
  margin: 0 0 8px 0;
}

.gowiki-references-list {
  margin: 0;
  padding-left: 24px;
  font-size: 14px;
  line-height: 1.6;
}

.gowiki-references-item {
  margin-bottom: 4px;
  color: var(--gw-color-text-soft);
}

.gowiki-references-link {
  color: inherit;
  text-decoration: none;
}

.gowiki-references-link:hover {
  color: var(--gw-color-link);
  text-decoration: underline;
}

.gowiki-references-empty {
  color: var(--gw-color-subtle);
  font-style: italic;
  font-size: 13px;
}

#app.gowiki-editing .gowiki-cite.ProseMirror-selectednode,
#app.gowiki-editing .gowiki-references.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
`

// ── Properties (shown in the properties panel) ──────────────────────────

const publicationProperties = [
  {
    name: "pmid",
    label: "PubMed ID",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "e.g. 38480887 (leave empty if using DOI)",
  },
  {
    name: "doi",
    label: "DOI",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "e.g. 10.1038/s41586-024-07067-4 (leave empty if using PMID)",
  },
]

const referencesProperties = [
  {
    name: "title",
    label: "Heading",
    default: "References",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? "References"),
  },
  {
    name: "show_heading",
    label: "Show heading",
    default: "true",
    parse: (raw: string) => {
      const v = raw.trim().toLowerCase()
      return v === "false" ? "false" : "true"
    },
    serialize: (value: string | null) => String(value ?? "true"),
    options: [
      { value: "true", label: "Yes" },
      { value: "false", label: "No" },
    ],
  },
]

// ── Plugin registration ─────────────────────────────────────────────────

export const bibliographyPlugin: WikiPlugin = {
  register(reg) {
    // ── Inline publication node ──
    reg.registerSchema({
      nodes: {
        publication: {
          group: "inline",
          inline: true,
          atom: true,
          attrs: {
            pmid: { default: "" },
            doi: { default: "" },
          },
          toDOM(node: PMNode) {
            return [
              "span",
              {
                class: "gowiki-cite",
                "data-pmid": node.attrs.pmid || "",
                "data-doi": node.attrs.doi || "",
              },
              `[citation: ${node.attrs.pmid ? "pmid:" + node.attrs.pmid : node.attrs.doi ? "doi:" + node.attrs.doi : "?"}]`,
            ]
          },
          parseDOM: [
            {
              tag: "span.gowiki-cite",
              getAttrs(dom: HTMLElement) {
                return {
                  pmid: dom.getAttribute("data-pmid") || "",
                  doi: dom.getAttribute("data-doi") || "",
                }
              },
            },
          ],
        },
      },
    })

    reg.registerSelfContainedDirective("publication", {
      tokenType: "publication",
      nodeType: "publication",
      properties: publicationProperties,
      inline: true,
    })

    reg.registerText("publication", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.publication.create({
            pmid: attrs.pmid ?? "",
            doi: attrs.doi ?? "",
          })
        )
      },
    })

    reg.registerPMNode("publication", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.pmid) parts.push(`pmid=${node.attrs.pmid}`)
        if (node.attrs.doi) parts.push(`doi="${node.attrs.doi}"`)
        return parts.length ? `{publication ${parts.join(" ")}}` : `{publication}`
      },
    })

    // ── Block references node ──
    reg.registerSchema({
      nodes: {
        references: {
          group: "block",
          atom: true,
          attrs: {
            title: { default: "References" },
            show_heading: { default: "true" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-references",
                "data-title": node.attrs.title || "References",
                "data-show-heading": node.attrs.show_heading || "true",
              },
              `References list (${node.attrs.title || "References"})`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-references",
              getAttrs(dom: HTMLElement) {
                return {
                  title: dom.getAttribute("data-title") || "References",
                  show_heading: dom.getAttribute("data-show-heading") || "true",
                }
              },
            },
          ],
        },
      },
    })

    reg.registerSelfContainedDirective("references", {
      tokenType: "references",
      nodeType: "references",
      properties: referencesProperties,
    })

    reg.registerText("references", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.references.create({
            title: attrs.title ?? "References",
            show_heading: attrs.show_heading ?? "true",
          })
        )
      },
    })

    reg.registerPMNode("references", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.title && node.attrs.title !== "References") {
          parts.push(`title="${node.attrs.title}"`)
        }
        if (node.attrs.show_heading && node.attrs.show_heading !== "true") {
          parts.push(`show_heading=${node.attrs.show_heading}`)
        }
        return parts.length ? `{references ${parts.join(" ")}}\n\n` : `{references}\n\n`
      },
    })

    // NodeViews + auto-append controller.
    reg.registerEditorPlugin((_schema: Schema) => {
      const controllerBySession = new WeakMap<EditorView, AutoReferencesController>()

      return new PMPlugin({
        key: new PluginKey("gowiki.bibliography"),
        props: {
          nodeViews: {
            publication(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new PublicationNodeView(node, view, getPos)
            },
            references(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new ReferencesNodeView(node, view, getPos)
            },
          },
        },
        view(view: EditorView) {
          const controller = new AutoReferencesController(view)
          controllerBySession.set(view, controller)
          return {
            update(v: EditorView, prevState) {
              if (v.state.doc !== prevState.doc) {
                controller.update()
              }
            },
            destroy() {
              controller.destroy()
              controllerBySession.delete(view)
            },
          }
        },
      })
    })

    // Insert commands.
    reg.registerCommand("publication", "insert", (state, dispatch) => {
      const nodeType = reg.schema.nodes.publication
      if (!nodeType) return false
      if (dispatch) {
        const node = nodeType.create({ pmid: "", doi: "" })
        const { from } = state.selection
        let tr = state.tr.insert(from, node)
        try {
          tr = tr.setSelection(NodeSelection.create(tr.doc, from))
          tr = enablePropertiesPanel(tr)
        } catch { /* keep default */ }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    reg.registerCommand("references", "insert", (state, dispatch) => {
      const nodeType = reg.schema.nodes.references
      if (!nodeType) return false
      if (dispatch) {
        const node = nodeType.create({})
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 200),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => { if (n.type === nodeType) insertedAt = pos }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch { /* keep default */ }
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    reg.registerStyle("bibliography", bibliographyStyles)
  },
}

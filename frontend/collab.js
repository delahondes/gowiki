/**
 * Collaborative editing module.
 *
 * Architecture:
 * - A Yjs Y.Doc contains a Y.Text ("markdown") which is the shared truth.
 * - Visual mode: ProseMirror doc is derived from the Y.Text. Edits in PM
 *   are serialized back to markdown and applied as Y.Text changes.
 * - Raw mode: textarea content is bound to Y.Text directly.
 * - The Go server relays Yjs sync messages between clients via WebSocket.
 *
 * This module exposes a CollabSession that manages the lifecycle.
 */

import * as Y from "yjs"
import { WebsocketProvider } from "y-websocket"

/**
 * @typedef {Object} CollabCallbacks
 * @property {() => string} getMarkdown - Get current markdown from the active editor
 * @property {(markdown: string, source: string) => void} setMarkdown - Apply markdown to the active editor
 * @property {() => string} getMode - Get current edit mode ("visual" or "raw")
 */

export class CollabSession {
  /**
   * @param {string} pagePath
   * @param {string} initialMarkdown
   * @param {CollabCallbacks} callbacks
   * @param {boolean} isGuest - true for collab guests (don't seed the document)
   */
  constructor(pagePath, initialMarkdown, callbacks, isGuest = false) {
    this.pagePath = pagePath
    this.callbacks = callbacks
    this.doc = new Y.Doc()
    this.ytext = this.doc.getText("markdown")
    this.provider = null
    this.connected = false
    this.suppressRemoteUpdate = false
    this.suppressLocalUpdate = false
    this.isGuest = isGuest

    this._initialMarkdown = initialMarkdown
  }

  /**
   * Connect to the collaboration server.
   */
  connect() {
    const proto = window.location.protocol === "https:" ? "wss" : "ws"
    const wsUrl = `${proto}://${window.location.host}/api/ws/collab`

    // y-websocket expects a "room name" which maps to the page path.
    // We use a custom WebSocket URL that includes the page path.
    this.provider = new WebsocketProvider(wsUrl, this.pagePath, this.doc, {
      connect: true,
      // The provider will append the room name to the URL.
      // Our Go relay extracts the page path from the URL.
    })

    this.provider.on("status", (event) => {
      this.connected = event.status === "connected"
    })

    // When we first sync, seed the document if it's empty.
    // Only the lock owner seeds — guests receive the state via sync.
    this.provider.on("sync", (synced) => {
      if (synced && !this.isGuest && this.ytext.length === 0 && this._initialMarkdown) {
        this.doc.transact(() => {
          this.ytext.insert(0, this._initialMarkdown)
        }, this) // origin = this, so we can filter in the observer
      }
    })

    // Listen for remote changes to the shared text.
    this.ytext.observe((event) => {
      if (event.transaction.origin === this) return // our own change
      if (this.suppressRemoteUpdate) return

      const newMarkdown = this.ytext.toString()
      this.suppressLocalUpdate = true
      try {
        this.callbacks.setMarkdown(newMarkdown, "remote")
      } finally {
        this.suppressLocalUpdate = false
      }
    })
  }

  /**
   * Called by the editor when local content changes.
   * Diffs the Y.Text and applies minimal changes.
   */
  localChange(newMarkdown) {
    if (this.suppressLocalUpdate) return
    if (!this.connected) return

    const currentYText = this.ytext.toString()
    if (newMarkdown === currentYText) return

    // Compute a simple diff and apply to Y.Text.
    this.suppressRemoteUpdate = true
    try {
      this.doc.transact(() => {
        applyStringDiff(this.ytext, currentYText, newMarkdown)
      }, this)
    } finally {
      this.suppressRemoteUpdate = false
    }
  }

  /**
   * Disconnect and clean up.
   */
  destroy() {
    if (this.provider) {
      this.provider.disconnect()
      this.provider.destroy()
      this.provider = null
    }
    this.doc.destroy()
  }
}

/**
 * Apply a string diff to a Y.Text by finding the changed region
 * and performing a single delete + insert. This is a simplified diff
 * that works well for typical editing operations.
 */
function applyStringDiff(ytext, oldStr, newStr) {
  // Find common prefix.
  let start = 0
  while (start < oldStr.length && start < newStr.length && oldStr[start] === newStr[start]) {
    start++
  }

  // Find common suffix (but don't overlap with prefix).
  let oldEnd = oldStr.length
  let newEnd = newStr.length
  while (oldEnd > start && newEnd > start && oldStr[oldEnd - 1] === newStr[newEnd - 1]) {
    oldEnd--
    newEnd--
  }

  // Delete the changed region from old, insert the new content.
  const deleteCount = oldEnd - start
  const insertText = newStr.slice(start, newEnd)

  if (deleteCount > 0) {
    ytext.delete(start, deleteCount)
  }
  if (insertText.length > 0) {
    ytext.insert(start, insertText)
  }
}

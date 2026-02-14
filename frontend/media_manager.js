function encodePath(path) {
  return String(path ?? "")
    .split("/")
    .filter(Boolean)
    .map(part => encodeURIComponent(part))
    .join("/")
}

function buildMediaApiPath(path) {
  const encoded = encodePath(path)
  return encoded ? `/api/media/${encoded}` : "/api/media"
}

function normalizeNamespacePath(rawPath) {
  return String(rawPath ?? "")
    .split("/")
    .filter(Boolean)
    .join("/")
}

function parentNamespacePath(namespacePath) {
  const parts = normalizeNamespacePath(namespacePath).split("/").filter(Boolean)
  if (parts.length === 0) return ""
  parts.pop()
  return parts.join("/")
}

function isImageFile(name) {
  return /\.(png|jpe?g|gif|svg)$/i.test(name)
}

async function listMedia(namespacePath) {
  const resp = await fetch(buildMediaApiPath(namespacePath))
  if (!resp.ok) {
    throw new Error(`Failed to list media: ${resp.status}`)
  }
  return await resp.json()
}

async function uploadMedia(namespacePath, file, overwrite) {
  const form = new FormData()
  form.append("file", file)
  form.append("overwrite", overwrite ? "true" : "false")
  const resp = await fetch(buildMediaApiPath(namespacePath), {
    method: "POST",
    body: form,
  })
  if (resp.status === 409) {
    throw new Error("A file with this name already exists.")
  }
  if (!resp.ok) {
    throw new Error(`Failed to upload media: ${resp.status}`)
  }
  return await resp.json()
}

async function deleteMedia(mediaPath) {
  const encoded = encodePath(mediaPath)
  const resp = await fetch(`/api/media/${encoded}`, {
    method: "DELETE",
  })
  if (!resp.ok) {
    throw new Error(`Failed to delete media: ${resp.status}`)
  }
  return await resp.json()
}

function formatMediaSize(bytes) {
  if (!bytes) return ""
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function openMediaManager(initialNamespacePath, onStatus, onInsert) {
  const state = {
    namespacePath: normalizeNamespacePath(initialNamespacePath),
    entries: [],
    showMarkdownFiles: false,
  }

  const overlay = document.createElement("div")
  overlay.className = "gowiki-media-modal-overlay"
  const dialog = document.createElement("div")
  dialog.className = "gowiki-media-modal"

  const header = document.createElement("div")
  header.className = "gowiki-media-modal-header"
  const title = document.createElement("div")
  title.className = "gowiki-media-modal-title"
  title.textContent = "Media manager"
  const closeBtn = document.createElement("button")
  closeBtn.type = "button"
  closeBtn.className = "gowiki-link-modal-btn"
  closeBtn.textContent = "Close"
  header.appendChild(title)
  header.appendChild(closeBtn)

  const nav = document.createElement("div")
  nav.className = "gowiki-media-modal-nav"
  const upBtn = document.createElement("button")
  upBtn.type = "button"
  upBtn.className = "gowiki-link-modal-btn"
  upBtn.textContent = "Up"
  const pathLabel = document.createElement("div")
  pathLabel.className = "gowiki-media-modal-path"
  nav.appendChild(upBtn)
  nav.appendChild(pathLabel)

  const uploadRow = document.createElement("div")
  uploadRow.className = "gowiki-media-modal-upload"
  const fileInput = document.createElement("input")
  fileInput.type = "file"
  const overwriteLabel = document.createElement("label")
  overwriteLabel.className = "gowiki-media-modal-overwrite"
  const overwriteInput = document.createElement("input")
  overwriteInput.type = "checkbox"
  overwriteLabel.appendChild(overwriteInput)
  overwriteLabel.appendChild(document.createTextNode("Overwrite"))
  const uploadBtn = document.createElement("button")
  uploadBtn.type = "button"
  uploadBtn.className = "gowiki-link-modal-btn"
  uploadBtn.textContent = "Upload"
  const showMarkdownLabel = document.createElement("label")
  showMarkdownLabel.className = "gowiki-media-modal-overwrite"
  const showMarkdownInput = document.createElement("input")
  showMarkdownInput.type = "checkbox"
  showMarkdownLabel.appendChild(showMarkdownInput)
  showMarkdownLabel.appendChild(document.createTextNode("Show markdown files"))
  uploadRow.appendChild(fileInput)
  uploadRow.appendChild(overwriteLabel)
  uploadRow.appendChild(showMarkdownLabel)
  uploadRow.appendChild(uploadBtn)

  const list = document.createElement("div")
  list.className = "gowiki-media-modal-list"

  const status = document.createElement("div")
  status.className = "gowiki-link-modal-warning"

  function setModalStatus(text, isError = false) {
    status.style.color = isError ? "#b00020" : "#166534"
    status.textContent = text
    if (typeof onStatus === "function") onStatus(text)
  }

  function close() {
    overlay.remove()
  }

  async function refresh() {
    pathLabel.textContent = `Namespace: /${state.namespacePath || ""}`
    upBtn.disabled = state.namespacePath.length === 0
    list.innerHTML = ""

    try {
      const data = await listMedia(state.namespacePath)
      const allEntries = data.entries ?? []
      state.entries = allEntries.filter(entry => {
        if (entry.kind !== "file") return true
        if (state.showMarkdownFiles) return true
        return !/\.md$/i.test(String(entry.name ?? ""))
      })
    } catch (err) {
      setModalStatus(String(err?.message ?? err), true)
      return
    }

    if (state.entries.length === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-media-modal-empty"
      empty.textContent = "No media in this namespace."
      list.appendChild(empty)
      return
    }

    for (const entry of state.entries) {
      const row = document.createElement("div")
      row.className = "gowiki-media-row"

      const label = document.createElement("div")
      label.className = "gowiki-media-row-label"
      const prefix = entry.kind === "folder" ? "[dir] " : "[file] "
      const size = entry.kind === "file" ? ` (${formatMediaSize(entry.size)})` : ""
      label.textContent = `${prefix}${entry.name}${size}`
      row.appendChild(label)

      if (entry.kind === "folder") {
        const openBtn = document.createElement("button")
        openBtn.type = "button"
        openBtn.className = "gowiki-link-modal-btn"
        openBtn.textContent = "Open"
        openBtn.addEventListener("click", () => {
          state.namespacePath = normalizeNamespacePath(entry.path)
          void refresh()
        })
        row.appendChild(openBtn)
      } else {
        const linkBtn = document.createElement("button")
        linkBtn.type = "button"
        linkBtn.className = "gowiki-link-modal-btn"
        linkBtn.textContent = "Add link"
        linkBtn.addEventListener("click", () => {
          if (typeof onInsert === "function") {
            onInsert("link", entry)
          }
          close()
        })
        row.appendChild(linkBtn)

        if (isImageFile(entry.name)) {
          const imgBtn = document.createElement("button")
          imgBtn.type = "button"
          imgBtn.className = "gowiki-link-modal-btn"
          imgBtn.textContent = "Add image"
          imgBtn.addEventListener("click", () => {
            if (typeof onInsert === "function") {
              onInsert("image", entry)
            }
            close()
          })
          row.appendChild(imgBtn)
        }

        const deleteBtn = document.createElement("button")
        deleteBtn.type = "button"
        deleteBtn.className = "gowiki-link-modal-btn"
        deleteBtn.textContent = "Delete"
        deleteBtn.addEventListener("click", async () => {
          try {
            await deleteMedia(entry.path)
            setModalStatus(`Deleted ${entry.name}`)
            await refresh()
          } catch (err) {
            setModalStatus(String(err?.message ?? err), true)
          }
        })
        row.appendChild(deleteBtn)
      }

      list.appendChild(row)
    }
  }

  closeBtn.addEventListener("click", close)
  overlay.addEventListener("click", event => {
    if (event.target === overlay) close()
  })
  upBtn.addEventListener("click", () => {
    state.namespacePath = parentNamespacePath(state.namespacePath)
    void refresh()
  })
  showMarkdownInput.addEventListener("change", () => {
    state.showMarkdownFiles = showMarkdownInput.checked
    void refresh()
  })

  uploadBtn.addEventListener("click", async () => {
    if (!fileInput.files || fileInput.files.length === 0) {
      setModalStatus("Choose a file to upload.", true)
      return
    }
    try {
      await uploadMedia(state.namespacePath, fileInput.files[0], overwriteInput.checked)
      fileInput.value = ""
      setModalStatus("Upload complete.")
      await refresh()
    } catch (err) {
      setModalStatus(String(err?.message ?? err), true)
    }
  })

  dialog.appendChild(header)
  dialog.appendChild(nav)
  dialog.appendChild(uploadRow)
  dialog.appendChild(list)
  dialog.appendChild(status)
  overlay.appendChild(dialog)
  document.body.appendChild(overlay)
  void refresh()
}

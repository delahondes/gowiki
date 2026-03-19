/**
 * Browser-side key management for X.509 document signing.
 * Uses Web Crypto API for key generation and IndexedDB for storage.
 * The private key never leaves the browser.
 */

const DB_NAME = "gowiki-signing"
const DB_VERSION = 1
const STORE_NAME = "keys"

interface StoredKey {
  username: string
  privateKey: CryptoKey
  publicKeySpki: ArrayBuffer // SPKI-encoded public key for CSR/export
  certificatePEM: string | null
  createdAt: string
}

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME, { keyPath: "username" })
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

function txGet(db: IDBDatabase, username: string): Promise<StoredKey | null> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, "readonly")
    const store = tx.objectStore(STORE_NAME)
    const req = store.get(username)
    req.onsuccess = () => resolve(req.result ?? null)
    req.onerror = () => reject(req.error)
  })
}

function txPut(db: IDBDatabase, entry: StoredKey): Promise<void> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, "readwrite")
    const store = tx.objectStore(STORE_NAME)
    const req = store.put(entry)
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

function txDelete(db: IDBDatabase, username: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(STORE_NAME, "readwrite")
    const store = tx.objectStore(STORE_NAME)
    const req = store.delete(username)
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

/**
 * Generate an ECDSA P-256 keypair and store in IndexedDB.
 * The private key is non-extractable.
 */
export async function generateKeypair(username: string): Promise<void> {
  const keyPair = await crypto.subtle.generateKey(
    { name: "ECDSA", namedCurve: "P-256" },
    false, // non-extractable private key
    ["sign", "verify"]
  )

  const publicKeySpki = await crypto.subtle.exportKey("spki", keyPair.publicKey)

  const db = await openDB()
  await txPut(db, {
    username,
    privateKey: keyPair.privateKey,
    publicKeySpki,
    certificatePEM: null,
    createdAt: new Date().toISOString(),
  })
  db.close()
}

/**
 * Check if the user has a signing key.
 */
export async function hasKey(username: string): Promise<boolean> {
  const db = await openDB()
  const entry = await txGet(db, username)
  db.close()
  return entry !== null
}

/**
 * Get the private key for signing.
 */
export async function getPrivateKey(username: string): Promise<CryptoKey | null> {
  const db = await openDB()
  const entry = await txGet(db, username)
  db.close()
  return entry?.privateKey ?? null
}

/**
 * Get the stored certificate PEM.
 */
export async function getCertificatePEM(username: string): Promise<string | null> {
  const db = await openDB()
  const entry = await txGet(db, username)
  db.close()
  return entry?.certificatePEM ?? null
}

/**
 * Import a signed certificate (PEM) into the key store.
 */
export async function importCertificate(username: string, pem: string): Promise<void> {
  const db = await openDB()
  const entry = await txGet(db, username)
  if (!entry) {
    db.close()
    throw new Error("No keypair found — generate a key first")
  }
  entry.certificatePEM = pem
  await txPut(db, entry)
  db.close()
}

/**
 * Export the public key as base64-encoded SPKI (for CSR or direct upload).
 */
export async function getPublicKeySPKI(username: string): Promise<string | null> {
  const db = await openDB()
  const entry = await txGet(db, username)
  db.close()
  if (!entry) return null
  const bytes = new Uint8Array(entry.publicKeySpki)
  return btoa(String.fromCharCode(...bytes))
}

/**
 * Delete the key and certificate.
 */
export async function deleteKey(username: string): Promise<void> {
  const db = await openDB()
  await txDelete(db, username)
  db.close()
}

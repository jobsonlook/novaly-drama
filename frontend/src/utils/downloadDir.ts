/** Persist a user-picked download folder via File System Access API + IndexedDB. */

const DB_NAME = 'novaly-prefs'
const DB_VERSION = 1
const STORE = 'handles'
const DIR_KEY = 'downloadDir'

type PermissionMode = 'read' | 'readwrite'

function supported(): boolean {
  return typeof window !== 'undefined' && typeof (window as any).showDirectoryPicker === 'function'
}

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onerror = () => reject(req.error || new Error('打开本地偏好失败'))
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE)
    }
    req.onsuccess = () => resolve(req.result)
  })
}

async function idbGet<T>(key: string): Promise<T | null> {
  const db = await openDb()
  try {
    return await new Promise((resolve, reject) => {
      const tx = db.transaction(STORE, 'readonly')
      const req = tx.objectStore(STORE).get(key)
      req.onerror = () => reject(req.error)
      req.onsuccess = () => resolve((req.result as T) ?? null)
    })
  } finally {
    db.close()
  }
}

async function idbSet(key: string, value: unknown): Promise<void> {
  const db = await openDb()
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, 'readwrite')
      tx.oncomplete = () => resolve()
      tx.onerror = () => reject(tx.error)
      tx.objectStore(STORE).put(value, key)
    })
  } finally {
    db.close()
  }
}

async function idbDel(key: string): Promise<void> {
  const db = await openDb()
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE, 'readwrite')
      tx.oncomplete = () => resolve()
      tx.onerror = () => reject(tx.error)
      tx.objectStore(STORE).delete(key)
    })
  } finally {
    db.close()
  }
}

function sanitizeFilename(name: string): string {
  const base = name.replace(/[\\/:*?"<>|]+/g, '_').replace(/\s+/g, ' ').trim()
  return base || 'download'
}

function triggerAnchorDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = sanitizeFilename(filename)
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

async function queryPermission(handle: FileSystemDirectoryHandle, mode: PermissionMode): Promise<PermissionState> {
  const anyHandle = handle as any
  if (typeof anyHandle.queryPermission === 'function') {
    return anyHandle.queryPermission({ mode })
  }
  return 'granted'
}

async function requestPermission(handle: FileSystemDirectoryHandle, mode: PermissionMode): Promise<PermissionState> {
  const anyHandle = handle as any
  if (typeof anyHandle.requestPermission === 'function') {
    return anyHandle.requestPermission({ mode })
  }
  return 'granted'
}

export function isDownloadDirSupported() {
  return supported()
}

export async function getStoredDownloadDirHandle(): Promise<FileSystemDirectoryHandle | null> {
  if (!supported()) return null
  try {
    return await idbGet<FileSystemDirectoryHandle>(DIR_KEY)
  } catch {
    return null
  }
}

export async function getDownloadDirName(): Promise<string | null> {
  const handle = await getStoredDownloadDirHandle()
  return handle?.name || null
}

export async function pickDownloadDirectory(): Promise<string> {
  if (!supported()) {
    throw new Error('当前浏览器不支持选择下载目录，请使用 Chrome / Edge')
  }
  const handle = await (window as any).showDirectoryPicker({ mode: 'readwrite' }) as FileSystemDirectoryHandle
  const perm = await requestPermission(handle, 'readwrite')
  if (perm !== 'granted') {
    throw new Error('需要写入权限才能保存到该目录')
  }
  await idbSet(DIR_KEY, handle)
  return handle.name
}

export async function clearDownloadDirectory(): Promise<void> {
  await idbDel(DIR_KEY)
}

async function ensureWritable(handle: FileSystemDirectoryHandle): Promise<boolean> {
  let perm = await queryPermission(handle, 'readwrite')
  if (perm === 'granted') return true
  if (perm === 'prompt') {
    perm = await requestPermission(handle, 'readwrite')
  }
  return perm === 'granted'
}

/** Return a writable download directory handle, optionally prompting the user to pick one. */
export async function resolveDownloadDirectory(options?: {
  promptIfMissing?: boolean
}): Promise<FileSystemDirectoryHandle | null> {
  if (!supported()) return null
  const existing = await getStoredDownloadDirHandle()
  if (existing && (await ensureWritable(existing))) return existing
  if (!options?.promptIfMissing) return null
  try {
    await pickDownloadDirectory()
    const next = await getStoredDownloadDirHandle()
    if (next && (await ensureWritable(next))) return next
  } catch {
    /* user cancelled or unsupported */
  }
  return null
}

export async function ensureSubdirectory(
  parent: FileSystemDirectoryHandle,
  name: string,
): Promise<FileSystemDirectoryHandle> {
  return parent.getDirectoryHandle(sanitizeFilename(name), { create: true })
}

export async function writeBlobToDirectory(
  dir: FileSystemDirectoryHandle,
  blob: Blob,
  filename: string,
): Promise<void> {
  const safeName = sanitizeFilename(filename)
  const fileHandle = await dir.getFileHandle(safeName, { create: true })
  const writable = await fileHandle.createWritable()
  try {
    await writable.write(blob)
  } finally {
    await writable.close()
  }
}

/** Save to configured folder when possible; otherwise browser default download folder. */
export async function saveBlobDownload(blob: Blob, filename: string): Promise<'dir' | 'default'> {
  const safeName = sanitizeFilename(filename)
  const handle = await getStoredDownloadDirHandle()
  if (!handle) {
    triggerAnchorDownload(blob, safeName)
    return 'default'
  }
  try {
    if (!(await ensureWritable(handle))) {
      triggerAnchorDownload(blob, safeName)
      return 'default'
    }
    await writeBlobToDirectory(handle, blob, safeName)
    return 'dir'
  } catch {
    triggerAnchorDownload(blob, safeName)
    return 'default'
  }
}

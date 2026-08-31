/** Browser → COS direct upload helpers (presigned PUT / multipart). */

export type CosPresign = {
  uploadUrl: string
  headers: Record<string, string>
  key: string
  path?: string
  resourceId?: number
  ext?: string
  filename?: string
}

const MULTIPART_THRESHOLD = 8 * 1024 * 1024 // 8 MiB
const PART_SIZE = 5 * 1024 * 1024 // COS min part size (except last)
const PART_CONCURRENCY = 4

export async function cosEnabled(): Promise<boolean> {
  try {
    const r = await fetch('/api/cos/status')
    if (!r.ok) return false
    const data = await r.json()
    return !!data.enabled
  } catch {
    return false
  }
}

export async function putFileToCos(presign: CosPresign, file: File | Blob) {
  if (file.size >= MULTIPART_THRESHOLD && presign.key) {
    await putFileMultipart(presign, file)
    return
  }
  const headers: Record<string, string> = { ...(presign.headers || {}) }
  const r = await fetch(presign.uploadUrl, {
    method: 'PUT',
    headers,
    body: file,
  })
  if (!r.ok) {
    const text = await r.text().catch(() => '')
    throw new Error(text || `云存储上传失败（HTTP ${r.status}）`)
  }
}

async function putFileMultipart(presign: CosPresign, file: File | Blob) {
  const contentType = (presign.headers && presign.headers['Content-Type']) || 'application/octet-stream'
  const key = presign.key

  const initRes = await fetch('/api/cos/multipart/init', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ key, contentType }),
  })
  if (!initRes.ok) {
    const text = await initRes.text().catch(() => '')
    throw new Error(text || '初始化分片上传失败')
  }
  const { uploadId } = await initRes.json() as { uploadId: string }

  const partCount = Math.ceil(file.size / PART_SIZE)
  const partNumbers = Array.from({ length: partCount }, (_, i) => i + 1)

  let signed: { partNumber: number; uploadUrl: string }[] = []
  try {
    const signRes = await fetch('/api/cos/multipart/sign-parts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key, uploadId, partNumbers }),
    })
    if (!signRes.ok) {
      throw new Error(await signRes.text().catch(() => '签名分片失败'))
    }
    const data = await signRes.json() as { parts: { partNumber: number; uploadUrl: string }[] }
    signed = data.parts || []
  } catch (e) {
    await abortMultipart(key, uploadId)
    throw e
  }

  const urlByPart = new Map(signed.map((p) => [p.partNumber, p.uploadUrl]))
  const completed: { partNumber: number; etag: string }[] = []

  try {
    let next = 0
    const workers = Array.from({ length: Math.min(PART_CONCURRENCY, partCount) }, async () => {
      while (next < partCount) {
        const i = next++
        const partNumber = i + 1
        const start = i * PART_SIZE
        const end = Math.min(start + PART_SIZE, file.size)
        const blob = file.slice(start, end)
        const uploadUrl = urlByPart.get(partNumber)
        if (!uploadUrl) throw new Error(`缺少分片 ${partNumber} 签名`)

        const r = await fetch(uploadUrl, { method: 'PUT', body: blob })
        if (!r.ok) {
          const text = await r.text().catch(() => '')
          throw new Error(text || `分片 ${partNumber} 上传失败（HTTP ${r.status}）`)
        }
        const etag = r.headers.get('ETag') || r.headers.get('etag') || ''
        if (!etag) throw new Error(`分片 ${partNumber} 未返回 ETag`)
        completed.push({ partNumber, etag })
      }
    })
    await Promise.all(workers)

    completed.sort((a, b) => a.partNumber - b.partNumber)
    const completeRes = await fetch('/api/cos/multipart/complete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        key,
        uploadId,
        parts: completed.map((p) => ({ partNumber: p.partNumber, etag: p.etag })),
      }),
    })
    if (!completeRes.ok) {
      throw new Error(await completeRes.text().catch(() => '合并分片失败'))
    }
  } catch (e) {
    await abortMultipart(key, uploadId)
    throw e
  }
}

async function abortMultipart(key: string, uploadId: string) {
  try {
    await fetch('/api/cos/multipart/abort', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key, uploadId }),
    })
  } catch {
    /* ignore */
  }
}

export function fileExt(file: File, fallback = '') {
  const name = file.name || ''
  const i = name.lastIndexOf('.')
  if (i >= 0) return name.slice(i + 1).toLowerCase()
  if (file.type.includes('png')) return 'png'
  if (file.type.includes('webp')) return 'webp'
  if (file.type.includes('gif')) return 'gif'
  if (file.type.includes('jpeg') || file.type.includes('jpg')) return 'jpg'
  if (file.type.includes('webm')) return 'webm'
  if (file.type.includes('quicktime')) return 'mov'
  if (file.type.includes('mp4')) return 'mp4'
  return fallback
}

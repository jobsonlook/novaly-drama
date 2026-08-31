export async function api(path: string, options?: RequestInit) {
  let r: Response
  try { r = await fetch('/api' + path, { headers: { 'Content-Type': 'application/json' }, ...options }) }
  catch { throw new Error('无法连接后端服务，请先在 backend 目录运行 go run .') }
  const raw = await r.text()
  let data: any = {}
  try { data = raw ? JSON.parse(raw) : {} } catch { throw new Error('后端服务不可用，请确认后端已启动') }
  if (!r.ok) throw new Error(data.error || `请求失败（HTTP ${r.status}）`)
  return data
}

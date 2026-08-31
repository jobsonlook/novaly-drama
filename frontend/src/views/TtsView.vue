<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Delete, Download, Headset } from '@element-plus/icons-vue'
import { api } from '@/api/client'
import { saveBlobDownload } from '@/utils/downloadDir'

type Character = {
  id: string
  name: string
  voice_hint?: string
  voice_type: string
  default_speed: number
}

type DialogueLine = {
  id: string
  source_shot_id?: number
  global_shot: number
  time: string
  type: string
  speaker: string
  text: string
  tone?: string
  emotion: string
  emotion_strength: number
  enable_emotion: boolean
  pitch: number
  speech_rate: number
  loudness_rate: number
  speed_ratio?: number
  emotion_hint?: string
  needs_review?: boolean
  filename: string
  voice_type: string
  audioUrl?: string
  audioReady?: boolean
  // legacy fields from older imports
  episode?: number
  shot?: number
}

type ProjectSummary = {
  id: string
  name: string
  updatedAt: string
  lineCount: number
  audioCount: number
}

type StoryboardProjectSummary = {
  id: number
  title: string
  shotCount: number
}

type StoryboardShotOption = {
  id: number
  globalShot: number
  label: string
  script: string
}

type DialoguesFile = {
  project?: string
  characters: Character[]
  lines: DialogueLine[]
}

const STORAGE_KEY = 'novaly_tts_active_project_id'

const configured = ref(false)
const projectId = ref('')
const projectSourceId = ref<number | null>(null)
const projectExtractionMode = ref('')
const sourceProjectId = ref<number | null>(null)
const projectName = ref('')
const characters = ref<Character[]>([])
const lines = ref<DialogueLine[]>([])
const projects = ref<ProjectSummary[]>([])
const sourceProjects = ref<StoryboardProjectSummary[]>([])
const importing = ref(false)
const extracting = ref(false)
const extractDialogVisible = ref(false)
const loadingSourceShots = ref(false)
const pageBooting = ref(false)
const sourceShots = ref<StoryboardShotOption[]>([])
const selectedShotIds = ref<number[]>([])
const extractProgress = ref({ done: 0, total: 0 })
const EXTRACT_CHUNK_SIZE = 3
const saving = ref(false)
const previewingId = ref<string | null>(null)
const previewUrl = ref('')
const batching = ref(false)
const batchProgress = ref({ done: 0, total: 0 })
const filterSpeaker = ref('')
const error = ref('')
const saveHint = ref('')
const dirty = ref(false)
let saveTimer: ReturnType<typeof setTimeout> | null = null
let skipDirty = false

const fileInput = ref<HTMLInputElement | null>(null)

const speakers = computed(() => {
  const set = new Set<string>()
  for (const line of lines.value) {
    if (line.speaker) set.add(line.speaker)
  }
  for (const c of characters.value) {
    if (c.name) set.add(c.name)
  }
  return Array.from(set)
})

const filteredLines = computed(() => {
  const list = !filterSpeaker.value
    ? [...lines.value]
    : lines.value.filter(l => l.speaker === filterSpeaker.value)
  return list.sort((a, b) => (a.global_shot || 0) - (b.global_shot || 0) || String(a.time).localeCompare(String(b.time)))
})

const audioReadyCount = computed(() => lines.value.filter(l => l.audioReady || l.audioUrl).length)

const voiceMap = computed(() => {
  const m = new Map<string, Character>()
  for (const c of characters.value) m.set(c.name, c)
  return m
})

function resolveVoice(line: DialogueLine): string {
  if (line.voice_type?.trim()) return line.voice_type.trim()
  return voiceMap.value.get(line.speaker)?.voice_type?.trim() || ''
}

function resolveSpeed(line: DialogueLine): number {
  if ((line.speed_ratio || 0) > 0) return line.speed_ratio!
  return voiceMap.value.get(line.speaker)?.default_speed || 1
}

/** 按当前总分镜 + 说话人 + 台词生成导出文件名（改台词后会跟着变） */
function buildAudioFilename(line: DialogueLine): string {
  const shot = Number(line.global_shot) || 0
  const speaker = (line.speaker || '未知').trim()
  const short = (line.text || '')
    .trim()
    .replace(/[^\w\u4e00-\u9fff]+/g, '')
    .slice(0, 16) || 'line'
  const raw = `分镜${String(shot).padStart(2, '0')}_${speaker}_${short}.mp3`
  return raw.replace(/[\\/:*?"<>|]+/g, '_')
}

function uid(prefix = 'id') {
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`
}

async function refreshStatus() {
  try {
    const s = await api('/tts/status')
    configured.value = !!s.configured
  } catch {
    configured.value = false
  }
}

async function loadProjectList() {
  try {
    const res = await api('/tts/projects')
    projects.value = res.projects || []
  } catch {
    projects.value = []
  }
}

async function loadSourceProjects() {
  try {
    sourceProjects.value = await api('/projects')
  } catch {
    sourceProjects.value = []
  }
}

function applyProject(p: any) {
  skipDirty = true
  projectId.value = p.id || ''
  projectSourceId.value = Number(p.sourceProjectId) || null
  projectExtractionMode.value = p.extractionMode || ''
  sourceProjectId.value = projectSourceId.value
  projectName.value = p.name || '未命名'
  characters.value = (p.characters || []).map((c: Character) => ({
    id: c.id || uid('char'),
    name: c.name || '',
    voice_hint: c.voice_hint || '',
    voice_type: c.voice_type || '',
    default_speed: c.default_speed > 0 ? c.default_speed : 1,
  }))
  lines.value = (p.lines || []).map((l: DialogueLine) => ({
    ...l,
    id: l.id || uid('line'),
    global_shot: l.global_shot || l.episode || 0,
    speaker: l.speaker || '',
    text: l.text || '',
    tone: l.tone || '',
    emotion: l.emotion || l.tone || '',
    emotion_strength: Number(l.emotion_strength) || 4,
    enable_emotion: l.enable_emotion !== false,
    pitch: Number(l.pitch) || 0,
    speech_rate: Number(l.speech_rate) || 0,
    loudness_rate: Number(l.loudness_rate) || 0,
    time: l.time || '',
    type: l.type || '台词',
    speed_ratio: (l.speed_ratio || 0) > 0 ? (l.speed_ratio || 1) : 1,
    voice_type: l.voice_type || '',
    filename: l.filename || `${l.id || 'line'}.mp3`,
    audioUrl: l.audioUrl || '',
    audioReady: !!(l.audioReady || l.audioUrl),
  }))
  dirty.value = false
  if (projectId.value) localStorage.setItem(STORAGE_KEY, projectId.value)
  setTimeout(() => { skipDirty = false }, 0)
}

async function openProject(id: string) {
  const p = await api(`/tts/projects/${id}`)
  applyProject(p)
  ElMessage.success(`已打开：${p.name}`)
}

function onSelectProject(id: string | null) {
  if (id) openProject(id)
}

function payload() {
  return {
    id: projectId.value || undefined,
    sourceProjectId: projectSourceId.value || undefined,
    extractionMode: projectExtractionMode.value || undefined,
    name: projectName.value || '未命名台词项目',
    characters: characters.value,
    lines: lines.value,
  }
}

const extractedShotIds = computed(() => {
  const set = new Set<number>()
  for (const line of lines.value) {
    if (line.source_shot_id) set.add(line.source_shot_id)
  }
  return set
})

const selectableShots = computed(() => sourceShots.value.filter(shot => shot.script.trim()))

const unextractedShots = computed(() =>
  selectableShots.value.filter(shot => !extractedShotIds.value.has(shot.id)),
)

async function extractFromStoryboard() {
  if (!sourceProjectId.value) {
    ElMessage.warning('请选择要提取台词的分镜项目')
    return
  }
  const source = sourceProjects.value.find(p => p.id === sourceProjectId.value)
  if (!source?.shotCount) {
    ElMessage.warning('这个项目还没有分镜')
    return
  }
  if (!selectedShotIds.value.length) {
    ElMessage.warning('请至少选择一个分镜')
    return
  }
  extracting.value = true
  error.value = ''
  extractProgress.value = { done: 0, total: selectedShotIds.value.length }
  try {
    const shotIds = [...selectedShotIds.value]
    let syncedProjectId = projectId.value &&
      projectSourceId.value === sourceProjectId.value &&
      projectExtractionMode.value === 'ai'
      ? projectId.value
      : ''
    let failedChunks = 0
    let succeededShots = 0
    for (let i = 0; i < shotIds.length; i += EXTRACT_CHUNK_SIZE) {
      const chunk = shotIds.slice(i, i + EXTRACT_CHUNK_SIZE)
      try {
        const p = await api(`/tts/from-project/${sourceProjectId.value}`, {
          method: 'POST',
          body: JSON.stringify({
            ttsProjectId: syncedProjectId,
            useAI: true,
            shotIds: chunk,
          }),
        })
        applyProject(p)
        syncedProjectId = p.id || syncedProjectId
        succeededShots += chunk.length
      } catch (chunkErr: any) {
        failedChunks += 1
        error.value = chunkErr?.message || '部分分镜提取失败'
        ElMessage.warning(`第 ${i + 1}-${i + chunk.length} 个分镜失败：${error.value}`)
      }
      extractProgress.value = {
        done: Math.min(i + chunk.length, shotIds.length),
        total: shotIds.length,
      }
    }
    await loadProjectList()
    if (!failedChunks) {
      extractDialogVisible.value = false
      ElMessage.success(`AI 已完成 ${shotIds.length} 个分镜，当前共 ${lines.value.length} 条台词`)
    } else if (succeededShots > 0) {
      ElMessage.warning(`已完成 ${succeededShots} 个分镜（${failedChunks} 批失败），当前共 ${lines.value.length} 条台词；可对失败分镜重试`)
    } else {
      ElMessage.error(error.value || '提取失败')
    }
  } catch (e: any) {
    error.value = e.message || '提取失败'
    ElMessage.error(error.value)
  } finally {
    extracting.value = false
    extractProgress.value = { done: 0, total: 0 }
  }
}

async function loadShotsFromEpisodes(projectId: number): Promise<StoryboardShotOption[]> {
  const project = await api(`/projects/${projectId}`)
  const genByShot = new Map<number, string>()
  for (const r of project.resources || []) {
    if (r?.type !== 'video' || !r.shotId) continue
    const text = String(r.genScript || r.description || '').trim()
    if (!text || genByShot.has(r.shotId)) continue
    genByShot.set(r.shotId, text)
  }
  const episodes = (project.episodes || []).filter((ep: { id?: number }) => ep?.id)
  const episodeDetails = await Promise.all(
    episodes.map((ep: { id: number }) => api(`/episodes/${ep.id}`)),
  )
  let globalShot = 0
  const shots: StoryboardShotOption[] = []
  for (const ep of episodeDetails) {
    for (const shot of ep.shots || []) {
      globalShot += 1
      shots.push({
        id: shot.id,
        globalShot,
        label: shot.label || `分镜 ${globalShot}`,
        script: String(shot.script || genByShot.get(shot.id) || '').trim(),
      })
    }
  }
  return shots
}

async function openExtractDialog() {
  if (!sourceProjectId.value) {
    ElMessage.warning('请选择要提取台词的分镜项目')
    return
  }
  extractDialogVisible.value = true
  loadingSourceShots.value = true
  try {
    let shots: StoryboardShotOption[] = []
    // Prefer dedicated endpoint when deployed; fall back to episode paging
    // because GET /projects/:id no longer embeds shots.
    try {
      const data = await api(`/tts/from-project/${sourceProjectId.value}/shots`)
      if (Array.isArray(data)) shots = data
    } catch {
      shots = []
    }
    if (!shots.length) {
      shots = await loadShotsFromEpisodes(sourceProjectId.value)
    }
    sourceShots.value = shots
    const firstUnextracted = shots.find(shot => !extractedShotIds.value.has(shot.id) && shot.script.trim())
    const firstSelectable = firstUnextracted || shots.find(shot => shot.script.trim())
    selectedShotIds.value = firstSelectable ? [firstSelectable.id] : []
    if (!selectableShots.value.length) {
      ElMessage.warning(shots.length ? '分镜文案为空，无法提取' : '该项目没有分镜')
    }
  } catch (e: any) {
    extractDialogVisible.value = false
    ElMessage.error(e.message || '读取分镜失败')
  } finally {
    loadingSourceShots.value = false
  }
}

function selectAllShots() {
  selectedShotIds.value = selectableShots.value.map(shot => shot.id)
}

function selectUnextractedShots() {
  selectedShotIds.value = unextractedShots.value.map(shot => shot.id)
  if (!selectedShotIds.value.length) {
    ElMessage.info('没有未提取的分镜')
  }
}

function clearSelectedShots() {
  selectedShotIds.value = []
}

async function saveProject(silent = false) {
  if (!characters.value.length && !lines.value.length) return
  saving.value = true
  try {
    const method = projectId.value ? 'PUT' : 'POST'
    const path = projectId.value ? `/tts/projects/${projectId.value}` : '/tts/projects'
    const p = await api(path, { method, body: JSON.stringify(payload()) })
    skipDirty = true
    projectId.value = p.id
    projectName.value = p.name
    if (Array.isArray(p.lines)) {
      const byId = new Map<string, DialogueLine>(p.lines.map((l: DialogueLine) => [l.id, l]))
      lines.value = lines.value.map(l => {
        const s = byId.get(l.id)
        if (!s) return l
        return { ...l, audioUrl: s.audioUrl || l.audioUrl, audioReady: !!(s.audioReady || s.audioUrl || l.audioReady) }
      })
    }
    dirty.value = false
    localStorage.setItem(STORAGE_KEY, projectId.value)
    saveHint.value = `已保存 ${new Date().toLocaleTimeString()}`
    await loadProjectList()
    if (!silent) ElMessage.success('已保存')
  } catch (e: any) {
    error.value = e.message
    if (!silent) ElMessage.error(e.message)
  } finally {
    saving.value = false
    skipDirty = false
  }
}

function scheduleSave() {
  if (skipDirty) return
  dirty.value = true
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = setTimeout(() => saveProject(true), 800)
}

watch([projectName, characters, lines], () => scheduleSave(), { deep: true })

function normalizeImported(data: DialoguesFile) {
  applyProject({
    id: '',
    name: data.project || '未命名',
    characters: data.characters || [],
    lines: data.lines || [],
  })
  dirty.value = true
}

async function onFileChange(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importing.value = true
  error.value = ''
  try {
    const text = await file.text()
    const data = JSON.parse(text) as DialoguesFile
    if (!Array.isArray(data.lines)) throw new Error('JSON 缺少 lines 数组')
    normalizeImported(data)
    await saveProject(true)
    ElMessage.success(`已导入并保存 ${lines.value.length} 条台词`)
  } catch (e: any) {
    error.value = e.message || '导入失败'
    ElMessage.error(error.value)
  } finally {
    importing.value = false
    input.value = ''
  }
}

function triggerImport() {
  fileInput.value?.click()
}

function addCharacter() {
  characters.value.push({
    id: uid('char'),
    name: '新角色',
    voice_hint: '',
    voice_type: '',
    default_speed: 1,
  })
}

function removeCharacter(idx: number) {
  characters.value.splice(idx, 1)
}

function addLine() {
  const id = uid('line')
  const maxShot = lines.value.reduce((m, l) => Math.max(m, l.global_shot || 0), 0)
  // Keep the new line visible under the current speaker filter.
  const speaker = (filterSpeaker.value || characters.value[0]?.name || '').trim()
  const charVoice = speaker ? voiceMap.value.get(speaker)?.voice_type?.trim() || '' : ''
  const charSpeed = speaker ? voiceMap.value.get(speaker)?.default_speed || 1 : 1
  lines.value.push({
    id,
    global_shot: maxShot + 1,
    time: '0-3秒',
    type: '台词',
    speaker,
    text: '',
    tone: '',
    emotion: '',
    emotion_hint: '',
    emotion_strength: 4,
    enable_emotion: true,
    pitch: 0,
    speech_rate: 0,
    loudness_rate: 0,
    speed_ratio: charSpeed,
    filename: `分镜${String(maxShot + 1).padStart(2, '0')}_${speaker || id}.mp3`,
    voice_type: charVoice,
  })
  ElMessage.success(speaker ? `已添加「${speaker}」台词` : '已添加台词')
}

function removeLine(row: DialogueLine) {
  const i = lines.value.findIndex(l => l.id === row.id)
  if (i >= 0) lines.value.splice(i, 1)
}

async function ensureProjectSaved() {
  if (!projectId.value || dirty.value) {
    await saveProject(true)
  }
  if (!projectId.value) throw new Error('请先保存项目')
}

async function previewLine(line: DialogueLine) {
  const voiceType = resolveVoice(line)
  if (!voiceType) {
    ElMessage.warning('请先为该角色或该行填写 voice_type')
    return
  }
  if (!line.text.trim()) {
    ElMessage.warning('台词为空')
    return
  }
  previewingId.value = line.id
  error.value = ''
  try {
    await ensureProjectSaved()
    const res = await api(`/tts/projects/${projectId.value}/synthesize`, {
      method: 'POST',
      body: JSON.stringify({
        lineId: line.id,
        text: line.text,
        voiceType,
        speedRatio: resolveSpeed(line),
        speechRate: line.speech_rate,
        pitch: line.pitch,
        loudnessRate: line.loudness_rate,
        enableEmotion: line.enable_emotion,
        emotion: line.emotion || line.tone || '',
        emotionStrength: line.emotion_strength || 4,
        emotionHint: line.emotion_hint || '',
        tone: line.tone || '',
        globalShot: line.global_shot,
        speaker: line.speaker,
        filename: buildAudioFilename(line),
      }),
    })
    line.audioUrl = res.audioUrl
    line.audioReady = true
    line.filename = res.filename || buildAudioFilename(line)
    if (res.project) applyProject(res.project)
    previewUrl.value = res.audioUrl + '?t=' + Date.now()
    ElMessage.success('已生成并保存')
    await loadProjectList()
  } catch (e: any) {
    error.value = e.message
    ElMessage.error(e.message)
  } finally {
    previewingId.value = null
  }
}

async function batchGenerate() {
  const scope = filterSpeaker.value ? filteredLines.value : lines.value
  if (!scope.length) {
    ElMessage.warning(filterSpeaker.value ? `当前筛选「${filterSpeaker.value}」没有台词` : '请先导入或添加台词')
    return
  }
  const ready: DialogueLine[] = []
  const missingSpeakers = new Set<string>()
  for (const line of scope) {
    if (!line.text.trim()) continue
    if (!resolveVoice(line)) {
      missingSpeakers.add(line.speaker || line.id)
      continue
    }
    ready.push(line)
  }
  if (!ready.length) {
    const who = [...missingSpeakers].slice(0, 3).join('、') || '所选台词'
    ElMessage.warning(`没有可生成的台词：${who} 缺少 voice_type`)
    return
  }
  if (missingSpeakers.size) {
    const who = [...missingSpeakers].slice(0, 5).join('、')
    const more = missingSpeakers.size > 5 ? ` 等 ${missingSpeakers.size} 个角色` : ''
    ElMessage.info(`已跳过未配音色的角色：${who}${more}（共跳过 ${scope.length - ready.length} 条）`)
  }
  const confirmScope = filterSpeaker.value
    ? `将生成「${filterSpeaker.value}」的 ${ready.length} 条台词，继续？`
    : `将生成 ${ready.length} 条已配音色的台词${missingSpeakers.size ? `（已跳过 ${scope.length - ready.length} 条）` : ''}，继续？`
  if (ready.length > 30 && !confirm(confirmScope)) return

  batching.value = true
  error.value = ''
  try {
    await ensureProjectSaved()
    const start = await api(`/tts/projects/${projectId.value}/batch`, {
      method: 'POST',
      body: JSON.stringify({ lineIds: ready.map(l => l.id) }),
    })
    const jobId = start.jobId as string
    batchProgress.value = { done: 0, total: start.total || ready.length }
    while (true) {
      await new Promise(r => setTimeout(r, 800))
      const job = await api(`/tts/jobs/${jobId}`)
      batchProgress.value = { done: job.done || 0, total: job.total || ready.length }
      if (job.status === 'done') {
        const p = await api(`/tts/projects/${projectId.value}`)
        applyProject(p)
        ElMessage.success(`批量完成 ${job.done} 条，已保存`)
        await loadProjectList()
        break
      }
      if (job.status === 'error') throw new Error(job.error || '批量合成失败')
    }
  } catch (e: any) {
    error.value = e.message
    ElMessage.error(e.message)
  } finally {
    batching.value = false
  }
}

async function downloadLine(line: DialogueLine) {
  if (!line.audioUrl) {
    ElMessage.warning('请先生成该条音频')
    return
  }
  try {
    const res = await fetch(line.audioUrl)
    if (!res.ok) throw new Error(`下载失败（HTTP ${res.status}）`)
    const blob = await res.blob()
    const where = await saveBlobDownload(blob, buildAudioFilename(line))
    if (where === 'dir') ElMessage.success('已保存到配置的下载目录')
  } catch (e: any) {
    ElMessage.error(e.message || '下载失败')
  }
}

async function downloadZip() {
  if (!projectId.value) {
    ElMessage.warning('请先保存项目')
    return
  }
  if (!audioReadyCount.value) {
    ElMessage.warning('还没有已生成的音频')
    return
  }
  try {
    // 先保存，保证 zip 里用的是当前总分镜 + 改过的台词
    await ensureProjectSaved()
    const res = await fetch(`/api/tts/projects/${projectId.value}/download`)
    if (!res.ok) {
      let msg = `导出失败（HTTP ${res.status}）`
      try {
        const data = await res.json()
        if (data?.error) msg = data.error
      } catch { /* ignore */ }
      throw new Error(msg)
    }
    const blob = await res.blob()
    const name = `${(projectName.value || 'tts').trim() || 'tts'}_音频.zip`
    const where = await saveBlobDownload(blob, name)
    if (where === 'dir') ElMessage.success('已保存到配置的下载目录')
  } catch (e: any) {
    ElMessage.error(e.message || '导出失败')
  }
}

async function deleteCurrentProject() {
  if (!projectId.value) {
    projectSourceId.value = null
    projectExtractionMode.value = ''
    characters.value = []
    lines.value = []
    projectName.value = ''
    return
  }
  try {
    await ElMessageBox.confirm('确定删除当前台词项目？音频也会删除。', '删除确认', { type: 'warning' })
    await api(`/tts/projects/${projectId.value}`, { method: 'DELETE' })
    localStorage.removeItem(STORAGE_KEY)
    projectId.value = ''
    projectSourceId.value = null
    projectExtractionMode.value = ''
    projectName.value = ''
    characters.value = []
    lines.value = []
    dirty.value = false
    await loadProjectList()
    ElMessage.success('已删除')
  } catch { /* cancel */ }
}

function playSaved(line: DialogueLine) {
  if (!line.audioUrl) return
  previewUrl.value = line.audioUrl + (line.audioUrl.includes('?') ? '&' : '?') + 't=' + Date.now()
}

function togglePlay(line: DialogueLine) {
  if (!line.audioUrl) {
    ElMessage.warning('请先生成该条音频')
    return
  }
  playSaved(line)
}

onMounted(async () => {
  pageBooting.value = true
  try {
    await refreshStatus()
    await Promise.all([loadProjectList(), loadSourceProjects()])
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved) {
      try {
        await openProject(saved)
        return
      } catch { /* fallthrough */ }
    }
    if (projects.value.length) {
      try { await openProject(projects.value[0].id) } catch { /* empty */ }
    }
  } finally {
    pageBooting.value = false
  }
})
</script>

<template>
  <section
    class="workspace tts-page"
    v-loading="pageBooting"
    element-loading-text="加载配音项目…"
    element-loading-background="rgba(17, 16, 15, 0.55)"
  >
    <header class="tts-header">
      <div>
        <p class="eyebrow">VOICE LAB</p>
        <h1>分镜台词配音</h1>
        <p class="premise">
          从分镜文案提取角色、台词、情绪、语速和音调；可逐条调整并合成，生成结果支持单条或批量下载。
        </p>
      </div>
      <div class="tts-actions">
        <el-tag :type="configured ? 'success' : 'warning'" effect="dark">
          {{ configured ? 'TTS 已配置' : '未配置 VOLC_TTS_*' }}
        </el-tag>
        <el-tag v-if="dirty" type="info" effect="plain">未保存…</el-tag>
        <el-tag v-else-if="saveHint" type="success" effect="plain">{{ saveHint }}</el-tag>
        <input ref="fileInput" type="file" accept="application/json,.json" hidden @change="onFileChange" />
        <el-button :loading="importing" @click="triggerImport">导入 JSON</el-button>
        <el-button type="primary" :loading="saving" @click="saveProject(false)">保存</el-button>
      </div>
    </header>

    <el-alert v-if="error" :title="error" type="error" show-icon class="tts-alert" @close="error = ''" />

    <el-card class="tts-card" shadow="never">
      <template #header>
        <div class="card-head">
          <span>分镜来源与配音项目</span>
          <div class="head-right">
            <el-button size="small" :icon="Delete" @click="deleteCurrentProject">删除当前</el-button>
          </div>
        </div>
      </template>
      <div class="source-row">
        <el-select
          v-model="sourceProjectId"
          filterable
          placeholder="选择 Novaly 分镜项目"
          style="width: 320px"
        >
          <el-option
            v-for="p in sourceProjects"
            :key="p.id"
            :label="`${p.title}（${p.shotCount} 分镜）`"
            :value="p.id"
          />
        </el-select>
        <el-button type="primary" :loading="extracting" :disabled="!sourceProjectId" @click="openExtractDialog">
          {{ projectSourceId === sourceProjectId && projectId && projectExtractionMode === 'ai' ? '选择分镜继续提取' : '选择分镜 AI 提取' }}
        </el-button>
        <span class="muted">AI 识别角色、台词和配音参数；支持全选或只选未提取分镜</span>
      </div>
      <div class="project-row">
        <el-input v-model="projectName" placeholder="配音项目名称" style="max-width: 320px" />
        <el-select
          :model-value="projectId"
          placeholder="打开已存项目"
          clearable
          style="width: 280px"
          @change="onSelectProject"
        >
          <el-option
            v-for="p in projects"
            :key="p.id"
            :label="`${p.name}（${p.audioCount}/${p.lineCount}）`"
            :value="p.id"
          />
        </el-select>
        <span class="muted">已生成音频 {{ audioReadyCount }} / {{ lines.length }}</span>
      </div>
    </el-card>

    <el-card class="tts-card" shadow="never">
      <template #header>
        <div class="card-head">
          <span>角色音色</span>
          <el-button size="small" :icon="Plus" @click="addCharacter">加角色</el-button>
        </div>
      </template>
      <el-table :data="characters" size="small" class="tts-table" empty-text="导入或添加角色">
        <el-table-column label="角色" width="140">
          <template #default="{ row }">
            <el-input v-model="row.name" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="音色建议" min-width="180">
          <template #default="{ row }">
            <el-input v-model="row.voice_hint" size="small" placeholder="可复制粘贴" />
          </template>
        </el-table-column>
        <el-table-column label="voice_type" min-width="240">
          <template #default="{ row }">
            <el-input v-model="row.voice_type" size="small" placeholder="粘贴火山音色 ID" />
          </template>
        </el-table-column>
        <el-table-column label="默认语速" width="140">
          <template #default="{ row }">
            <el-input-number v-model="row.default_speed" :min="0.2" :max="3" :step="0.05" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="" width="70" fixed="right">
          <template #default="{ $index }">
            <el-button link type="danger" :icon="Delete" @click="removeCharacter($index)" />
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card class="tts-card" shadow="never">
      <template #header>
        <div class="card-head">
          <span>台词列表（{{ filteredLines.length }} / {{ lines.length }}）</span>
          <div class="head-right">
            <el-select v-model="filterSpeaker" clearable placeholder="按角色筛选" style="width: 140px">
              <el-option v-for="s in speakers" :key="s" :label="s" :value="s" />
            </el-select>
            <el-button size="small" :icon="Plus" @click="addLine">加台词</el-button>
            <el-button type="primary" :loading="batching" :disabled="!configured || !filteredLines.length" @click="batchGenerate">
              {{ filterSpeaker ? `批量生成「${filterSpeaker}」` : '批量生成' }}
            </el-button>
            <el-button :icon="Download" :disabled="!audioReadyCount" @click="downloadZip">批量下载 zip</el-button>
          </div>
        </div>
        <el-progress
          v-if="batching || (batchProgress.total && batchProgress.done && batchProgress.done < batchProgress.total)"
          :percentage="batchProgress.total ? Math.round((batchProgress.done / batchProgress.total) * 100) : 0"
          :stroke-width="8"
          class="batch-progress"
        />
      </template>

      <div v-if="previewUrl" class="preview-bar">
        <el-icon><Headset /></el-icon>
        <audio :src="previewUrl" controls autoplay class="preview-audio" />
      </div>

      <el-table :data="filteredLines" size="small" max-height="560" class="tts-table" empty-text="请先选择项目并从分镜提取台词">
        <el-table-column label="总分镜" width="90">
          <template #default="{ row }">
            <el-input-number v-model="row.global_shot" :min="0" :controls="false" size="small" class="num-sm" />
          </template>
        </el-table-column>
        <el-table-column label="时段" width="100">
          <template #default="{ row }">
            <el-input v-model="row.time" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="说话人" width="110">
          <template #default="{ row }">
            <el-input v-model="row.speaker" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="台词（可粘贴）" min-width="200">
          <template #default="{ row }">
            <el-input v-model="row.text" type="textarea" :autosize="{ minRows: 1, maxRows: 4 }" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="情感指令" min-width="360">
          <template #default="{ row }">
            <div class="emotion-cell">
              <el-input v-model="row.emotion" type="textarea" :autosize="{ minRows: 2, maxRows: 6 }" size="small" />
              <el-tag v-if="row.needs_review" size="small" type="warning" effect="plain">建议复核</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="情感" width="72" align="center">
          <template #default="{ row }">
            <el-switch v-model="row.enable_emotion" />
          </template>
        </el-table-column>
        <el-table-column label="强度" width="108">
          <template #default="{ row }">
            <el-input-number v-model="row.emotion_strength" :min="1" :max="5" :step="1" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="音调" width="100">
          <template #default="{ row }">
            <el-input-number v-model="row.pitch" :min="-12" :max="12" :step="1" :controls="false" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="语速" width="100">
          <template #default="{ row }">
            <el-input-number v-model="row.speech_rate" :min="-50" :max="100" :step="5" :controls="false" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="音量" width="100">
          <template #default="{ row }">
            <el-input-number v-model="row.loudness_rate" :min="-50" :max="100" :step="5" :controls="false" size="small" />
          </template>
        </el-table-column>
        <el-table-column label="覆盖音色" min-width="160">
          <template #default="{ row }">
            <el-input v-model="row.voice_type" size="small" :placeholder="resolveVoice(row) || '用角色音色'" />
          </template>
        </el-table-column>
        <el-table-column label="音频" width="90">
          <template #default="{ row }">
            <el-button
              v-if="row.audioReady || row.audioUrl"
              link
              type="success"
              @click="togglePlay(row)"
            >
              播放
            </el-button>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180">
          <template #default="{ row }">
            <el-button link type="primary" :loading="previewingId === row.id" :disabled="!configured" @click="previewLine(row)">
              生成
            </el-button>
            <el-button link type="primary" :disabled="!(row.audioReady || row.audioUrl)" @click="downloadLine(row)">
              下载
            </el-button>
            <el-button link type="danger" @click="removeLine(row)">删</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="extractDialogVisible" title="选择分镜 · AI 提取台词" width="760px" destroy-on-close>
      <div class="extract-dialog-tools">
        <span class="muted">可全选重新提取，或只选未提取分镜；每批 {{ EXTRACT_CHUNK_SIZE }} 个并行提取，进度会逐步更新。选得多会多花时间，可一直挂着跑完。</span>
        <div class="extract-dialog-actions">
          <el-button size="small" @click="selectUnextractedShots">仅未提取（{{ unextractedShots.length }}）</el-button>
          <el-button size="small" @click="selectAllShots">全选（{{ selectableShots.length }}）</el-button>
          <el-button size="small" @click="clearSelectedShots">清空</el-button>
        </div>
      </div>
      <div v-loading="loadingSourceShots || extracting" class="shot-choice-list">
        <el-checkbox-group v-model="selectedShotIds" :disabled="extracting">
          <label v-for="shot in sourceShots" :key="shot.id" class="shot-choice">
            <el-checkbox :value="shot.id" :disabled="!shot.script.trim() || extracting">
              总分镜 {{ shot.globalShot }} · {{ shot.label }}
              <span v-if="extractedShotIds.has(shot.id)" class="shot-status done">已提取</span>
              <span v-else-if="shot.script.trim()" class="shot-status pending">未提取</span>
            </el-checkbox>
            <p>{{ shot.script.trim().slice(0, 180) || '分镜文案为空' }}</p>
          </label>
        </el-checkbox-group>
      </div>
      <el-progress
        v-if="extracting && extractProgress.total"
        :percentage="Math.round((extractProgress.done / extractProgress.total) * 100)"
        :stroke-width="8"
        class="extract-progress"
      />
      <template #footer>
        <span class="dialog-footer">
          <span class="muted">
            已选 {{ selectedShotIds.length }} / {{ selectableShots.length }}
            <template v-if="extracting && extractProgress.total">
              · 进度 {{ extractProgress.done }}/{{ extractProgress.total }}
            </template>
          </span>
          <el-button @click="extractDialogVisible = false" :disabled="extracting">取消</el-button>
          <el-button
            type="primary"
            :loading="extracting"
            :disabled="!selectedShotIds.length"
            @click="extractFromStoryboard"
          >
            AI 提取并同步
          </el-button>
        </span>
      </template>
    </el-dialog>
  </section>
</template>

<style scoped>
.tts-page {
  padding: 28px 32px 48px;
  width: 100%;
  max-width: 1200px;
  margin: 0 auto;
  box-sizing: border-box;
}

.tts-header {
  display: flex;
  justify-content: space-between;
  gap: 24px;
  align-items: flex-start;
  margin-bottom: 20px;
}

.eyebrow {
  margin: 0 0 6px;
  font-size: 11px;
  letter-spacing: 0.16em;
  color: #8f8880;
}

.tts-header h1 {
  margin: 0 0 8px;
  font-size: 28px;
  font-weight: 800;
  color: #eee9e1;
}

.premise {
  margin: 0;
  max-width: 640px;
  color: #aaa197;
  line-height: 1.55;
  font-size: 14px;
}

.tts-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.tts-alert {
  margin-bottom: 16px;
}

.tts-card {
  margin-bottom: 16px;
  background: #171513;
  border: 1px solid #3c3731;
}

.card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.head-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.project-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.source-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  padding-bottom: 14px;
  margin-bottom: 14px;
  border-bottom: 1px solid #302c28;
}

.emotion-cell {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

.extract-dialog-tools,
.dialog-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.extract-dialog-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.extract-progress {
  margin-top: 12px;
}

.shot-status {
  margin-left: 8px;
  font-size: 12px;
  font-weight: 500;
}

.shot-status.done {
  color: #67c23a;
}

.shot-status.pending {
  color: #c0a060;
}

.shot-choice-list {
  max-height: 520px;
  min-height: 120px;
  overflow: auto;
  margin-top: 14px;
  padding-right: 6px;
}

.shot-choice {
  display: block;
  padding: 12px 14px;
  margin-bottom: 8px;
  border: 1px solid #3c3731;
  border-radius: 8px;
  background: #171513;
  cursor: pointer;
}

.shot-choice:has(.is-checked) {
  border-color: rgba(255, 120, 90, 0.65);
  background: rgba(255, 120, 90, 0.08);
}

.shot-choice p {
  margin: 7px 0 0 24px;
  color: #8f8880;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  overflow: hidden;
}

.muted {
  color: #8f8880;
  font-size: 12px;
  font-weight: 400;
}

.batch-progress {
  margin-top: 12px;
}

.preview-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  padding: 10px 12px;
  background: #211e1b;
  border: 1px solid #3c3731;
  border-radius: 8px;
}

.preview-audio {
  flex: 1;
  height: 36px;
}

.tts-table {
  --el-table-bg-color: transparent;
  --el-table-tr-bg-color: transparent;
  --el-table-header-bg-color: #211e1b;
}

.num-sm {
  width: 56px;
}

.audio-tag {
  cursor: pointer;
}
</style>

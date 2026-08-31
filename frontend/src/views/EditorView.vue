<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api/client'

type TrackKind = 'video' | 'audio' | 'image' | 'text' | 'effect' | 'filter'
type MediaKind = 'video' | 'audio' | 'image' | 'text' | 'transition' | 'effect' | 'filter'
type Clip = {
  id: string; trackId: string; kind: TrackKind; name: string; src?: string; start: number; duration: number
  trimStart?: number; volume?: number; opacity?: number; playbackRate?: number; text?: string; color?: string
  fontSize?: number; x?: number; y?: number; filter?: string; effect?: string; transition?: string
}
type Track = { id: string; kind: TrackKind; name: string; visible: boolean; muted: boolean; locked: boolean; clips: Clip[] }
type MediaItem = { id: string; kind: MediaKind; name: string; src?: string; duration: number; filter?: string; effect?: string; transition?: string; shotId?: number; sortOrder?: number; sourceRank?: number; resourceId?: number }
type EditData = { tracks: Track[]; ratio: string; zoom: number; timelineHeight?: number }

const route = useRoute()
const router = useRouter()
const episodeId = computed(() => Number(route.params.episodeId))
const loading = ref(true)
const saving = ref(false)
const exporting = ref(false)
const project = ref<any>(null)
const episode = ref<any>(null)
const resources = ref<any[]>([])
const shots = ref<any[]>([])
const libraryTab = ref<MediaKind>('video')
const search = ref('')
const playhead = ref(0)
const playing = ref(false)
const selectedClipId = ref('')
const zoom = ref(72)
const tracks = ref<Track[]>([])
const videoEl = ref<HTMLVideoElement | null>(null)
const audioEl = ref<HTMLAudioElement | null>(null)
const previewSectionEl = ref<HTMLElement | null>(null)
const timelineScrollEl = ref<HTMLElement | null>(null)
const mediaLoading = ref(false)
const mediaError = ref('')
const previewFit = ref<'contain' | 'cover'>('contain')
const previewArea = reactive({ width: 0, height: 0 })
const timelineAreaWidth = ref(0)
const timelineHeight = ref(320)
const history = ref<string[]>([])
const historyIndex = ref(-1)
let raf = 0
let lastTick = 0
let mediaSwitchToken = 0
let saveTimer: number | undefined
let previewResizeObserver: ResizeObserver | null = null
let timelineResizeObserver: ResizeObserver | null = null

const uid = (prefix: string) => `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
const defaultTracks = (): Track[] => [
  { id: uid('track'), kind: 'video', name: '主视频', visible: true, muted: false, locked: false, clips: [] },
  { id: uid('track'), kind: 'image', name: '画中画', visible: true, muted: false, locked: false, clips: [] },
  { id: uid('track'), kind: 'audio', name: '音频', visible: true, muted: false, locked: false, clips: [] },
  { id: uid('track'), kind: 'text', name: '字幕', visible: true, muted: false, locked: false, clips: [] },
  { id: uid('track'), kind: 'filter', name: '滤镜/特效', visible: true, muted: false, locked: false, clips: [] },
]

const totalDuration = computed(() => Math.max(10, ...tracks.value.flatMap(t => t.clips.map(c => c.start + c.duration))))
const selectedClip = computed(() => tracks.value.flatMap(t => t.clips).find(c => c.id === selectedClipId.value) || null)
const selectedTrack = computed(() => tracks.value.find(t => t.clips.some(c => c.id === selectedClipId.value)) || null)
const activeClips = computed(() => tracks.value.flatMap(t => t.visible ? t.clips : []).filter(c => playhead.value >= c.start && playhead.value < c.start + c.duration))
const activeVisual = computed(() => [...activeClips.value].reverse().find(c => c.kind === 'video' || c.kind === 'image'))
const activeAudio = computed(() => activeClips.value.find(c => c.kind === 'audio' && !tracks.value.find(t => t.id === c.trackId)?.muted))
const activeTexts = computed(() => activeClips.value.filter(c => c.kind === 'text'))
const activeFilter = computed(() => [...activeClips.value].reverse().find(c => c.kind === 'filter'))
const previewRatio = computed(() => (project.value?.videoRatio || '16:9').replace(':', ' / '))
const previewCanvasSize = computed(() => {
  const [rw, rh] = String(project.value?.videoRatio || '16:9').split(':').map(Number)
  const ratio = rw > 0 && rh > 0 ? rw / rh : 16 / 9
  const width = Math.max(0, previewArea.width - 36)
  const height = Math.max(0, previewArea.height - 82)
  if (!width || !height) return {}
  if (width / height > ratio) return { width: `${Math.floor(height * ratio)}px`, height: `${Math.floor(height)}px` }
  return { width: `${Math.floor(width)}px`, height: `${Math.floor(width / ratio)}px` }
})
const editorGridStyle = computed(() => ({ gridTemplateRows: `58px minmax(220px, 1fr) 7px ${timelineHeight.value}px` }))
const timelineContentWidth = computed(() => Math.max(totalDuration.value * zoom.value, Math.max(320, timelineAreaWidth.value - 180)))
const previewMediaStyle = computed(() => ({
  opacity: activeVisual.value?.opacity ?? 1,
  filter: activeFilter.value?.filter || 'none',
  animation: activeFilter.value?.effect
    ? `editor-${effectAnimation(activeFilter.value.effect)} .8s ease-in-out infinite alternate`
    : activeFilter.value?.transition
      ? `editor-${transitionAnimation(activeFilter.value.transition)} ${activeFilter.value.duration || .6}s ease both`
      : undefined,
}))
function previewTextStyle(text: Clip) {
  const x = Math.max(5, Math.min(95, text.x ?? 50))
  const y = Math.max(6, Math.min(92, text.y ?? 82))
  return { left: `${x}%`, top: `${y}%`, fontSize: `${Math.max(12, (text.fontSize ?? 38) / 2)}px`, color: text.color || '#fff' }
}

const filters: MediaItem[] = [
  { id: 'none', kind: 'filter', name: '原色', duration: 5, filter: 'none' },
  { id: 'gray', kind: 'filter', name: '黑白', duration: 5, filter: 'grayscale(1)' },
  { id: 'sepia', kind: 'filter', name: '复古', duration: 5, filter: 'sepia(.85)' },
  { id: 'warm', kind: 'filter', name: '暖色', duration: 5, filter: 'sepia(.25) saturate(1.25)' },
  { id: 'cool', kind: 'filter', name: '冷色', duration: 5, filter: 'hue-rotate(175deg) saturate(.8)' },
  { id: 'vivid', kind: 'filter', name: '鲜艳', duration: 5, filter: 'saturate(1.8) contrast(1.08)' },
  { id: 'bright', kind: 'filter', name: '明亮', duration: 5, filter: 'brightness(1.25)' },
  { id: 'contrast', kind: 'filter', name: '高对比', duration: 5, filter: 'contrast(1.5)' },
  { id: 'blur', kind: 'filter', name: '柔焦', duration: 5, filter: 'blur(3px)' },
]
const effects: MediaItem[] = ['淡入', '淡出', '闪白', '抖动', '放大', '缩小', '脉冲'].map((name, i) => ({ id: `effect-${i}`, kind: 'effect', name, duration: 2, effect: name }))
const transitions: MediaItem[] = ['淡化', '滑动', '擦除', '叠化', '缩放', '旋转'].map((name, i) => ({ id: `transition-${i}`, kind: 'transition', name, duration: .6, transition: name }))
const textItems: MediaItem[] = [
  { id: 'title', kind: 'text', name: '标题文字', duration: 3 },
  { id: 'subtitle', kind: 'text', name: '字幕文字', duration: 3 },
  { id: 'custom', kind: 'text', name: '自定义文字', duration: 3 },
]

const mediaItems = computed<MediaItem[]>(() => {
  const shotOrderById = new Map<number, number>(shots.value.map(s => [Number(s.id), Number(s.sortOrder) || Number.MAX_SAFE_INTEGER]))
  const currentShotIds = new Set<number>(shots.value.map(s => Number(s.id)))
  const currentVideoURLs = new Set<string>(shots.value.map(s => String(s.videoUrl || '').trim()).filter(Boolean))
  const shotVideos = shots.value.filter(s => s.videoUrl).map(s => ({ id: `shot-${s.id}`, kind: 'video' as const, name: `分镜 ${s.sortOrder} · ${s.label || '成片'}`, src: s.videoUrl, duration: s.duration || 10, shotId: Number(s.id), sortOrder: Number(s.sortOrder) || Number.MAX_SAFE_INTEGER, sourceRank: 0 }))
  const resourceItems = resources.value.flatMap(r => {
    const out: MediaItem[] = []
    const shotId = Number(r.shotId) || undefined
    if (r.videoUrl && resourceVideoBelongsToEpisode(r, currentShotIds) && !currentVideoURLs.has(String(r.videoUrl).trim())) {
      const sortOrder = shotId ? shotOrderById.get(shotId) : parseStoryboardNumber(r.name)
      out.push({ id: `rv-${r.id}`, kind: 'video', name: resourceVersionName(r, sortOrder), src: editorResourceVideoURL(r), duration: r.duration || 10, shotId, sortOrder, sourceRank: 1, resourceId: Number(r.id) })
    }
    if (r.imageUrl || r.stylizedImageUrl) out.push({ id: `ri-${r.id}`, kind: 'image', name: r.name, src: r.stylizedImageUrl || r.imageUrl, duration: 5 })
    if ((r.type === 'audio' || /\.(mp3|wav|m4a|aac)(\?|$)/i.test(r.videoUrl || '')) && r.videoUrl) out.push({ id: `ra-${r.id}`, kind: 'audio', name: r.name, src: r.videoUrl, duration: r.duration || 10 })
    return out
  })
  const videoItems = [...shotVideos, ...resourceItems.filter(i => i.kind === 'video')].sort(compareVideoMedia)
  const map: Record<MediaKind, MediaItem[]> = { video: videoItems, image: resourceItems.filter(i => i.kind === 'image'), audio: resourceItems.filter(i => i.kind === 'audio'), text: textItems, transition: transitions, effect: effects, filter: filters }
  const q = search.value.trim().toLowerCase()
  return map[libraryTab.value].filter(i => !q || i.name.toLowerCase().includes(q))
})

function resourceVideoBelongsToEpisode(resource: any, currentShotIds: Set<number>) {
  const shotId = Number(resource.shotId) || 0
  if (shotId) return currentShotIds.has(shotId)
  const match = String(resource.name || '').match(/第\s*(\d+)\s*集/)
  if (!match) return false
  return Number(match[1]) === Number(episode.value?.number)
}

function resourceVersionName(resource: any, sortOrder?: number) {
  const order = Number.isFinite(sortOrder) && Number(sortOrder) < Number.MAX_SAFE_INTEGER ? Number(sortOrder) : parseStoryboardNumber(resource.name)
  const original = String(resource.name || '').replace(/^第\s*\d+\s*集\s*[·._\-—]?\s*/i, '').trim()
  const explicitVersion = original.match(/版本\s*\d+/i)?.[0]
  const suffix = explicitVersion || `历史版本 · #${resource.id}`
  return order < Number.MAX_SAFE_INTEGER ? `分镜 ${order} · ${suffix}` : `${original || '视频素材'} · ${suffix}`
}

function parseStoryboardNumber(name = '') {
  const match = name.match(/分镜\s*0*(\d+)/i)
  return match ? Number(match[1]) : Number.MAX_SAFE_INTEGER
}

function compareVideoMedia(a: MediaItem, b: MediaItem) {
  const orderA = Number.isFinite(a.sortOrder) ? Number(a.sortOrder) : parseStoryboardNumber(a.name)
  const orderB = Number.isFinite(b.sortOrder) ? Number(b.sortOrder) : parseStoryboardNumber(b.name)
  if (orderA !== orderB) return orderA - orderB
  if ((a.sourceRank ?? 1) !== (b.sourceRank ?? 1)) return (a.sourceRank ?? 1) - (b.sourceRank ?? 1)
  if ((a.shotId ?? 0) !== (b.shotId ?? 0)) return (a.shotId ?? 0) - (b.shotId ?? 0)
  return (b.resourceId ?? 0) - (a.resourceId ?? 0)
}

function editorResourceVideoURL(resource: any) {
  const version = resource.updatedAt ? `&v=${encodeURIComponent(resource.updatedAt)}` : ''
  return `/api/resources/${resource.id}/download?inline=1${version}`
}

function normalizeSavedMediaSources(savedTracks: Track[]) {
  const byDirectURL = new Map(resources.value.filter(r => r.videoUrl).map(r => [r.videoUrl, r]))
  for (const clip of savedTracks.flatMap(track => track.clips)) {
    if (clip.kind !== 'video' || !clip.src) continue
    const directResource = byDirectURL.get(clip.src)
    const idMatch = clip.src.match(/\/projects\/\d+\/resources\/(\d+)\.(?:mp4|webm|mov|m4v)(?:[?#]|$)/i)
    const resource = directResource || (idMatch ? resources.value.find(r => String(r.id) === idMatch[1]) : null)
    if (resource) clip.src = editorResourceVideoURL(resource)
  }
}

function snapshot() {
  const raw = JSON.stringify(tracks.value)
  if (history.value[historyIndex.value] === raw) return
  history.value = history.value.slice(0, historyIndex.value + 1)
  history.value.push(raw)
  if (history.value.length > 60) history.value.shift()
  historyIndex.value = history.value.length - 1
  scheduleSave()
}
function undo() { if (historyIndex.value > 0) { historyIndex.value--; tracks.value = JSON.parse(history.value[historyIndex.value]); scheduleSave() } }
function redo() { if (historyIndex.value < history.value.length - 1) { historyIndex.value++; tracks.value = JSON.parse(history.value[++historyIndex.value]); scheduleSave() } }

function trackFor(kind: TrackKind) {
  let track = tracks.value.find(t => t.kind === kind && !t.locked)
  if (!track) {
    track = { id: uid('track'), kind, name: `${kind} ${tracks.value.length + 1}`, visible: true, muted: false, locked: false, clips: [] }
    tracks.value.push(track)
  }
  return track
}
function addMedia(item: MediaItem, at = playhead.value) {
  const kind: TrackKind = item.kind === 'transition' || item.kind === 'effect' ? 'filter' : item.kind
  const track = trackFor(kind)
  if (track.locked) return
  const start = findAvailableStart(track, Math.max(0, at), item.duration)
  const clip: Clip = { id: uid('clip'), trackId: track.id, kind, name: item.name, src: item.src, start, duration: item.duration, trimStart: 0, volume: 1, opacity: 1, playbackRate: 1, filter: item.filter, effect: item.effect, transition: item.transition }
  if (kind === 'text') Object.assign(clip, { text: item.name === '字幕文字' ? '请输入字幕' : item.name, color: '#ffffff', fontSize: item.name === '标题文字' ? 64 : 38, x: 50, y: item.name === '标题文字' ? 20 : 82 })
  track.clips.push(clip); selectedClipId.value = clip.id; snapshot()
}
function findAvailableStart(track: Track, requested: number, duration: number) {
  let start = requested
  const ordered = [...track.clips].sort((a, b) => a.start - b.start)
  for (const clip of ordered) {
    if (start + duration <= clip.start) break
    if (start < clip.start + clip.duration && start + duration > clip.start) start = clip.start + clip.duration
  }
  return Math.round(start * 1000) / 1000
}
function autoArrangeShots() {
  const list = mediaItems.value.filter(i => i.kind === 'video' && i.id.startsWith('shot-'))
  if (!list.length) return ElMessage.warning('当前分集还没有已生成视频')
  const track = trackFor('video'); let cursor = 0; track.clips = []
  for (const item of list) { addMedia(item, cursor); cursor += item.duration }
  snapshot(); ElMessage.success(`已按顺序导入 ${list.length} 个分镜成片`)
}
function deleteSelected() {
  if (!selectedClip.value || selectedTrack.value?.locked) return
  selectedTrack.value!.clips = selectedTrack.value!.clips.filter(c => c.id !== selectedClipId.value); selectedClipId.value = ''; snapshot()
}
function splitSelected() {
  const c = selectedClip.value; const t = selectedTrack.value
  if (!c || !t || t.locked || playhead.value <= c.start || playhead.value >= c.start + c.duration) return
  const left = playhead.value - c.start; const right: Clip = { ...c, id: uid('clip'), start: playhead.value, duration: c.duration - left, trimStart: (c.trimStart || 0) + left * (c.playbackRate || 1) }
  c.duration = left; t.clips.push(right); selectedClipId.value = right.id; snapshot()
}
function addTrack(kind: TrackKind) { tracks.value.push({ id: uid('track'), kind, name: `新${kind}轨`, visible: true, muted: false, locked: false, clips: [] }); snapshot() }
function deleteTrack(track: Track) { if (tracks.value.length <= 1) return; tracks.value = tracks.value.filter(t => t.id !== track.id); snapshot() }

function effectAnimation(effect = '') {
  if (effect.includes('抖')) return 'shake'
  if (effect.includes('闪')) return 'flash'
  if (effect.includes('放大')) return 'zoom-in'
  if (effect.includes('缩小')) return 'zoom-out'
  if (effect.includes('脉冲')) return 'pulse'
  if (effect.includes('淡入')) return 'fade-in'
  if (effect.includes('淡出')) return 'fade-out'
  return 'none'
}

function transitionAnimation(transition = '') {
  if (transition.includes('滑')) return 'slide'
  if (transition.includes('擦')) return 'wipe'
  if (transition.includes('缩放')) return 'zoom-in'
  if (transition.includes('旋转')) return 'rotate'
  return 'fade-in'
}

function beginClipDrag(e: PointerEvent, clip: Clip, mode: 'move' | 'left' | 'right') {
  const track = tracks.value.find(t => t.id === clip.trackId)
  if (track?.locked) return
  e.preventDefault(); e.stopPropagation(); selectedClipId.value = clip.id
  if (mode === 'move') {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const offset = Math.max(0, Math.min(clip.duration - .001, (e.clientX - rect.left) / zoom.value))
    playhead.value = clip.start + offset
    syncMedia()
  }
  const startX = e.clientX
  const original = { start: clip.start, duration: clip.duration, trimStart: clip.trimStart || 0 }
  const move = (ev: PointerEvent) => {
    const delta = (ev.clientX - startX) / zoom.value
    if (mode === 'move') clip.start = snapClipTime(Math.max(0, original.start + delta), clip)
    if (mode === 'right') clip.duration = Math.max(.1, original.duration + delta)
    if (mode === 'left') {
      const nextStart = Math.max(0, Math.min(original.start + original.duration - .1, original.start + delta))
      const changed = nextStart - original.start
      clip.start = nextStart; clip.duration = original.duration - changed
      clip.trimStart = Math.max(0, original.trimStart + changed * (clip.playbackRate || 1))
    }
  }
  const up = () => { window.removeEventListener('pointermove', move); scheduleSave() }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up, { once: true })
}

function snapClipTime(value: number, clip: Clip) {
  const threshold = 8 / zoom.value
  const candidates = [0, playhead.value]
  for (const other of tracks.value.flatMap(track => track.clips)) {
    if (other.id === clip.id) continue
    candidates.push(other.start, other.start + other.duration, other.start - clip.duration, other.start + other.duration - clip.duration)
  }
  let nearest = value; let distance = threshold
  for (const candidate of candidates) {
    const nextDistance = Math.abs(candidate - value)
    if (candidate >= 0 && nextDistance < distance) { nearest = candidate; distance = nextDistance }
  }
  return Math.round(nearest * 1000) / 1000
}

function beginTimelineResize(e: PointerEvent) {
  e.preventDefault()
  const startY = e.clientY; const startHeight = timelineHeight.value
  const move = (ev: PointerEvent) => {
    timelineHeight.value = Math.round(Math.max(190, Math.min(window.innerHeight * .68, startHeight + startY - ev.clientY)))
  }
  const up = () => { window.removeEventListener('pointermove', move); snapshot() }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up, { once: true })
}

function onEditorKeydown(e: KeyboardEvent) {
  const target = e.target as HTMLElement | null
  if (target?.matches('input, textarea, [contenteditable="true"]')) return
  if (e.code === 'Space') { e.preventDefault(); void togglePlay() }
  if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); deleteSelected() }
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z') { e.preventDefault(); e.shiftKey ? redo() : undo() }
}

function setPlayheadFromPointer(e: PointerEvent, el: HTMLElement) {
  const rect = el.getBoundingClientRect()
  playhead.value = Math.max(0, Math.min(totalDuration.value, (e.clientX - rect.left) / zoom.value))
  syncMedia()
}
function beginTimelineScrub(e: PointerEvent) {
  if (e.button !== 0) return
  const el = e.currentTarget as HTMLElement
  e.preventDefault(); setPlayheadFromPointer(e, el)
  const move = (ev: PointerEvent) => setPlayheadFromPointer(ev, el)
  const up = () => window.removeEventListener('pointermove', move)
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up, { once: true })
}
function dropMediaOnTimeline(e: DragEvent) {
  const raw = e.dataTransfer?.getData('application/json') || ''
  if (!raw) return
  try {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    addMedia(JSON.parse(raw), Math.max(0, (e.clientX - rect.left) / zoom.value))
  } catch { ElMessage.warning('无法识别拖入的素材') }
}
function fitTimeline() {
  const available = Math.max(320, timelineAreaWidth.value - 196)
  zoom.value = Math.max(30, Math.min(180, available / Math.max(totalDuration.value, 1)))
  timelineScrollEl.value?.scrollTo({ left: 0, behavior: 'smooth' }); scheduleSave()
}
function changeTimelineZoom(delta: number) {
  zoom.value = Math.max(30, Math.min(180, zoom.value + delta)); scheduleSave()
}
async function handleTimelineWheel(e: WheelEvent) {
  const scroller = timelineScrollEl.value
  if (!scroller) return
  if (e.ctrlKey || e.metaKey) {
    e.preventDefault()
    const rect = scroller.getBoundingClientRect()
    const pointerX = e.clientX - rect.left
    const contentX = Math.max(0, scroller.scrollLeft + pointerX - 180)
    const anchorTime = contentX / zoom.value
    const oldZoom = zoom.value
    zoom.value = Math.max(30, Math.min(180, zoom.value + (e.deltaY < 0 ? 12 : -12)))
    if (zoom.value !== oldZoom) {
      await nextTick()
      scroller.scrollLeft = Math.max(0, anchorTime * zoom.value - pointerX + 180)
      scheduleSave()
    }
    return
  }
  if (Math.abs(e.deltaX) > Math.abs(e.deltaY) || e.shiftKey) {
    e.preventDefault(); scroller.scrollLeft += e.deltaX || e.deltaY
  }
}
function syncMedia() {
  const visual = activeVisual.value
  if (videoEl.value && visual?.kind === 'video') {
    const rate = visual.playbackRate || 1
    const wanted = (visual.trimStart || 0) + (playhead.value - visual.start) * rate
    if (Math.abs(videoEl.value.currentTime - wanted) > .25) videoEl.value.currentTime = Math.max(0, wanted)
    videoEl.value.playbackRate = rate
    videoEl.value.volume = tracks.value.find(t => t.id === visual.trackId)?.muted ? 0 : (visual.volume ?? 1)
  }
  const audio = activeAudio.value
  if (audioEl.value && audio) {
    const rate = audio.playbackRate || 1
    const wanted = (audio.trimStart || 0) + (playhead.value - audio.start) * rate
    if (Math.abs(audioEl.value.currentTime - wanted) > .25) audioEl.value.currentTime = Math.max(0, wanted)
    audioEl.value.playbackRate = rate
    audioEl.value.volume = audio.volume ?? 1
  }
}
function describeMediaError(el: HTMLMediaElement) {
  const code = el.error?.code
  if (code === MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED) return '浏览器不支持该视频编码，建议使用 H.264 + AAC 的 MP4'
  if (code === MediaError.MEDIA_ERR_NETWORK) return '视频加载失败，请检查素材地址或网络'
  if (code === MediaError.MEDIA_ERR_DECODE) return '视频解码失败，素材文件可能损坏或编码不兼容'
  return '视频暂时无法加载'
}
function onPreviewLoadStart() { mediaLoading.value = true; mediaError.value = '' }
function onPreviewLoaded(e: Event) {
  mediaLoading.value = false; mediaError.value = ''
  const el = e.currentTarget as HTMLVideoElement
  if (el === videoEl.value) syncMedia()
}
function onPreviewError(e: Event) {
  mediaSwitchToken++
  mediaLoading.value = false
  mediaError.value = describeMediaError(e.currentTarget as HTMLVideoElement)
  playing.value = false; stopElement(e.currentTarget as HTMLVideoElement); pauseMedia()
}
async function waitUntilPlayable(el: HTMLMediaElement) {
  if (el.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) return
  await new Promise<void>((resolve, reject) => {
    const finish = (error?: Error) => { clearTimeout(timer); el.removeEventListener('canplay', ready); el.removeEventListener('error', failed); error ? reject(error) : resolve() }
    const ready = () => finish()
    const failed = () => finish(new Error(describeMediaError(el)))
    const timer = window.setTimeout(() => finish(new Error('视频加载超时')), 10000)
    el.addEventListener('canplay', ready, { once: true }); el.addEventListener('error', failed, { once: true }); el.load()
  })
}
async function playMedia(el: HTMLMediaElement | null) {
  if (!el) return
  await waitUntilPlayable(el)
  await el.play()
}
function onPlaybackFailure(e: any) {
  playing.value = false; stopElement(videoEl.value); stopElement(audioEl.value); pauseMedia(); mediaLoading.value = false
  mediaError.value = e?.message || '媒体播放失败'
}
function tick(ts: number) {
  if (!playing.value) return
  const visual = activeVisual.value
  const video = videoEl.value
  if (visual?.kind === 'video' && video && !video.paused && video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) {
    playhead.value = visual.start + Math.max(0, video.currentTime - (visual.trimStart || 0)) / (visual.playbackRate || 1)
    lastTick = ts
  } else {
    if (!lastTick) lastTick = ts
    playhead.value += (ts - lastTick) / 1000; lastTick = ts
  }
  if (playhead.value >= totalDuration.value) { playhead.value = 0; playing.value = false; pauseMedia(); return }
  syncMedia(); raf = requestAnimationFrame(tick)
}
function stopElement(el: HTMLMediaElement | null) {
  if (!el) return
  el.pause()
  el.muted = true
}
function pauseMedia() { videoEl.value?.pause(); audioEl.value?.pause(); cancelAnimationFrame(raf); lastTick = 0 }
async function resumeCurrentMedia(token = ++mediaSwitchToken) {
  await nextTick()
  if (!playing.value || token !== mediaSwitchToken) return
  syncMedia()
  await Promise.all([playMedia(videoEl.value), playMedia(audioEl.value)])
  if (!playing.value || token !== mediaSwitchToken) {
    stopElement(videoEl.value); stopElement(audioEl.value)
    return
  }
  if (videoEl.value) videoEl.value.muted = false
  if (audioEl.value) audioEl.value.muted = false
  cancelAnimationFrame(raf); lastTick = 0
  raf = requestAnimationFrame(tick)
}
async function togglePlay() {
  if (playing.value) { playing.value = false; mediaSwitchToken++; return pauseMedia() }
  mediaError.value = ''; playing.value = true
  try {
    await resumeCurrentMedia()
  } catch (e: any) {
    onPlaybackFailure(e)
    ElMessage.error(mediaError.value)
  }
}

function scheduleSave() { clearTimeout(saveTimer); saveTimer = window.setTimeout(() => void save(), 700) }
async function save() {
  if (!episodeId.value) return
  saving.value = true
  try { await api(`/episodes/${episodeId.value}/editor`, { method: 'PUT', body: JSON.stringify({ data: { tracks: tracks.value, ratio: project.value?.videoRatio || '16:9', zoom: zoom.value, timelineHeight: timelineHeight.value } }) }) }
  catch (e: any) { ElMessage.error(e.message || '保存剪辑工程失败') }
  finally { saving.value = false }
}

async function resetProject() {
  try { await ElMessageBox.confirm('将清空当前时间轴，但不会删除素材和成片。', '重置剪辑工程', { type: 'warning' }); tracks.value = defaultTracks(); selectedClipId.value = ''; snapshot() } catch {}
}

function drawCover(ctx: CanvasRenderingContext2D, source: HTMLVideoElement | HTMLImageElement, w: number, h: number) {
  const sw = source instanceof HTMLVideoElement ? source.videoWidth : source.naturalWidth; const sh = source instanceof HTMLVideoElement ? source.videoHeight : source.naturalHeight
  if (!sw || !sh) return
  const scale = Math.max(w / sw, h / sh); const dw = sw * scale; const dh = sh * scale
  ctx.drawImage(source, (w - dw) / 2, (h - dh) / 2, dw, dh)
}
async function exportVideo() {
  if (exporting.value) return
  const videoClips = tracks.value.flatMap(t => t.clips).filter(c => c.kind === 'video' || c.kind === 'image')
  if (!videoClips.length) return ElMessage.warning('时间轴没有可导出的画面')
  exporting.value = true; playing.value = false; pauseMedia()
  try {
    const ratio = project.value?.videoRatio || '16:9'; const [rw, rh] = ratio.split(':').map(Number); const w = rw < rh ? 720 : 1280; const h = Math.round(w * rh / rw)
    const canvas = document.createElement('canvas'); canvas.width = w; canvas.height = h; const ctx = canvas.getContext('2d')!
    const stream = canvas.captureStream(30)
    const audioContext = new AudioContext()
    const audioDestination = audioContext.createMediaStreamDestination()
    audioDestination.stream.getAudioTracks().forEach(track => stream.addTrack(track))
    const connectedMedia = new WeakSet<HTMLMediaElement>()
    const connectAudio = (el: HTMLMediaElement | null) => {
      if (!el || connectedMedia.has(el)) return
      try { audioContext.createMediaElementSource(el).connect(audioDestination); connectedMedia.add(el) } catch { /* media may already be connected */ }
    }
    const recorder = new MediaRecorder(stream, { mimeType: MediaRecorder.isTypeSupported('video/webm;codecs=vp9,opus') ? 'video/webm;codecs=vp9,opus' : 'video/webm' }); const chunks: Blob[] = []
    recorder.ondataavailable = e => e.data.size && chunks.push(e.data); recorder.start(1000)
    const start = performance.now(); const duration = totalDuration.value
    await new Promise<void>((resolve, reject) => {
      const render = async () => {
        const t = (performance.now() - start) / 1000; playhead.value = Math.min(t, duration); await nextTick(); syncMedia(); connectAudio(videoEl.value); connectAudio(audioEl.value); videoEl.value?.play().catch(() => {}); audioEl.value?.play().catch(() => {})
        ctx.save(); ctx.fillStyle = '#000'; ctx.fillRect(0, 0, w, h); ctx.filter = activeFilter.value?.filter || 'none'
        const visual = activeVisual.value
        try {
          if (visual?.kind === 'video' && videoEl.value) drawCover(ctx, videoEl.value, w, h)
          else if (visual?.kind === 'image' && visual.src) { const img = new Image(); img.crossOrigin = 'anonymous'; img.src = visual.src; await img.decode(); drawCover(ctx, img, w, h) }
        } catch (e) { reject(new Error('素材跨域限制导致无法导出，请改用同源素材')); return }
        ctx.restore()
        for (const text of activeTexts.value) { ctx.fillStyle = text.color || '#fff'; ctx.font = `700 ${text.fontSize || 38}px sans-serif`; ctx.textAlign = 'center'; ctx.strokeStyle = 'rgba(0,0,0,.75)'; ctx.lineWidth = 6; const x = w * ((text.x ?? 50) / 100); const y = h * ((text.y ?? 82) / 100); ctx.strokeText(text.text || '', x, y); ctx.fillText(text.text || '', x, y) }
        if (t >= duration) resolve(); else requestAnimationFrame(render)
      }; void render()
    })
    recorder.stop(); await new Promise(r => recorder.onstop = r); await audioContext.close(); const blob = new Blob(chunks, { type: 'video/webm' }); const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = `${project.value?.title || 'Novaly'}-${episode.value?.title || '剪辑'}.webm`; a.click(); setTimeout(() => URL.revokeObjectURL(a.href), 1000)
    ElMessage.success('剪辑视频已导出')
  } catch (e: any) { ElMessage.error(e.message || '导出失败') }
  finally { exporting.value = false; playhead.value = 0 }
}

async function load() {
  loading.value = true
  try {
    const data = await api(`/episodes/${episodeId.value}/editor`); project.value = data.project; episode.value = data.episode; shots.value = data.shots || []; resources.value = data.resources || []
    tracks.value = data.edit?.tracks?.length ? data.edit.tracks : defaultTracks(); normalizeSavedMediaSources(tracks.value); zoom.value = data.edit?.zoom || 72; timelineHeight.value = data.edit?.timelineHeight || 320; history.value = [JSON.stringify(tracks.value)]; historyIndex.value = 0
  } catch (e: any) { ElMessage.error(e.message || '加载剪辑台失败') }
  finally {
    loading.value = false
    await nextTick()
    document.querySelector<HTMLElement>('.novaly-main')?.scrollTo({ top: 0, left: 0 })
  }
}

watch([activeVisual, activeAudio], async ([nextVisual, nextAudio], [previousVisual, previousAudio]) => {
  const oldVideo = videoEl.value
  const oldAudio = audioEl.value
  const shouldResume = playing.value
  const token = ++mediaSwitchToken

  // Vue 会在下一次渲染时替换媒体节点；替换前必须先停掉旧节点，
  // 否则新分镜加载期间旧节点仍可能在后台输出声音。
  if (previousVisual?.id !== nextVisual?.id) stopElement(oldVideo)
  if (previousAudio?.id !== nextAudio?.id) stopElement(oldAudio)
  cancelAnimationFrame(raf); lastTick = 0
  mediaError.value = ''
  mediaLoading.value = nextVisual?.kind === 'video'

  await nextTick()
  syncMedia()
  if (!shouldResume || token !== mediaSwitchToken) return
  try {
    await resumeCurrentMedia(token)
  } catch (e: any) {
    if (token !== mediaSwitchToken) return
    onPlaybackFailure(e)
    ElMessage.error(mediaError.value)
  }
})
onMounted(() => {
  void load()
  window.addEventListener('keydown', onEditorKeydown)
  if (previewSectionEl.value) {
    previewResizeObserver = new ResizeObserver(([entry]) => {
      if (!entry) return
      previewArea.width = entry.contentRect.width; previewArea.height = entry.contentRect.height
    })
    previewResizeObserver.observe(previewSectionEl.value)
  }
  if (timelineScrollEl.value) {
    timelineResizeObserver = new ResizeObserver(([entry]) => { if (entry) timelineAreaWidth.value = entry.contentRect.width })
    timelineResizeObserver.observe(timelineScrollEl.value)
  }
})
onBeforeUnmount(() => { pauseMedia(); clearTimeout(saveTimer); previewResizeObserver?.disconnect(); timelineResizeObserver?.disconnect(); window.removeEventListener('keydown', onEditorKeydown) })
</script>

<template>
  <div v-loading="loading" class="editor-page" :style="editorGridStyle">
    <header class="editor-head">
      <div><el-button text @click="router.back()">← 返回</el-button><b>剪辑台</b><span>{{ project?.title }} · {{ episode?.title }}</span></div>
      <div class="editor-actions"><small>{{ saving ? '保存中…' : '已自动保存' }}</small><el-button @click="autoArrangeShots">导入全部分镜成片</el-button><el-button @click="save">保存工程</el-button><el-button type="danger" :loading="exporting" @click="exportVideo">{{ exporting ? '渲染中…' : '导出视频' }}</el-button></div>
    </header>
    <main class="editor-main">
      <aside class="media-library">
        <div class="media-tabs"><button v-for="tab in ([['video','视频'],['image','图片'],['audio','音频'],['text','文字'],['transition','转场'],['effect','特效'],['filter','滤镜']] as const)" :key="tab[0]" :class="{active:libraryTab===tab[0]}" @click="libraryTab=tab[0]">{{ tab[1] }}</button></div>
        <el-input v-model="search" clearable placeholder="搜索素材" />
        <div class="media-grid">
          <button v-for="item in mediaItems" :key="item.id" class="media-card" draggable="true" @dragstart="$event.dataTransfer?.setData('application/json', JSON.stringify(item))" @dblclick="addMedia(item)">
            <span v-if="item.kind==='video' && item.src" class="media-video-placeholder"><i>▶</i><small>视频素材</small></span>
            <img v-else-if="item.kind==='image' && item.src" :src="item.src" />
            <span v-else class="media-icon">{{ item.kind==='audio'?'♫':item.kind==='text'?'T':item.kind==='filter'?'◐':'✦' }}</span>
            <b>{{ item.name }}</b><small>双击加入</small>
          </button>
          <el-empty v-if="!mediaItems.length" description="暂无素材" :image-size="60" />
        </div>
      </aside>
      <section ref="previewSectionEl" class="preview-section">
        <div class="preview-tools"><button :class="{active:previewFit==='contain'}" @click="previewFit='contain'">适应</button><button :class="{active:previewFit==='cover'}" @click="previewFit='cover'">填充</button></div>
        <div class="preview-canvas" :style="{...previewCanvasSize,aspectRatio:previewRatio}" @click="togglePlay">
          <video v-if="activeVisual?.kind==='video'" ref="videoEl" :key="activeVisual.id" :src="activeVisual.src" :style="{...previewMediaStyle,objectFit:previewFit}" playsinline preload="auto" @loadstart="onPreviewLoadStart" @loadeddata="onPreviewLoaded" @canplay="onPreviewLoaded" @error="onPreviewError" />
          <img v-else-if="activeVisual?.kind==='image'" :src="activeVisual.src" :style="{...previewMediaStyle,objectFit:previewFit}" />
          <div v-else class="preview-empty">把视频或图片拖入时间轴</div>
          <div v-if="mediaLoading && !mediaError" class="preview-status"><span class="loading-dot" />正在加载视频…</div>
          <div v-if="mediaError" class="preview-status error" @click.stop><b>无法播放</b><span>{{ mediaError }}</span><el-button size="small" @click="videoEl?.load()">重新加载</el-button></div>
          <button v-if="activeVisual && !playing && !mediaLoading && !mediaError" class="preview-play-button" type="button" aria-label="播放" @click.stop="togglePlay">▶</button>
          <div v-for="text in activeTexts" :key="text.id" class="preview-text" :style="previewTextStyle(text)"><span>{{ text.text || '请输入文字' }}</span></div>
          <audio v-if="activeAudio" ref="audioEl" :key="activeAudio.id" :src="activeAudio.src" />
        </div>
        <div class="transport"><el-button circle @click="playhead=0;syncMedia()">↺</el-button><el-button type="primary" circle @click="togglePlay">{{ playing?'Ⅱ':'▶' }}</el-button><span>{{ playhead.toFixed(2) }} / {{ totalDuration.toFixed(2) }} 秒</span><input v-model.number="playhead" type="range" min="0" :max="totalDuration" step=".01" @input="syncMedia" /></div>
      </section>
      <aside class="property-panel">
        <h3>属性</h3><div v-if="selectedClip" class="property-form">
          <label>名称<el-input v-model="selectedClip.name" @change="snapshot" /></label>
          <label>开始时间<el-input-number v-model="selectedClip.start" :min="0" :step=".1" @change="snapshot" /></label>
          <label>持续时间<el-input-number v-model="selectedClip.duration" :min=".1" :step=".1" @change="snapshot" /></label>
          <label v-if="selectedClip.kind==='video'||selectedClip.kind==='audio'">音量<el-slider v-model="selectedClip.volume" :min="0" :max="1" :step=".05" @change="snapshot" /></label>
          <label v-if="selectedClip.kind==='video'||selectedClip.kind==='image'">透明度<el-slider v-model="selectedClip.opacity" :min="0" :max="1" :step=".05" @change="snapshot" /></label>
          <label v-if="selectedClip.kind==='video'">播放速度<el-input-number v-model="selectedClip.playbackRate" :min=".25" :max="4" :step=".25" @change="snapshot" /></label>
          <template v-if="selectedClip.kind==='text'"><label>文字<el-input v-model="selectedClip.text" type="textarea" @change="snapshot" /></label><label>字号<el-slider v-model="selectedClip.fontSize" :min="16" :max="120" @change="snapshot" /></label><label>横向位置<el-slider v-model="selectedClip.x" :min="0" :max="100" @change="snapshot" /></label><label>纵向位置<el-slider v-model="selectedClip.y" :min="0" :max="100" @change="snapshot" /></label><label>颜色<el-color-picker v-model="selectedClip.color" @change="snapshot" /></label></template>
          <el-button type="danger" plain @click="deleteSelected">删除片段</el-button>
        </div><el-empty v-else description="选择时间轴片段后编辑" :image-size="60" />
      </aside>
    </main>
    <div class="pane-splitter" title="上下拖动调整时间轴高度" @pointerdown="beginTimelineResize"><i /></div>
    <section class="timeline-section">
      <div class="timeline-toolbar"><el-button @click="resetProject">重置</el-button><el-button :disabled="historyIndex<=0" @click="undo">撤销</el-button><el-button :disabled="historyIndex>=history.length-1" @click="redo">重做</el-button><el-button :disabled="!selectedClip" @click="splitSelected">在游标处拆分</el-button><el-dropdown @command="addTrack"><el-button>添加轨道⌄</el-button><template #dropdown><el-dropdown-menu><el-dropdown-item v-for="k in ['video','image','audio','text','filter']" :key="k" :command="k">{{ k }}</el-dropdown-item></el-dropdown-menu></template></el-dropdown><span class="timeline-time">{{ playhead.toFixed(2) }}s / {{ totalDuration.toFixed(2) }}s</span><span class="zoom-control"><button title="缩小" @click="changeTimelineZoom(-15)">−</button><button class="fit" title="适应全部" @click="fitTimeline">适应全部</button><el-slider v-model="zoom" :min="30" :max="180" :show-tooltip="false" @change="scheduleSave" /><button title="放大" @click="changeTimelineZoom(15)">＋</button></span></div>
      <div ref="timelineScrollEl" class="timeline-scroll" @wheel="handleTimelineWheel">
        <div class="ruler-row"><div class="track-head">轨道</div><div class="ruler" :style="{width:timelineContentWidth+'px'}" @pointerdown="beginTimelineScrub"><span v-for="n in Math.ceil(totalDuration)+1" :key="n" :style="{left:(n-1)*zoom+'px'}">{{ n-1 }}s</span><i class="ruler-playhead" :style="{left:playhead*zoom+'px'}" /></div></div>
        <div v-for="track in tracks" :key="track.id" class="track-row"><div class="track-head"><b>{{ track.name }}</b><div><button @click="track.visible=!track.visible;snapshot()">{{ track.visible?'◉':'○' }}</button><button @click="track.muted=!track.muted;snapshot()">{{ track.muted?'🔇':'♫' }}</button><button @click="track.locked=!track.locked;snapshot()">{{ track.locked?'🔒':'🔓' }}</button><button @click="deleteTrack(track)">×</button></div></div><div class="track-lane" :class="{locked:track.locked}" :style="{width:timelineContentWidth+'px'}" @pointerdown.self="beginTimelineScrub" @dragover.prevent @drop="dropMediaOnTimeline"><button v-for="clip in track.clips" :key="clip.id" class="timeline-clip" :class="[clip.kind,{selected:selectedClipId===clip.id,current:playhead>=clip.start&&playhead<clip.start+clip.duration}]" :style="{left:clip.start*zoom+'px',width:Math.max(20,clip.duration*zoom)+'px',opacity:clip.opacity??1}" @pointerdown="beginClipDrag($event,clip,'move')"><i class="resize-handle left" @pointerdown="beginClipDrag($event,clip,'left')"/><span>{{ clip.name }}</span><small>{{ clip.duration.toFixed(1) }}s</small><i class="resize-handle right" @pointerdown="beginClipDrag($event,clip,'right')"/></button><i class="playhead" :style="{left:playhead*zoom+'px'}" /></div></div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.editor-page{height:calc(100vh - 72px);display:grid;grid-template-rows:58px minmax(300px,1fr) 330px;background:#0f0e0d;color:#e8e2da;overflow:hidden}.editor-head{display:flex;align-items:center;justify-content:space-between;padding:0 18px;border-bottom:1px solid #302c28;background:#171513}.editor-head>div{display:flex;align-items:center;gap:12px}.editor-head span,.editor-head small{color:#928a81;font-size:12px}.editor-main{display:grid;grid-template-columns:270px minmax(360px,1fr) 260px;min-height:0}.media-library,.property-panel{padding:12px;border-right:1px solid #302c28;overflow:auto;background:#151311}.property-panel{border-right:0;border-left:1px solid #302c28}.media-tabs{display:grid;grid-template-columns:repeat(4,1fr);gap:4px;margin-bottom:10px}.media-tabs button{border:0;border-radius:5px;padding:6px 2px;background:#25211e;color:#aaa29a;font-size:12px;cursor:pointer}.media-tabs button.active{background:#ff7658;color:#24120d}.media-grid{display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:10px}.media-card{min-width:0;min-height:92px;padding:5px;border:1px solid #38332e;border-radius:7px;background:#201d1a;color:#ddd6cd;display:flex;flex-direction:column;gap:3px;align-items:stretch;cursor:grab;text-align:left}.media-card video,.media-card img{width:100%;height:58px;object-fit:cover;border-radius:4px}.media-card b{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px}.media-card small{font-size:10px;color:#7f776f}.media-icon{height:58px;display:grid;place-items:center;font-size:28px;background:#2b2723;border-radius:4px}.preview-section{min-width:0;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:14px;background:#090909}.preview-canvas{position:relative;max-width:100%;max-height:calc(100% - 52px);width:min(74%,900px);background:#000;overflow:hidden;box-shadow:0 0 0 1px #333}.preview-canvas video,.preview-canvas>img{width:100%;height:100%;object-fit:cover}.preview-empty{height:100%;display:grid;place-items:center;color:#5f5953}.preview-text{position:absolute;transform:translate(-50%,-50%);font-weight:700;text-shadow:0 2px 4px #000;white-space:pre-wrap;text-align:center}.transport{width:min(90%,900px);display:flex;align-items:center;gap:10px;margin-top:10px}.transport input{flex:1}.property-form{display:grid;gap:14px}.property-form label{display:grid;gap:5px;color:#a69e95;font-size:12px}.timeline-section{min-height:0;border-top:1px solid #302c28;background:#12100f;display:flex;flex-direction:column}.timeline-toolbar{height:44px;padding:6px 12px;display:flex;align-items:center;gap:7px;border-bottom:1px solid #302c28}.zoom-control{margin-left:auto;width:180px;display:flex;align-items:center;gap:8px;font-size:12px}.zoom-control .el-slider{flex:1}.timeline-scroll{overflow:auto;flex:1}.ruler-row,.track-row{display:flex;min-width:max-content}.track-head{position:sticky;left:0;z-index:4;width:180px;min-width:180px;padding:6px 10px;background:#1c1916;border-right:1px solid #38332e;border-bottom:1px solid #2c2824}.track-head b{font-size:12px}.track-head>div{display:flex;gap:3px;margin-top:5px}.track-head button{border:0;background:#2a2622;color:#aaa;padding:2px 5px;border-radius:3px}.ruler{height:30px;position:relative;background:#181512;border-bottom:1px solid #302c28}.ruler span{position:absolute;font-size:9px;color:#716a63;border-left:1px solid #39342e;height:100%;padding-left:3px}.ruler i,.playhead{position:absolute;top:0;bottom:0;width:1px;background:#ff7658;z-index:3}.track-lane{height:62px;position:relative;background:repeating-linear-gradient(90deg,#151311 0,#151311 71px,#211e1b 72px);border-bottom:1px solid #292521}.track-lane.locked{opacity:.55}.timeline-clip{position:absolute;top:6px;height:48px;border:1px solid #675348;border-radius:5px;background:#49352d;color:#f1e9df;padding:4px 7px;text-align:left;overflow:hidden;cursor:grab}.timeline-clip.video{background:#433263}.timeline-clip.audio{background:#24483f}.timeline-clip.image{background:#315047}.timeline-clip.text{background:#5a4426}.timeline-clip.filter{background:#3a3a54}.timeline-clip.selected{outline:2px solid #ff7658}.timeline-clip span,.timeline-clip small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.timeline-clip small{font-size:9px;opacity:.7}.resize-handle{position:absolute;top:0;bottom:0;width:7px;z-index:4;cursor:ew-resize}.resize-handle.left{left:0}.resize-handle.right{right:0}@keyframes editor-shake{from{transform:translateX(-3px)}to{transform:translateX(3px)}}@keyframes editor-flash{from{filter:brightness(1)}to{filter:brightness(1.9)}}@keyframes editor-zoom-in{from{transform:scale(1)}to{transform:scale(1.08)}}@keyframes editor-zoom-out{from{transform:scale(1.08)}to{transform:scale(1)}}@keyframes editor-pulse{from{opacity:.75}to{opacity:1}}@keyframes editor-fade-in{from{opacity:.3}to{opacity:1}}@keyframes editor-fade-out{from{opacity:1}to{opacity:.3}}@keyframes editor-none{from{opacity:1}to{opacity:1}}@media(max-width:1000px){.editor-main{grid-template-columns:220px 1fr}.property-panel{display:none}.editor-page{grid-template-rows:58px 1fr 280px}}
@keyframes editor-slide{from{transform:translateX(22%);opacity:.2}to{transform:translateX(0);opacity:1}}
@keyframes editor-wipe{from{clip-path:inset(0 100% 0 0)}to{clip-path:inset(0 0 0 0)}}
@keyframes editor-rotate{from{transform:rotate(-5deg) scale(.92);opacity:.2}to{transform:rotate(0) scale(1);opacity:1}}
.editor-page{height:calc(100dvh - 57px);min-height:620px;grid-template-rows:58px minmax(250px,1fr) minmax(230px,36vh)}
.editor-head{position:relative;z-index:8;min-width:0}
.editor-head>div{min-width:0}
.editor-head span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.editor-actions{flex-shrink:0}
.editor-main{grid-template-columns:minmax(220px,270px) minmax(420px,1fr) minmax(230px,260px);overflow:hidden}
.preview-section{position:relative;min-height:0;padding-bottom:58px}
.preview-canvas{flex:0 0 auto;width:auto;height:auto;max-width:none;max-height:none;cursor:pointer}
.preview-canvas video,.preview-canvas>img{object-fit:contain}
.preview-tools{position:absolute;right:16px;top:12px;z-index:10;display:flex;padding:3px;border:1px solid #38332e;border-radius:7px;background:rgba(23,21,19,.9)}.preview-tools button{border:0;border-radius:5px;padding:5px 10px;background:transparent;color:#8f877e;font-size:11px;cursor:pointer}.preview-tools button.active{background:#40342e;color:#fff}.preview-tools button:hover{color:#ff8064}
.preview-play-button{position:absolute;left:50%;top:50%;z-index:7;display:grid;place-items:center;width:58px;height:58px;padding:0 0 0 4px;transform:translate(-50%,-50%);border:1px solid rgba(255,255,255,.5);border-radius:50%;background:rgba(8,8,8,.68);box-shadow:0 8px 30px rgba(0,0,0,.42);color:#fff;font-size:23px;cursor:pointer;transition:.16s ease}.preview-play-button:hover{transform:translate(-50%,-50%) scale(1.08);border-color:#ff8064;background:rgba(255,118,88,.82)}
.transport{position:absolute;left:50%;bottom:10px;z-index:9;transform:translateX(-50%);width:min(calc(100% - 40px),900px);margin:0;padding:6px 10px;border:1px solid #302c28;border-radius:9px;background:rgba(23,21,19,.94);box-shadow:0 5px 18px rgba(0,0,0,.35)}
.preview-status{position:absolute;inset:0;z-index:6;display:flex;align-items:center;justify-content:center;gap:10px;background:rgba(0,0,0,.68);color:#d8d0c7;font-size:13px}
.preview-status.error{flex-direction:column;text-align:center;padding:24px;color:#e8e2da}
.preview-status.error b{color:#ff8064;font-size:16px}
.loading-dot{width:16px;height:16px;border:2px solid #625951;border-top-color:#ff7658;border-radius:50%;animation:editor-spin .8s linear infinite}
.media-video-placeholder{height:58px;display:grid;place-items:center;align-content:center;gap:4px;border-radius:4px;background:linear-gradient(145deg,#29231f,#151311);color:#ff8064}.media-video-placeholder i{display:grid;place-items:center;width:25px;height:25px;padding-left:2px;border:1px solid #665148;border-radius:50%;font-size:10px;font-style:normal}.media-video-placeholder small{color:#766e66;font-size:9px}
.timeline-toolbar{flex-wrap:nowrap;overflow-x:auto;flex-shrink:0}
.timeline-time{margin-left:auto;color:#aaa198;font:12px ui-monospace,SFMono-Regular,Menlo,monospace;white-space:nowrap}
.zoom-control{margin-left:8px;width:280px;min-width:280px}.zoom-control button{display:grid;place-items:center;min-width:28px;height:28px;padding:0;border:1px solid #3b3530;border-radius:5px;background:#24201d;color:#c3bab1;cursor:pointer}.zoom-control button:hover{border-color:#ff7658;color:#ff8064}.zoom-control button.fit{width:auto;padding:0 9px;font-size:11px;white-space:nowrap}
.ruler{cursor:col-resize;user-select:none}.ruler-playhead::before{content:"";position:absolute;top:0;left:50%;width:10px;height:10px;transform:translate(-50%,-2px) rotate(45deg);border-radius:2px;background:#ff7658;box-shadow:0 1px 5px rgba(0,0,0,.45)}
.track-lane{cursor:text}.timeline-clip{touch-action:none}.resize-handle::after{content:"";position:absolute;top:14px;bottom:14px;width:2px;border-radius:2px;background:rgba(255,255,255,.48);opacity:0}.timeline-clip:hover .resize-handle::after,.timeline-clip.selected .resize-handle::after{opacity:1}.resize-handle.left::after{left:2px}.resize-handle.right::after{right:2px}
.preview-text{z-index:8;max-width:88%;line-height:1.35;text-shadow:0 1px 2px #000,0 0 5px #000;pointer-events:none}.preview-text span{display:inline-block;max-width:100%;padding:.12em .42em;border-radius:.28em;background:rgba(0,0,0,.26);overflow-wrap:anywhere;box-shadow:0 1px 4px rgba(0,0,0,.2)}
.timeline-clip.current{box-shadow:inset 0 0 0 1px rgba(105,190,255,.9),0 0 8px rgba(74,160,225,.25)}.timeline-clip.selected{outline:2px solid #ff7658;outline-offset:-1px}.timeline-clip.current.selected{box-shadow:inset 0 0 0 1px rgba(255,209,196,.9),0 0 10px rgba(255,118,88,.3)}
.editor-page{width:100%;max-width:100vw;min-width:0;box-sizing:border-box}
.editor-head,.editor-main,.preview-section,.property-panel,.timeline-section,.timeline-scroll{min-width:0;max-width:100%}
.editor-head{overflow:hidden}.editor-main{width:100%;grid-template-columns:minmax(190px,240px) minmax(0,1fr) minmax(220px,260px)}
.media-library,.preview-section,.property-panel{min-height:0;height:100%;box-sizing:border-box}.preview-section{overflow:hidden}.property-panel{position:relative;z-index:6;display:block}
.timeline-section{width:100%;overflow:hidden}.timeline-scroll{width:100%;overscroll-behavior:contain}
.timeline-toolbar{width:100%;max-width:100%;box-sizing:border-box;overflow:hidden}.timeline-toolbar>.zoom-control{position:sticky;right:0;z-index:8;flex-shrink:0;padding-left:10px;background:#12100f}.timeline-time{flex-shrink:0}
.pane-splitter{position:relative;z-index:12;display:grid;place-items:center;height:7px;background:#181512;cursor:row-resize;border-top:1px solid #302c28;border-bottom:1px solid #302c28}.pane-splitter i{width:44px;height:3px;border-radius:3px;background:#554b43;transition:.15s}.pane-splitter:hover i{width:70px;background:#ff7658}
@keyframes editor-spin{to{transform:rotate(360deg)}}
@media(max-height:760px){.editor-page{min-height:0;grid-template-rows:52px minmax(220px,1fr) 250px}.editor-head{padding:0 12px}.preview-section{padding:8px}.timeline-toolbar{height:40px}}
</style>

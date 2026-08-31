import { computed, nextTick, onMounted, onUnmounted, ref, watch, type InjectionKey } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox, ElNotification } from 'element-plus'
import logoDark from '@/assets/novaly_dark.png'
import { api } from '@/api/client'
import { cosEnabled, putFileToCos, type CosPresign } from '@/api/cosUpload'
import {
  saveBlobDownload,
  getDownloadDirName,
  resolveDownloadDirectory,
  ensureSubdirectory,
  writeBlobToDirectory,
  isDownloadDirSupported,
} from '@/utils/downloadDir'
import type {
  AIModel,
  CharacterRef,
  ConfirmModalState,
  Episode,
  ImagePreviewState,
  ImageGenProgressState,
  ImageGenJobView,
  StylizeJobView,
  StylizeModalState,
  PositioningModalState,
  MotionGridModalState,
  SceneGridModalState,
  SceneReverseModalState,
  ScenePanoramaModalState,
  PanoramaViewerState,
  DirectorDeskModalState,
  EditResourceModalState,
  ModelFormState,
  ModelPreset,
  Project,
  ProjectSummary,
  PromptPreviewState,
  Provider,
  Resource,
  ResourceDisplayEntry,
  ResourceGenRef,
  SceneReference,
  Shot,
  ShotRef,
  CrewAsset,
  CrewJob,
  CrewQCIssue,
} from '@/types'

const defaultStyle = '超真实历史战争电影质感，35mm 电影胶片颗粒，轻微暗角，低对比度胶片调色，4K 超高细节纹理，写实写真渲染，不二次元、不水墨插画，不是CG，不是游戏宣传片，不是概念艺术，纯真人影视实拍效果'
const scriptPlaceholder = `描述你想创作的画面：可 @图1 @图2 引用下方参考图。双人及以上请写九格站位：人名(左前)3/4正面朝右；有站位参考图时以图为准。

【0-3秒】镜头：中景固定，韩铮(左前)3/4正面朝右对峙，阿彪(右中)3/4正面朝左；音效：低沉鼓点+包厢人声；阿彪说：「韩总，干了这杯。」
【3-7秒】镜头：韩铮听完微顿，举杯不饮；音效：杯沿轻碰
【7-10秒】镜头：两人僵持余韵；音效：远处包厢笑声
单人/空镜不必硬写格子。`
const resolutionOptions = ['480p', '720p', '1080p']
  const maxShotRefs = 12
const maxSceneReferences = 12
const maxPositioningRefs = 12
const maxMotionGridRefs = 12
const maxSceneGridRefs = 8
const maxSceneReverseRefs = 8
const maxScenePanoramaRefs = 8
const maxRecentShotRefs = 36
const imageResolutionOptions = [
  { value: '1k', label: '1K' },
  { value: '2k', label: '2K' },
  { value: '4k', label: '4K' },
] as const
const imageResolutionStorageKey = 'novaly.imageResolution'
const imageModelStorageKey = 'novaly.imageModelId'

function loadImageResolution(): '1k' | '2k' | '4k' {
  try {
    const v = localStorage.getItem(imageResolutionStorageKey)
    if (v === '1k' || v === '2k' || v === '4k') return v
    // migrate old quality key
    const q = localStorage.getItem('novaly.imageQuality')
    if (q === 'low') return '1k'
    if (q === 'high') return '4k'
    if (q === 'medium') return '2k'
  } catch { /* ignore */ }
  return '1k'
}

function loadImageModelId(): number | null {
  try {
    const v = localStorage.getItem(imageModelStorageKey)
    if (!v) return null
    const n = Number(v)
    return Number.isFinite(n) && n > 0 ? n : null
  } catch {
    return null
  }
}

function recentShotRefsStorageKey(projectId: number) {
  return `novaly.recentShotRefs.${projectId}`
}

export const CHARACTER_STYLIZE_PROMPT = '只需要把图中的人物脸部转成手绘电影分镜插画风格，全彩，保持脸部特征不变，不要logo，之外其他任何东西都不要改'
export const SCENE_STYLIZE_PROMPT = '只需要把图中的人物脸部转成手绘电影分镜插画风格，全彩，保持脸部特征不变，不要logo，之外其他任何东西都不要改'
export const OTHER_STYLIZE_PROMPT = '只需要把图中的人物脸部转成手绘电影分镜插画风格，全彩，保持脸部特征不变，不要logo，之外其他任何东西都不要改'

export const DOUBAO_WEB_VIDEO_PRESETS: ModelPreset[] = [
  { name: 'Seedance 2.0 Mini', modelId: 'doubao-seedance-2-0-mini' },
  { name: 'Seedance Web', modelId: 'doubao-seedance-2-0-fast' },
  { name: 'Seedance 2.0', modelId: 'doubao-seedance-2-0' },
]

export function useNovaly() {
  const router = useRouter()
  const route = useRoute()
  const projects = ref<ProjectSummary[]>([])
  const trashProjects = ref<ProjectSummary[]>([])
  const active = ref<Project | null>(null)
  const activeEpisode = ref<Episode | null>(null)
  const providers = ref<Provider[]>([])
  const providerKeys = ref<Record<number, string>>({})
  const shownKeys = ref<Record<number, boolean>>({})
  const view = ref<'studio' | 'settings' | 'tts'>('studio')
  const settingsTab = ref<'providers' | 'download' | 'trash'>('providers')
  let applyingRoute = false
  const studioTab = ref<'scripts' | 'episodes' | 'resources'>('scripts')
  const resourceFilter = ref<'all' | 'character' | 'scene' | 'prop' | 'other' | 'video'>('all')
  const resourceQuery = ref('')
  const resourceLibraryTab = ref<'library' | 'trash'>('library')
  const resourceTrash = ref<Resource[]>([])
  const showAddForm = ref(false)
  const loading = ref(false)
  const projectLoading = ref(false)
  const projectHydrated = ref(false)
  const providersLoading = ref(false)
  let hydrateToken = 0
  const saving = ref(false)
  const generating = ref<number | null>(null)
  const uploadingShot = ref<number | null>(null)
  const uploadingShotRef = ref<number | null>(null)
  const applyingShotVideo = ref<number | null>(null)
  const testing = ref<number | null>(null)
  const error = ref('')
  const form = ref({
    title: '',
    episodeCount: 1,
    style: '',
    videoRatio: '16:9',
    kind: 'script',
    genre: '',
    synopsis: '',
    visualManual: '',
    directorManual: '',
    storyboardPace: 'fine',
  })
  const resourceForm = ref({ type: 'character' as 'character' | 'scene' | 'prop' | 'video', name: '', description: '', imageData: '' })
  const regenerateResourceId = ref(0)
  const baseGenPrompt = ref('')
  const promptRevision = ref('')
  const sceneReferences = ref<SceneReference[]>([])
  const sceneRefPickerOpen = ref(false)
  const refPickerTarget = ref<'resource' | 'positioning' | 'motionGrid' | 'sceneGrid' | 'sceneReverse'>('resource')
  const positioningModal = ref<PositioningModalState>(null)
  const positioningRefs = ref<SceneReference[]>([])
  const positioningRefLabelOverrides = new Map<string, string>()
  const positioningReplaceIndex = ref<number | null>(null)
  const positioningPickingSkeleton = ref(false)
  const motionGridModal = ref<MotionGridModalState>(null)
  const motionGridRefs = ref<SceneReference[]>([])
  const motionGridReplaceIndex = ref<number | null>(null)
  const motionGridAnchor = ref<SceneReference | null>(null)
  const sceneGridModal = ref<SceneGridModalState>(null)
  const sceneGridRefs = ref<SceneReference[]>([])
  const sceneGridReplaceIndex = ref<number | null>(null)
  const sceneGridPickingOverhead = ref(false)
  const sceneReverseModal = ref<SceneReverseModalState>(null)
  const sceneReverseRefs = ref<SceneReference[]>([])
  const scenePanoramaModal = ref<ScenePanoramaModalState>(null)
  const scenePanoramaRefs = ref<SceneReference[]>([])
  const sceneReverseReplaceIndex = ref<number | null>(null)
  const sceneReversePickingSkeleton = ref(false)
  const videoFiles = ref<File[]>([])
  const resourceImageFile = ref<File | null>(null)
  const uploadingVideos = ref(false)
  const characterCandidates = ref<{ url: string; resourceId?: number }[]>([])
  const selectedCandidate = ref('')
  const generatingCharacter = ref(false)
  const lastCharacterPrompt = ref('')
  const generatingScene = ref(false)
  const generatingProp = ref(false)
  const lastScenePrompt = ref('')
  const lastPropPrompt = ref('')
  const candidateCount = ref(1)
  const imageResolution = ref<'1k' | '2k' | '4k'>(loadImageResolution())
  watch(imageResolution, (v) => {
    try { localStorage.setItem(imageResolutionStorageKey, v) } catch { /* ignore */ }
  })
  const imageModelId = ref<number | null>(loadImageModelId())
  watch(imageModelId, (v) => {
    try {
      if (v == null) localStorage.removeItem(imageModelStorageKey)
      else localStorage.setItem(imageModelStorageKey, String(v))
    } catch { /* ignore */ }
  })
  // backward-compat alias used by older template bindings during transition
  const imageQuality = imageResolution
  const imageQualityOptions = imageResolutionOptions
  const imageGenJobs = ref<ImageGenJobView[]>([])
  const focusedImageJobId = ref<number | null>(null)
  const submittingImageGen = ref(false)
  const imageGenPollTokens = new Map<number, number>()
  const imageGenPolling = new Set<number>()
  let imageGenResumeToken = 0
  let studioSyncTimer: number | null = null
  let studioSyncInFlight = false
  const studioSyncChannel = typeof BroadcastChannel !== 'undefined'
    ? new BroadcastChannel('novaly-studio-sync')
    : null
  const shotGenPollTimers = new Map<number, number>()
  const dirtyShotIds = ref<Set<number>>(new Set())
  type ShotEditSnapshot = {
    label: string
    script: string
    note: string
    visualStyle: string
    duration: number
    resolution: string
    videoModelId: number | null
    positioningPrompt: string
    motionGridPrompt: string
    refsJson: string
    positioningRefsJson: string
    motionGridRefsJson: string
  }
  const shotEditBaseline = ref<Record<number, ShotEditSnapshot>>({})
  const shotSessionBaseline = ref<Record<number, ShotEditSnapshot>>({})
  const skipSaveShotIds = ref(new Set<number>())
  const shotPageSize = 10
  const shotTotal = ref(0)
  const shotPage = ref(1)
  const shotPageLoading = ref(false)
  let shotPageLoadToken = 0
  let suppressShotPageWatch = false
  const trashPage = ref(1)
  const trashPageSize = 12
  const trashTotal = ref(0)
  const trashLoading = ref(false)
  let trashLoadToken = 0
  let suppressTrashPageWatch = false
  const picker = ref<number | null>(null)
  const recentShotRefsStored = ref<ShotRef[]>([])
  const pickerReplaceIndex = ref<number | null>(null)
  const promptPreview = ref<PromptPreviewState>(null)
  const previewingPrompt = ref<number | null>(null)
  const optimizingScripts = ref<Set<number>>(new Set())
  const matchingShotRefs = ref<Set<number>>(new Set())
  const extractingFrame = ref<number | null>(null)
  /** Bumped after optimize so el-input remounts with the new script. */
  const shotScriptEpoch = ref<Record<number, number>>({})
  const stylizingResources = ref<Set<number>>(new Set())
  const stylizeJobs = ref<StylizeJobView[]>([])
  let stylizeJobSeq = 0
  const showAdvanced = ref<Record<number, boolean>>({})
  const expandedShots = ref<Set<number>>(new Set())
  const highlightedShotId = ref(0)
  let highlightShotTimer: ReturnType<typeof setTimeout> | null = null
  const confirmModal = ref<ConfirmModalState>(null)
  const confirmLoading = ref(false)
  const modelForm = ref<ModelFormState>(null)
  const modelFormSaving = ref(false)
  const stylizeModal = ref<StylizeModalState>(null)
  const editResourceModal = ref<EditResourceModalState>(null)
  const updatingResource = ref<number | null>(null)
  const projectMenuOpen = ref(false)
  const imagePreview = ref<ImagePreviewState>(null)
  const panoramaViewer = ref<PanoramaViewerState>(null)
  const directorDeskModal = ref<DirectorDeskModalState>(null)
  const crewModalOpen = ref(false)
  const crewJob = ref<CrewJob | null>(null)
  const crewBusy = ref(false)
  const crewChatBusy = ref(false)
  const crewShotConflict = ref(0)
  const episodeScriptOpen = ref(true)
  const extractingEpisodeIds = ref<number[]>([])
  const selectedAssetIds = ref<number[]>([])
  const batchResourceBusy = ref<'prompts' | 'images' | ''>('')
  let crewPollTimer: ReturnType<typeof setTimeout> | null = null
  let crewPollToken = 0

  const characters = computed(() => active.value?.resources.filter(r => r.type === 'character') ?? [])
  const scenes = computed(() => active.value?.resources.filter(r => r.type === 'scene') ?? [])
  const hasSceneReference = computed(() => sceneReferences.value.length > 0)
  const img2imgRefLabel = computed(() => `参考图（图生图，可多选，${sceneReferences.value.length}/${maxSceneReferences}）`)
  const img2imgRefHint = computed(() => {
    if (resourceForm.value.type === 'character') {
      return regenerateResourceId.value > 0
        ? '换配饰时：人物定妆照和道具图都要选。系统会按图号标注谁是角色、谁是道具；本次修改写清「把脖子上的奖牌换成图N那一枚」。'
        : '已选参考图：AI 将基于参考图生成三视图角色定妆照'
    }
    if (resourceForm.value.type === 'prop') return '已选参考图：AI 将基于参考图生成道具参考图'
    return '已选参考图：AI 将基于参考图生成场景参考图（可指定人物站位）'
  })
  function refListForTarget(t: 'resource' | 'positioning' | 'motionGrid' | 'sceneGrid' | 'sceneReverse') {
    if (t === 'positioning') return positioningRefs
    if (t === 'motionGrid') return motionGridRefs
    if (t === 'sceneGrid') return sceneGridRefs
    if (t === 'sceneReverse') return sceneReverseRefs
    return sceneReferences
  }
  function activeReplaceIndex(): number | null {
    const t = refPickerTarget.value
    if (t === 'positioning') return positioningReplaceIndex.value
    if (t === 'motionGrid') return motionGridReplaceIndex.value
    if (t === 'sceneGrid') return sceneGridReplaceIndex.value
    if (t === 'sceneReverse') return sceneReverseReplaceIndex.value
    return null
  }
  function setActiveReplaceIndex(v: number | null) {
    const t = refPickerTarget.value
    if (t === 'positioning') positioningReplaceIndex.value = v
    else if (t === 'motionGrid') motionGridReplaceIndex.value = v
    else if (t === 'sceneGrid') sceneGridReplaceIndex.value = v
    else if (t === 'sceneReverse') sceneReverseReplaceIndex.value = v
  }
  const sceneRefPickerTitle = computed(() => {
	if (refPickerTarget.value === 'positioning' && positioningPickingSkeleton.value) return '替换火柴人骨架'
    if (refPickerTarget.value === 'sceneGrid' && sceneGridPickingOverhead.value) return '选择俯视布局线稿'
    if (refPickerTarget.value === 'sceneReverse' && sceneReversePickingSkeleton.value) return '选择反打线稿'
    const replaceIdx = activeReplaceIndex()
    const replaceList = refListForTarget(refPickerTarget.value).value
    if (replaceIdx != null) {
      const cur = replaceList[replaceIdx]
      return cur ? `替换 图${replaceIdx + 1} · ${cur.label}` : `替换 图${replaceIdx + 1}`
    }
    if (refPickerTarget.value === 'positioning') return '选择站位图参考'
    if (refPickerTarget.value === 'motionGrid') return '选择9帧图参考'
    if (refPickerTarget.value === 'sceneGrid') return '选择9宫格参考'
    if (refPickerTarget.value === 'sceneReverse') return '选择反打图参考'
    if (resourceForm.value.type === 'character') return '选择角色参考图'
    if (resourceForm.value.type === 'prop') return '选择道具参考图'
    return '选择场景参考图'
  })
  const sceneRefPickerHint = computed(() => {
	if (refPickerTarget.value === 'positioning' && positioningPickingSkeleton.value) {
		return '请选择一张骨架图、站位示意图或你认可的空间草图。选中后将直接替换当前火柴人骨架，并作为正式站位图生成时的图1。'
	}
    if (refPickerTarget.value === 'sceneGrid' && sceneGridPickingOverhead.value) {
      return '从资源库选择一张俯视布局图或俯视线稿，将直接作为生成9宫格的空间方位约束。'
    }
    if (refPickerTarget.value === 'sceneReverse' && sceneReversePickingSkeleton.value) {
      return '请选择资源库中已生成的反打线稿。它会作为图1锁定构图，用来重新生成反打图。'
    }
    if (activeReplaceIndex() != null) {
      return '选择一张新图替换当前位置；图序号保持不变，便于提示词中的「图N为谁」对应。'
    }
    if (refPickerTarget.value === 'positioning') {
      return `可多选参考图（最多 ${maxPositioningRefs} 张）：角色、场景、道具均可；自动匹配时角色优先真人原图（与写实场景对齐）。再次点击可取消选择。`
    }
    if (refPickerTarget.value === 'motionGrid') {
      return `可多选参考图（最多 ${maxMotionGridRefs} 张）：第 1 张通常为上一镜收势帧，其余为角色/场景；再次点击可取消选择。`
    }
    if (refPickerTarget.value === 'sceneGrid') {
      return `可多选参考图（最多 ${maxSceneGridRefs} 张）：建议第 1 张为场景原图，其余为其它角度/细节参考；再次点击可取消选择。`
    }
    if (refPickerTarget.value === 'sceneReverse') {
      return `可多选参考图（最多 ${maxSceneReverseRefs} 张）：第 1 张必须是场景原图。9 宫格会自动带上「俯视全景」（底排左，整桌俯视），不要用手选俯视近景。`
    }
    const max = maxSceneReferences
    if (resourceForm.value.type === 'character') return `点选真人/非真人按你选的用；定妆照/衍生图默认真人优先。可多选（最多 ${max} 张），再次点击可取消选择。`
    if (resourceForm.value.type === 'prop') return `可多选道具参考图（最多 ${max} 张），再次点击可取消选择。`
    return `点选原图/非真人按你选的用；自动匹配时场景优先原图。建议第 1 张为场景环境，其余为角色参考，最多 ${max} 张，选完点完成。`
  })
  const refPickerReferences = computed(() => refListForTarget(refPickerTarget.value).value)
  const refPickerMax = computed(() => {
    if (refPickerTarget.value === 'positioning') return maxPositioningRefs
    if (refPickerTarget.value === 'motionGrid') return maxMotionGridRefs
    if (refPickerTarget.value === 'sceneGrid') return maxSceneGridRefs
    if (refPickerTarget.value === 'sceneReverse') return maxSceneReverseRefs
    return maxSceneReferences
  })
  const refPickerReplaceHint = computed(() => {
    const idx = activeReplaceIndex()
    if (idx == null) return ''
    return `替换模式：点选一张图即可替换 图${idx + 1}`
  })
  const props = computed(() => active.value?.resources.filter(r => r.type === 'prop') ?? [])
  const others = computed(() => active.value?.resources.filter(r => r.type === 'other') ?? [])
  const videoResources = computed(() => active.value?.resources.filter(r => r.type === 'video') ?? [])
  const resourcePage = ref(1)
  const resourcePageSize = 12
  const libraryPageItems = ref<Resource[]>([])
  const libraryTotal = ref(0)
  const libraryLoading = ref(false)
  const libraryReady = ref(false)
  const libraryCounts = ref({ all: 0, character: 0, scene: 0, prop: 0, other: 0, video: 0 })
  const libraryParentId = ref<number | null>(null)
  const libraryParent = ref<Resource | null>(null)
  let libraryLoadToken = 0
  let suppressResourcePageWatch = false

  async function loadLibraryPage(opts?: { resetPage?: boolean }) {
    if (!active.value) return
    if (opts?.resetPage) {
      suppressResourcePageWatch = true
      resourcePage.value = 1
      suppressResourcePageWatch = false
    }
    const projectId = active.value.id
    const token = ++libraryLoadToken
    libraryLoading.value = true
    try {
      const type = resourceFilter.value || 'all'
      const q = resourceQuery.value.trim()
      const qs = new URLSearchParams({
        page: String(resourcePage.value),
        pageSize: String(resourcePageSize),
        type,
        hideSceneGridCells: '1',
      })
      if (q) qs.set('q', q)
      if (libraryParentId.value) qs.set('parentId', String(libraryParentId.value))
      const data = await api(`/projects/${projectId}/resources?${qs.toString()}`)
      if (token !== libraryLoadToken || active.value?.id !== projectId) return
      const items = Array.isArray(data?.items) ? data.items as Resource[] : (Array.isArray(data) ? data as Resource[] : [])
      libraryPageItems.value = items
      libraryTotal.value = typeof data?.total === 'number' ? data.total : items.length
      libraryReady.value = true
      if (data?.counts && typeof data.counts === 'object') {
        libraryCounts.value = {
          all: Number(data.counts.all) || 0,
          character: Number(data.counts.character) || 0,
          scene: Number(data.counts.scene) || 0,
          prop: Number(data.counts.prop) || 0,
          other: Number(data.counts.other) || 0,
          video: Number(data.counts.video) || 0,
        }
      }
      mergeNewResources(items)
    } catch (e: any) {
      if (token === libraryLoadToken) error.value = e.message
    } finally {
      if (token === libraryLoadToken) libraryLoading.value = false
    }
  }

  function openResourceDerives(parent: Resource) {
    if (!parent?.id || parent.parentId) return
    libraryParentId.value = parent.id
    libraryParent.value = parent
    selectedAssetIds.value = []
    void loadLibraryPage({ resetPage: true })
  }

  function closeResourceDerives() {
    libraryParentId.value = null
    libraryParent.value = null
    selectedAssetIds.value = []
    void loadLibraryPage({ resetPage: true })
  }

  function resetLibraryPaging() {
    libraryLoadToken++
    libraryPageItems.value = []
    libraryTotal.value = 0
    libraryReady.value = false
    libraryLoading.value = false
    libraryCounts.value = { all: 0, character: 0, scene: 0, prop: 0, other: 0, video: 0 }
    libraryParentId.value = null
    libraryParent.value = null
    resourcePage.value = 1
    resourceQuery.value = ''
    trashLoadToken++
    resourceTrash.value = []
    trashTotal.value = 0
    trashLoading.value = false
    trashPage.value = 1
    shotPageLoadToken++
    shotTotal.value = 0
    shotPage.value = 1
    shotPageLoading.value = false
    clearAllShotDirty()
  }

  const topLevelLibraryResources = computed(() =>
    (active.value?.resources ?? []).filter(r =>
      !r.parentId
      && r.genType !== 'scene_grid_cell'
      && r.genType !== 'motion_grid_cell'
      && r.genType !== 'scene_panorama_view',
    ),
  )
  const resourceCounts = computed(() => ({
    all: libraryReady.value ? libraryCounts.value.all : topLevelLibraryResources.value.length,
    character: libraryReady.value ? libraryCounts.value.character : topLevelLibraryResources.value.filter(r => r.type === 'character').length,
    scene: libraryReady.value ? libraryCounts.value.scene : topLevelLibraryResources.value.filter(r => r.type === 'scene').length,
    prop: libraryReady.value ? libraryCounts.value.prop : topLevelLibraryResources.value.filter(r => r.type === 'prop').length,
    other: libraryReady.value ? libraryCounts.value.other : topLevelLibraryResources.value.filter(r => r.type === 'other').length,
    video: libraryReady.value ? libraryCounts.value.video : topLevelLibraryResources.value.filter(r => r.type === 'video').length,
  }))
  const managedResources = computed(() => {
    if (libraryReady.value) return libraryPageItems.value
    let items = topLevelLibraryResources.value.filter(r => !parseCandidateName(r.name) || r.isGroupPrimary)
    if (resourceFilter.value === 'all') return items
    return items.filter(r => r.type === resourceFilter.value)
  })
  const managedResourceDisplay = computed<ResourceDisplayEntry[]>(() =>
    managedResources.value.map(resource => ({ kind: 'resource', resource }))
  )
  const managedResourceTrash = computed(() => resourceTrash.value)
  const resourceTrashCounts = computed(() => ({
    all: trashTotal.value,
    character: 0,
    scene: 0,
    prop: 0,
    other: 0,
    video: 0,
  }))
  const applyingPrimary = ref<number | null>(null)
  const candidatesPersisted = computed(() => characterCandidates.value.some(c => c.resourceId))
  const showCreateButton = computed(() => {
    if (resourceForm.value.type === 'video') return videoFiles.value.length > 0
    if (resourceForm.value.imageData) return true
    return true
  })
  const createResourceLabel = computed(() => {
    if (resourceForm.value.type === 'video') {
      return uploadingVideos.value ? `上传中（${videoFiles.value.length} 个）…` : `上传 ${videoFiles.value.length} 个视频`
    }
    const label = resourceFormTypeLabel(resourceForm.value.type)
    if (existingLibraryBase.value && (resourceForm.value.imageData || candidatesPersisted.value)) {
      return saving.value ? `写入「${resourceDisplayName(existingLibraryBase.value)}」…` : `写入已有${label}`
    }
    if (resourceForm.value.imageData) return `添加${label}`
    if (candidatesPersisted.value) return saving.value ? '写入中…' : '完成'
    return saving.value ? `添加${label}中…` : `添加${label}`
  })
  const videoModels = computed(() =>
    providers.value
      .filter(p => p.enabled !== false)
      .flatMap(p => p.models.filter(m => m.capability === 'video' && m.enabled))
      .slice()
      .sort((a, b) => (a.providerId - b.providerId) || a.id - b.id),
  )
  const imageModels = computed(() =>
    providers.value
      .filter(p => p.enabled !== false)
      .flatMap(p => p.models.filter(m => m.capability === 'image' && m.enabled))
      .slice()
      .sort((a, b) => (a.providerId - b.providerId) || a.id - b.id),
  )
  const textModels = computed(() =>
    providers.value
      .filter(p => p.enabled !== false)
      .flatMap(p => p.models.filter(m => m.capability === 'text' && m.enabled))
      .slice()
      .sort((a, b) => (a.providerId - b.providerId) || a.id - b.id),
  )
  /** Enabled image models grouped by provider for studio / settings selectors. */
  const imageModelsByProvider = computed(() => {
    const groups: { providerId: number; providerName: string; models: AIModel[] }[] = []
    for (const p of providers.value) {
      if (p.enabled === false) continue
      const models = p.models
        .filter(m => m.capability === 'image' && m.enabled)
        .slice()
        .sort((a, b) => a.id - b.id)
      if (!models.length) continue
      groups.push({ providerId: p.id, providerName: p.name, models })
    }
    return groups
  })
  const defaultTextModel = computed(() => textModels.value.find(m => m.isDefault) ?? textModels.value[0] ?? null)
  const defaultTextModelId = computed(() => defaultTextModel.value?.id ?? null)
  const defaultImageModel = computed(() => imageModels.value.find(m => m.isDefault) ?? imageModels.value[0] ?? null)
  const defaultImageModelId = computed(() => defaultImageModel.value?.id ?? null)
  const defaultImageModelLabel = computed(() => {
    const m = defaultImageModel.value
    if (!m) return '未配置图像模型'
    return imageModelLabel(m, { prefix: '默认', showDefault: false })
  })
  const effectiveImageModelId = computed(() => {
    if (imageModelId.value && imageModels.value.some(m => m.id === imageModelId.value)) {
      return imageModelId.value
    }
    return defaultImageModelId.value
  })
  const defaultVideoModel = computed(() => videoModels.value.find(m => m.isDefault) ?? videoModels.value[0] ?? null)
  const defaultVideoModelId = computed(() => defaultVideoModel.value?.id ?? null)
  const defaultVideoModelLabel = computed(() => {
    const m = defaultVideoModel.value
    if (!m) return '未配置视频模型'
    return videoModelLabel(m, { prefix: '默认', showDefault: false })
  })
  const pickerPrimaryCharacters = computed(() => characters.value.filter(c => (!parseCandidateName(c.name) || c.isGroupPrimary) && !!(c.imageUrl || c.stylizedImageUrl)))
  const pickerPrimaryScenes = computed(() => scenes.value.filter(s => (!parseCandidateName(s.name) || s.isGroupPrimary) && !!(s.imageUrl || s.stylizedImageUrl)))
  const pickerPrimaryProps = computed(() => props.value.filter(p => (!parseCandidateName(p.name) || p.isGroupPrimary) && !!p.imageUrl))
  const pickerPrimaryOthers = computed(() => others.value.filter(o => (!parseCandidateName(o.name) || o.isGroupPrimary) && !!(o.imageUrl || o.stylizedImageUrl)))

  // 场景9宫格整图与切分格子不平铺进选择器；改为点进场景后二级挑选格子
  const pickerFlatScenes = computed(() =>
    pickerPrimaryScenes.value.filter(s => !s.gridId && s.genType !== 'scene_grid' && s.genType !== 'scene_grid_cell'),
  )
  function sceneGridsFor(scene: Resource): Resource[] {
    const base = (parseCandidateName(scene.name)?.base || scene.name).trim()
    if (!base) return []
    return scenes.value.filter(r =>
      r.genType === 'scene_grid' && !r.deletedAt && !!r.imageUrl &&
      (r.parentId === scene.id || (r.genRefs || []).some(g => g.id === scene.id) || r.name === `${base} · 9宫格` || r.name.startsWith(`${base} · 9宫格 · `)),
    )
  }
  const sceneGridsMap = computed(() => {
    const map = new Map<number, Resource[]>()
    for (const s of pickerFlatScenes.value) {
      const grids = sceneGridsFor(s)
      if (grids.length) map.set(s.id, grids)
    }
    return map
  })
  function gridCellsFor(gridId: number): Resource[] {
    return (active.value?.resources ?? [])
      .filter(r => r.gridId === gridId && !r.deletedAt && !!r.imageUrl)
      .sort((a, b) => (a.gridCell || 0) - (b.gridCell || 0))
  }
  const splitGridIds = computed(() => {
    const set = new Set<number>()
    for (const r of active.value?.resources ?? []) {
      if (r.gridId && !r.deletedAt) set.add(r.gridId)
    }
    return set
  })
  function isGridCellItem(item: Resource) {
    return !!(item.gridId || item.genType === 'scene_grid_cell' || item.genType === 'motion_grid_cell' || item.genType === 'scene_panorama_view')
  }
  function gridIsSplit(gridId: number) {
    return splitGridIds.value.has(gridId)
  }
  function mergeGridCells(cells: Resource[]) {
    if (!active.value || !cells?.length) return
    for (const cell of cells) upsertResourceInCaches(cell)
  }
  async function loadGridCells(gridId: number) {
    if (!gridId || !active.value) return [] as Resource[]
    try {
      const result = await api(`/resources/${gridId}/grid-cells`)
      const cells = Array.isArray(result?.cells) ? result.cells as Resource[] : []
      if (cells.length) mergeGridCells(cells)
      return cells
    } catch {
      return gridCellsFor(gridId)
    }
  }

  function aiGenerateLabel(type: 'character' | 'scene' | 'prop', generating: boolean) {
    if (generating) {
      const msg = imageGenProgress.value?.message
      if (msg) return msg
      return `生成中（${candidateCount.value} 张）…`
    }
    if (hasSceneReference.value) {
      return ({ character: '✦ AI 图生图生成定妆照', scene: '✦ AI 图生图生成场景参考图', prop: '✦ AI 图生图生成道具参考图' })[type]
    }
    return ({ character: '✦ AI 生成角色定妆照', scene: '✦ AI 生成场景参考图', prop: '✦ AI 生成道具参考图' })[type]
  }
  function parseCandidateName(name: string) {
    const m = name.match(/^(.+?)\s*·\s*候选(\d+)$/)
    if (!m) return null
    return { base: m[1].trim(), index: Number(m[2]) }
  }
  function resourceBaseName(name: string) {
    return parseCandidateName(name)?.base || name.trim()
  }
  function findExistingLibraryBase(name: string, type: string) {
    const base = name.trim().toLowerCase()
    if (!base || (type !== 'character' && type !== 'scene' && type !== 'prop')) return undefined
    const seen = new Set<number>()
    const pool: Resource[] = []
    for (const r of [...libraryPageItems.value, ...(active.value?.resources || [])]) {
      if (seen.has(r.id)) continue
      seen.add(r.id)
      pool.push(r)
    }
    const parentId = libraryParentId.value
    const sameType = parentId
      ? pool.filter(r => r.type === type && r.parentId === parentId)
      : pool.filter(r => r.type === type && !r.parentId)
    const exact = sameType.find(r => !parseCandidateName(r.name) && r.name.trim().toLowerCase() === base)
    if (exact) return exact
    return sameType.find((r) => {
      const n = resourceBaseName(r.name).toLowerCase()
      return n === base && (!parseCandidateName(r.name) || r.isGroupPrimary)
    })
  }
  const existingLibraryBase = computed(() =>
    findExistingLibraryBase(resourceForm.value.name, resourceForm.value.type),
  )
  function candidateGroupKey(item: Resource) {
    const p = parseCandidateName(item.name)
    if (!p) return ''
    return `${item.type}:${p.base}`
  }
  function resourceFormTypeLabel(type: string) {
    return ({
      character: '角色',
      scene: '场景',
      prop: '道具',
      other: '其他',
      video: '视频',
      positioning: '站位图',
      positioning_skeleton: '火柴人骨架',
      scene_grid: '场景9宫格',
      scene_reverse: '反打图',
      scene_reverse_skeleton: '反打骨架',
      scene_panorama: '场景全景',
      scene_panorama_view: '全景机位',
      motion_grid: '9帧图',
    } as Record<string, string>)[type] || type
  }

  async function load() {
    loading.value = true
    try {
      projects.value = await api('/projects')
      await loadTrash()
    } finally {
      loading.value = false
    }
  }

  function projectShell(id: number): Project {
    const s = projects.value.find(p => p.id === id)
    return {
      id,
      title: s?.title || '加载中…',
      episodeCount: s?.episodeCount || 1,
      kind: s?.kind || 'script',
      genre: s?.genre || '',
      synopsis: s?.synopsis || '',
      visualManual: s?.visualManual || '',
      directorManual: s?.directorManual || '',
      style: s?.style || '',
      videoRatio: s?.videoRatio || '16:9',
      storyboardPace: s?.storyboardPace || 'fine',
      episodes: [],
      resources: [],
    }
  }

  function studioPath(opts?: { tab?: 'scripts' | 'episodes' | 'resources'; episodeNumber?: number }) {
    const id = active.value?.id
    if (!id) return '/'
    const tab = opts?.tab ?? studioTab.value
    if (tab === 'resources') return `/projects/${id}/resources`
    if (tab === 'scripts') return `/projects/${id}`
    const n = opts?.episodeNumber ?? activeEpisode.value?.number ?? 1
    return `/projects/${id}/episodes/${n}`
  }

  function settingsPath(tab: 'providers' | 'download' | 'trash' = 'providers') {
    if (tab === 'trash') return '/settings/trash'
    if (tab === 'download') return '/settings/download'
    return '/settings'
  }

  async function navigateTo(path: string) {
    if (route.path === path) return
    applyingRoute = true
    try {
      await router.push(path)
      // Let route watchers see applyingRoute=true before we clear it.
      await Promise.resolve()
      await new Promise<void>(resolve => queueMicrotask(() => resolve()))
    } finally {
      applyingRoute = false
    }
  }
  async function loadTrash() {
    try { trashProjects.value = await api('/projects/trash') } catch { trashProjects.value = [] }
  }
  function askConfirm(opts: ConfirmModalState) { confirmModal.value = opts }
  function closeConfirm() { confirmModal.value = null }
  async function runConfirm() {
    if (!confirmModal.value) return
    confirmLoading.value = true
    try { await confirmModal.value.onConfirm() } finally { confirmLoading.value = false; confirmModal.value = null }
  }
  async function deleteProject(p: ProjectSummary) {
    askConfirm({
      title: '删除项目',
      message: `确定将「${p.title}」移入回收站？项目数据会保留，可在回收站恢复。`,
      confirmText: '移入回收站',
      danger: true,
      onConfirm: async () => {
        await api(`/projects/${p.id}`, { method: 'DELETE' })
        const wasActive = active.value?.id === p.id
        if (wasActive) { active.value = null; activeEpisode.value = null }
        projects.value = await api('/projects')
        await loadTrash()
        if (wasActive) {
          if (projects.value.length) await openProject(projects.value[0].id)
          else await goHome()
        }
      },
    })
  }
  async function restoreProject(p: ProjectSummary) {
    await api(`/projects/${p.id}/restore`, { method: 'POST' })
    projects.value = await api('/projects')
    await loadTrash()
    await openProject(p.id)
  }
  function purgeProject(p: ProjectSummary) {
    askConfirm({
      title: '彻底删除',
      message: `确定永久删除「${p.title}」？此操作不可恢复，所有分镜和资源将被清除。`,
      confirmText: '永久删除',
      danger: true,
      onConfirm: async () => {
        await api(`/projects/${p.id}/permanent`, { method: 'DELETE' })
        await loadTrash()
      },
    })
  }
  function openTrash() { openSettings('trash') }
  function deleteActiveProject() {
    if (!active.value) return
    const p = projects.value.find(x => x.id === active.value!.id) || { id: active.value.id, title: active.value.title, episodeCount: active.value.episodeCount, style: active.value.style, videoRatio: active.value.videoRatio, shotCount: 0 }
    projectMenuOpen.value = false
    deleteProject(p)
  }
  const addingEpisode = ref(false)
  async function addEpisode() {
    if (!active.value || addingEpisode.value) return
    if (active.value.episodeCount >= 100) { error.value = '集数不能超过 100'; return }
    if (activeEpisode.value) {
      try { await saveEpisodeScript() } catch { /* keep going so a new episode can still be created */ }
    }
    addingEpisode.value = true
    try {
      const ep = await api(`/projects/${active.value.id}/episodes`, { method: 'POST' })
      active.value.episodes = [...(active.value.episodes || []), normalizeEpisode({ ...ep, shots: [] })]
      active.value.episodeCount = active.value.episodes.length
      activeEpisode.value = normalizeEpisode(ep)
      shotTotal.value = 0
      suppressShotPageWatch = true
      shotPage.value = 1
      suppressShotPageWatch = false
      episodeScriptOpen.value = true
      const stayScripts = studioTab.value === 'scripts'
      if (!stayScripts) studioTab.value = 'episodes'
      projects.value = await api('/projects')
      await navigateTo(studioPath({
        tab: stayScripts ? 'scripts' : 'episodes',
        episodeNumber: activeEpisode.value.number,
      }))
    } catch (e: any) {
      error.value = e.message || '添加分集失败'
    } finally {
      addingEpisode.value = false
    }
  }
  function removeEpisode(ep: Episode) {
    if (!active.value || (active.value.episodes || []).length <= 1) return
    askConfirm({
      title: '删除分集',
      message: `确定删除「${ep.title}」？该集所有分镜将一并删除，其余分集会自动重编号。`,
      confirmText: '删除分集',
      danger: true,
      onConfirm: async () => {
        const wasId = activeEpisode.value?.id
        await api(`/episodes/${ep.id}`, { method: 'DELETE' })
        const p = normalizeProject(await api(`/projects/${active.value!.id}`))
        active.value = p
        const next = wasId === ep.id ? p.episodes[0] : p.episodes.find(e => e.id === wasId) || p.episodes[0]
        activeEpisode.value = next ? normalizeEpisode(next) : null
        const stayScripts = studioTab.value === 'scripts'
        if (!stayScripts) studioTab.value = 'episodes'
        projects.value = await api('/projects')
        await navigateTo(studioPath({ tab: stayScripts ? 'scripts' : 'episodes' }))
      },
    })
  }
  async function loadProviders() {
    providersLoading.value = true
    try {
      providers.value = await api('/settings/providers')
    } catch (e: any) {
      error.value = e.message
    } finally {
      providersLoading.value = false
    }
  }
  function normalizeShot(s: any): Shot {
    let refs: ShotRef[] = (s.refs || []).map((r: any) => ({
      kind: r.kind === 'scene' ? 'scene' : r.kind === 'prop' ? 'prop' : r.kind === 'other' ? 'other' : 'character',
      id: r.id,
      variant: r.variant === 'original' ? 'original' : r.variant === 'stylized' ? 'stylized' : undefined,
      label: typeof r.label === 'string' && r.label.trim() ? r.label.trim() : undefined,
    }))
    if (!refs.length) {
      const charRefs: CharacterRef[] = (s.characterRefs || []).map((r: any) => ({
        id: r.id, variant: r.variant === 'original' ? 'original' : 'stylized',
      }))
      if (!charRefs.length && s.characterIds?.length) {
        s.characterIds.forEach((id: number) => charRefs.push({ id, variant: 'stylized' }))
      }
      refs = charRefs.map(r => ({ kind: 'character' as const, id: r.id, variant: r.variant }))
      if (s.sceneId) refs.push({ kind: 'scene', id: s.sceneId, variant: 'original' })
    }
    return {
      ...s,
      refs,
      characterRefs: refs.filter(r => r.kind === 'character').map(r => ({ id: r.id, variant: r.variant || 'stylized' })),
      sceneId: refs.find(r => r.kind === 'scene')?.id ?? s.sceneId ?? null,
      visualStyle: s.visualStyle || '',
      note: s.note || '',
      imageRefs: s.imageRefs || '',
      label: s.label || '',
      duration: s.duration > 0 ? s.duration : 10,
      resolution: s.resolution || '720p',
      videoModelId: s.videoModelId ?? null,
      positioningPrompt: typeof s.positioningPrompt === 'string' ? s.positioningPrompt : '',
      positioningRefs: Array.isArray(s.positioningRefs) ? s.positioningRefs : [],
      motionGridPrompt: typeof s.motionGridPrompt === 'string' ? s.motionGridPrompt : '',
      motionGridRefs: Array.isArray(s.motionGridRefs) ? s.motionGridRefs : [],
      videoEta: typeof s.videoEta === 'string' ? s.videoEta : '',
    }
  }
  function normalizeEpisode(ep: any): Episode {
    const shots = (ep.shots || []).map(normalizeShot).sort((a: Shot, b: Shot) => a.sortOrder - b.sortOrder || a.id - b.id)
    const shotTotalValue = typeof ep.shotTotal === 'number' ? ep.shotTotal : shots.length
    return { ...ep, script: ep.script || '', directorPlan: ep.directorPlan || '', shots, shotTotal: shotTotalValue, assets: Array.isArray(ep.assets) ? ep.assets : [], crewStatus: ep.crewStatus || '', crewStage: ep.crewStage || '' }
  }
  function normalizeProject(p: any): Project {
    return {
      ...p,
      kind: p.kind || 'script',
      genre: p.genre || '',
      synopsis: p.synopsis || '',
      visualManual: p.visualManual || '',
      directorManual: p.directorManual || '',
      videoRatio: p.videoRatio || '16:9',
      storyboardPace: p.storyboardPace || 'fine',
      episodes: (p.episodes || []).map(normalizeEpisode),
      resources: p.resources || [],
    }
  }
  function selectEpisodeByNumber(episodeNumber?: number) {
    const episodes = active.value?.episodes || []
    if (!episodes.length) {
      activeEpisode.value = null
      shotTotal.value = 0
      return
    }
    const ep = episodeNumber != null
      ? episodes.find(e => e.number === episodeNumber)
      : null
    activeEpisode.value = normalizeEpisode(ep || episodes[0])
    shotTotal.value = activeEpisode.value.shotTotal ?? activeEpisode.value.shots.length
    if (!shotTotal.value) episodeScriptOpen.value = true
  }

  async function goToEpisode(episodeNumber: number) {
    if (!active.value) return
    const switchingEpisode = activeEpisode.value?.number !== episodeNumber
    if (switchingEpisode && activeEpisode.value) {
      try { await saveEpisodeScript() } catch { /* still switch so the user is not stuck */ }
    }
    if (switchingEpisode) selectEpisodeByNumber(episodeNumber)
    studioTab.value = 'episodes'
    await navigateTo(studioPath({ tab: 'episodes', episodeNumber }))
    try {
      await loadCrewJob()
      // 已经有分镜就直接进入工作区；只有空分镜集才弹出 AI 剧组引导。
      if (shotTotal.value < 1) {
        crewModalOpen.value = true
        crewShotConflict.value = 0
      } else {
        crewModalOpen.value = false
        crewShotConflict.value = 0
      }
      if (crewJob.value?.status === 'running') void pollCrewJob()
    } catch {
      // 分镜页仍正常打开；剧组状态稍后可由页面监听重新加载。
    }
  }

  function shotEditSnapshot(shot: Shot): ShotEditSnapshot {
    return {
      label: shot.label || '',
      script: shot.script || '',
      note: shot.note || '',
      visualStyle: shot.visualStyle || '',
      duration: shot.duration,
      resolution: shot.resolution,
      videoModelId: shot.videoModelId || null,
      positioningPrompt: shot.positioningPrompt || '',
      motionGridPrompt: shot.motionGridPrompt || '',
      refsJson: JSON.stringify(shot.refs || []),
      positioningRefsJson: JSON.stringify(shot.positioningRefs || []),
      motionGridRefsJson: JSON.stringify(shot.motionGridRefs || []),
    }
  }

  function captureShotBaseline(shot: Shot) {
    shotEditBaseline.value = { ...shotEditBaseline.value, [shot.id]: shotEditSnapshot(shot) }
  }

  function beginShotEditSession(shot: Shot) {
    if (shotSessionBaseline.value[shot.id]) return
    shotSessionBaseline.value = { ...shotSessionBaseline.value, [shot.id]: shotEditSnapshot(shot) }
  }

  function clearShotSessionBaseline(shotId: number) {
    if (!shotSessionBaseline.value[shotId]) return
    const next = { ...shotSessionBaseline.value }
    delete next[shotId]
    shotSessionBaseline.value = next
  }

  function applyShotSnapshot(shot: Shot, snap: ShotEditSnapshot): Shot {
    return {
      ...shot,
      label: snap.label,
      script: snap.script,
      note: snap.note,
      visualStyle: snap.visualStyle,
      duration: snap.duration,
      resolution: snap.resolution,
      videoModelId: snap.videoModelId,
      positioningPrompt: snap.positioningPrompt,
      motionGridPrompt: snap.motionGridPrompt,
      refs: JSON.parse(snap.refsJson) as Shot['refs'],
      positioningRefs: JSON.parse(snap.positioningRefsJson) as Shot['positioningRefs'],
      motionGridRefs: JSON.parse(snap.motionGridRefsJson) as Shot['motionGridRefs'],
    }
  }

  function isShotDirty(shotId: number): boolean {
    if (dirtyShotIds.value.has(shotId)) return true
    const session = shotSessionBaseline.value[shotId]
    const shot = activeEpisode.value?.shots?.find(s => s.id === shotId)
    if (!session || !shot) return false
    return JSON.stringify(shotEditSnapshot(shot)) !== JSON.stringify(session)
  }

  function revertShotEdits(shot: Shot) {
    const snap = shotSessionBaseline.value[shot.id] || shotEditBaseline.value[shot.id]
    if (!snap) return
    const local = activeEpisode.value?.shots?.find(s => s.id === shot.id)
    if (!local) return
    skipSaveShotIds.value = new Set(skipSaveShotIds.value).add(shot.id)
    replaceShot(applyShotSnapshot(local, snap))
    clearShotDirty(shot.id)
    clearShotSessionBaseline(shot.id)
    bumpShotScriptEpoch(shot.id)
    window.setTimeout(() => {
      const next = new Set(skipSaveShotIds.value)
      next.delete(shot.id)
      skipSaveShotIds.value = next
    }, 0)
  }

  async function reloadShotFromServer(shot: Shot) {
    const raw = await api(`/shots/${shot.id}`)
    const latest = normalizeShot(raw)
    skipSaveShotIds.value = new Set(skipSaveShotIds.value).add(shot.id)
    replaceShot(latest)
    captureShotBaseline(latest)
    clearShotDirty(shot.id)
    clearShotSessionBaseline(shot.id)
    bumpShotScriptEpoch(shot.id)
    window.setTimeout(() => {
      const next = new Set(skipSaveShotIds.value)
      next.delete(shot.id)
      skipSaveShotIds.value = next
    }, 0)
  }

  function markShotDirty(shotId: number) {
    const next = new Set(dirtyShotIds.value)
    next.add(shotId)
    dirtyShotIds.value = next
  }

  function clearShotDirty(shotId: number) {
    if (!dirtyShotIds.value.has(shotId)) return
    const next = new Set(dirtyShotIds.value)
    next.delete(shotId)
    dirtyShotIds.value = next
  }

  function clearAllShotDirty() {
    if (!dirtyShotIds.value.size) return
    dirtyShotIds.value = new Set()
  }

  function mergeShotPreservingDirty(local: Shot | undefined, remote: Shot): Shot {
    if (!local || !dirtyShotIds.value.has(local.id)) return remote
    return {
      ...remote,
      label: local.label,
      script: local.script,
      note: local.note,
      visualStyle: local.visualStyle,
      duration: local.duration,
      resolution: local.resolution,
      videoModelId: local.videoModelId,
      positioningPrompt: local.positioningPrompt,
      positioningRefs: local.positioningRefs,
      motionGridPrompt: local.motionGridPrompt,
      motionGridRefs: local.motionGridRefs,
      refs: local.refs,
      characterRefs: local.characterRefs,
      sceneId: local.sceneId,
    }
  }

  async function loadShotPage(page = shotPage.value, opts?: { force?: boolean }) {
    if (!active.value || !activeEpisode.value?.id) return
    const epId = activeEpisode.value.id
    const nextPage = Math.max(1, page)
    const token = ++shotPageLoadToken
    shotPageLoading.value = true
    try {
      const ep = await api(`/episodes/${epId}?page=${nextPage}&pageSize=${shotPageSize}`)
      if (token !== shotPageLoadToken || activeEpisode.value?.id !== epId) return
      const normalized = normalizeEpisode(ep)
      const localById = new Map((activeEpisode.value.shots || []).map(s => [s.id, s]))
      normalized.shots = normalized.shots.map(remote =>
        mergeShotPreservingDirty(localById.get(remote.id), remote),
      )
      for (const s of normalized.shots) {
        if (!dirtyShotIds.value.has(s.id)) captureShotBaseline(s)
      }
      activeEpisode.value = normalized
      shotTotal.value = normalized.shotTotal ?? normalized.shots.length
      const maxPage = Math.max(1, Math.ceil(shotTotal.value / shotPageSize) || 1)
      const clamped = Math.min(nextPage, maxPage)
      if (shotPage.value !== clamped) {
        suppressShotPageWatch = true
        shotPage.value = clamped
        suppressShotPageWatch = false
      }
      syncEpisode()
      syncShotGenPolls()
      if (clamped !== nextPage) {
        await loadShotPage(clamped, { force: true })
      }
    } catch (e: any) {
      if (token === shotPageLoadToken) error.value = e.message
    } finally {
      if (token === shotPageLoadToken) shotPageLoading.value = false
    }
  }

  async function hydrateProject(
    id: number,
    opts?: { tab?: 'scripts' | 'episodes' | 'resources'; episodeNumber?: number },
  ) {
    const token = ++hydrateToken
    projectLoading.value = true
    view.value = 'studio'
    projectMenuOpen.value = false
    if (active.value?.id !== id) {
      clearTransientImageGenState()
      resetLibraryPaging()
      active.value = projectShell(id)
      activeEpisode.value = null
      projectHydrated.value = false
    } else if (!active.value) {
      active.value = projectShell(id)
      projectHydrated.value = false
    }
    studioTab.value = opts?.tab === 'resources' ? 'resources' : opts?.tab === 'episodes' ? 'episodes' : 'scripts'
    try {
      const raw = await api(`/projects/${id}`)
      if (token !== hydrateToken) return
      active.value = normalizeProject(raw)
      hydrateRecentShotRefs(active.value)
      studioTab.value = opts?.tab === 'resources' ? 'resources' : opts?.tab === 'episodes' ? 'episodes' : 'scripts'
      selectEpisodeByNumber(opts?.episodeNumber)
      projectHydrated.value = true
      const pageFromRoute = Number(route.query.page)
      const initialShotPage = Number.isFinite(pageFromRoute) && pageFromRoute >= 1 ? Math.floor(pageFromRoute) : 1
      suppressShotPageWatch = true
      shotPage.value = initialShotPage
      suppressShotPageWatch = false
      clearAllShotDirty()
      void loadShotPage(initialShotPage, { force: true })
      void loadResourceTrash({ resetPage: true })
      void resumeImageGenerationJobs(id)
      syncShotGenPolls()
      void loadLibraryPage({ resetPage: true })
    } catch (e) {
      if (token === hydrateToken) {
        projectHydrated.value = false
        throw e
      }
    } finally {
      if (token === hydrateToken) projectLoading.value = false
    }
  }

  const refreshingStudio = ref(false)
  async function refreshStudio() {
    if (!active.value) return
    const id = active.value.id
    const episodeNumber = activeEpisode.value?.number
    const tab = studioTab.value
    refreshingStudio.value = true
    error.value = ''
    try {
      await hydrateProject(id, { tab, episodeNumber })
      await refreshProjectResources()
    } catch (e: any) {
      error.value = e?.message || '刷新失败'
    } finally {
      refreshingStudio.value = false
    }
  }

  async function openProject(id: number) {
    error.value = ''
    view.value = 'studio'
    projectMenuOpen.value = false
    studioTab.value = 'scripts'
    // Enter studio immediately with a shell; fetch in the background.
    if (active.value?.id !== id) {
      clearTransientImageGenState()
      resetLibraryPaging()
      active.value = projectShell(id)
      activeEpisode.value = null
      projectHydrated.value = false
    }
    projectLoading.value = true
    await navigateTo(studioPath({ tab: 'scripts' }))
    try {
      await hydrateProject(id, { tab: 'scripts' })
    } catch (e: any) {
      error.value = e?.message || '项目不存在或无法打开'
      projectLoading.value = false
      await goHome()
    }
  }

  async function goHome() {
    hydrateToken++
    clearTransientImageGenState()
    resetLibraryPaging()
    projectLoading.value = false
    projectHydrated.value = false
    view.value = 'studio'
    active.value = null
    activeEpisode.value = null
    recentShotRefsStored.value = []
    projectMenuOpen.value = false
    await navigateTo('/')
  }
  function resetCreateForm() {
    form.value = {
      title: '',
      episodeCount: 1,
      style: '',
      videoRatio: '16:9',
      kind: 'script',
      genre: '',
      synopsis: '',
      visualManual: '',
      directorManual: '',
      storyboardPace: 'fine',
    }
  }
  async function create() {
    error.value = ''
    if (!form.value.title.trim()) { error.value = '请先给作品取个名字'; return }
    try {
      const p = await api('/projects', {
        method: 'POST',
        body: JSON.stringify({
          title: form.value.title,
          kind: form.value.kind || 'script',
          genre: form.value.genre,
          synopsis: form.value.synopsis,
          visualManual: form.value.visualManual,
          directorManual: form.value.directorManual,
          style: form.value.style,
          videoRatio: form.value.videoRatio,
          storyboardPace: form.value.storyboardPace || 'fine',
          episodeCount: 1,
        }),
      })
      projects.value = await api('/projects')
      await openProject(p.id)
      resetCreateForm()
    } catch (e: any) { error.value = e.message }
  }
  async function saveProject() {
    if (!active.value) return
    saving.value = true
    try {
      await api(`/projects/${active.value.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          title: active.value.title,
          kind: active.value.kind || 'script',
          genre: active.value.genre || '',
          synopsis: active.value.synopsis || '',
          visualManual: active.value.visualManual || '',
          directorManual: active.value.directorManual || '',
          style: active.value.style,
          videoRatio: active.value.videoRatio || '16:9',
          storyboardPace: active.value.storyboardPace || 'fine',
        }),
      })
      projects.value = await api('/projects')
    } catch (e: any) { error.value = e.message } finally { saving.value = false }
  }
  async function addShot(insertAt?: number) {
    if (!activeEpisode.value) return
    if (typeof insertAt !== 'number' || !Number.isFinite(insertAt)) insertAt = undefined
    if (!activeEpisode.value.shots) activeEpisode.value.shots = []
    error.value = ''
    const payload: { script: string; insertAt?: number } = { script: '' }
    if (insertAt !== undefined) payload.insertAt = insertAt
    try {
      const shot = await api(`/episodes/${activeEpisode.value.id}/shots`, { method: 'POST', body: JSON.stringify(payload) })
      const normalized = normalizeShot(shot)
      shotTotal.value += 1
      const targetIndex = insertAt !== undefined ? insertAt : Math.max(0, shotTotal.value - 1)
      const targetPage = Math.floor(targetIndex / shotPageSize) + 1
      expandShot(normalized.id)
      await loadShotPage(targetPage, { force: true })
    } catch (e: any) {
      error.value = e.message || '添加分镜失败'
    }
  }
  async function moveShot(shot: Shot, delta: -1 | 1) {
    if (!activeEpisode.value?.shots?.length) return
    error.value = ''
    try {
      const result = await api(`/shots/${shot.id}/move`, {
        method: 'PUT',
        body: JSON.stringify({ delta }),
      })
      if (result?.moved === false) return
      const sortOrder = typeof result?.sortOrder === 'number'
        ? result.sortOrder
        : (result?.shot?.sortOrder ?? shot.sortOrder)
      const targetPage = Math.floor(Math.max(0, sortOrder - 1) / shotPageSize) + 1
      await loadShotPage(targetPage, { force: true })
    } catch (e: any) {
      error.value = e.message || '调整分镜顺序失败'
    }
  }
  function isShotExpanded(shotId: number) {
    if (generating.value === shotId || uploadingShot.value === shotId) return true
    return expandedShots.value.has(shotId)
  }
  function toggleShotExpand(shotId: number) {
    const next = new Set(expandedShots.value)
    if (next.has(shotId)) next.delete(shotId)
    else next.add(shotId)
    expandedShots.value = next
  }
  function expandShot(shotId: number) {
    const next = new Set(expandedShots.value)
    next.add(shotId)
    expandedShots.value = next
  }
  async function saveShot(shot: Shot, opts?: { replace?: boolean }) {
    if (skipSaveShotIds.value.has(shot.id)) return shot
    const snapshot = {
      label: shot.label || '',
      script: shot.script,
      note: shot.note || '',
      visualStyle: shot.visualStyle,
      duration: shot.duration,
      resolution: shot.resolution,
      videoModelId: shot.videoModelId || null,
      positioningPrompt: shot.positioningPrompt || '',
      refsJson: JSON.stringify(shot.refs || []),
      positioningRefsJson: JSON.stringify(shot.positioningRefs || []),
      motionGridPrompt: shot.motionGridPrompt || '',
      motionGridRefsJson: JSON.stringify(shot.motionGridRefs || []),
    }
    const updated = await api(`/shots/${shot.id}`, { method: 'PUT', body: JSON.stringify({
      label: snapshot.label, script: snapshot.script, note: snapshot.note, visualStyle: snapshot.visualStyle, imageRefs: shot.imageRefs,
      refs: shot.refs, duration: snapshot.duration, resolution: snapshot.resolution,
      videoModelId: snapshot.videoModelId,
      positioningPrompt: snapshot.positioningPrompt,
      positioningRefs: shot.positioningRefs || [],
      motionGridPrompt: snapshot.motionGridPrompt,
      motionGridRefs: shot.motionGridRefs || [],
    }) })
    const current = activeEpisode.value?.shots?.find(s => s.id === shot.id)
    const stillEditing = !!(current && (
      current.script !== snapshot.script
      || current.note !== snapshot.note
      || current.label !== snapshot.label
      || current.visualStyle !== snapshot.visualStyle
      || current.duration !== snapshot.duration
      || current.resolution !== snapshot.resolution
      || (current.videoModelId || null) !== snapshot.videoModelId
      || (current.positioningPrompt || '') !== snapshot.positioningPrompt
      || (current.motionGridPrompt || '') !== snapshot.motionGridPrompt
      || JSON.stringify(current.refs || []) !== snapshot.refsJson
      || JSON.stringify(current.positioningRefs || []) !== snapshot.positioningRefsJson
      || JSON.stringify(current.motionGridRefs || []) !== snapshot.motionGridRefsJson
    ))
    if (!stillEditing) clearShotDirty(shot.id)
    else markShotDirty(shot.id)
    const saved = normalizeShot((updated as any)?.id ? updated : (updated as any)?.shot ?? updated)
    if (opts?.replace !== false) {
      const local = activeEpisode.value?.shots?.find(s => s.id === shot.id)
      if (stillEditing && local) replaceShot(mergeShotPreservingDirty(local, saved))
      else replaceShot(saved)
    } else if (typeof saved.script === 'string' && saved.script !== snapshot.script) {
      clearShotDirty(shot.id)
      replaceShot(saved)
    }
    applyPackedShots(updated, { preserveDirtyIds: [shot.id] })
    if (!stillEditing) {
      captureShotBaseline(saved)
      clearShotSessionBaseline(shot.id)
    }
    return saved
  }
  async function removeShot(shot: Shot) {
    if (!confirm('确定删除这个分镜？')) return
    await api(`/shots/${shot.id}`, { method: 'DELETE' })
    clearShotDirty(shot.id)
    shotTotal.value = Math.max(0, shotTotal.value - 1)
    const maxPage = Math.max(1, Math.ceil(shotTotal.value / shotPageSize) || 1)
    const page = Math.min(shotPage.value, maxPage)
    await loadShotPage(page, { force: true })
  }
  async function generateShot(shot: Shot) {
    if (!shot.script.trim()) { error.value = '请先填写分镜描述'; return }
    if (!shot.refs.length) { error.value = '请添加至少一张参考图（角色或场景）'; return }
    expandShot(shot.id)
    generating.value = shot.id
    error.value = ''
    replaceShot(normalizeShot({ ...shot, status: 'generating', errorMessage: '', videoEta: '' }))
    notifyStudioSync({ type: 'shot', projectId: active.value?.id, shotId: shot.id, status: 'generating' })
    // Poll while the long POST runs — status can flip to done via server resume after restart.
    const poll = window.setInterval(() => { void refreshShotFromServer(shot.id) }, 3000)
    try {
      await saveShot(shot, { replace: false })
      const r = await fetch(`/api/shots/${shot.id}/generate`, { method: 'POST', headers: { 'Content-Type': 'application/json' } })
      const raw = await r.text()
      let data: any = {}
      try { data = raw ? JSON.parse(raw) : {} } catch { throw new Error('后端服务不可用，请确认后端已启动') }
      if (!r.ok) {
        // POST may fail after restart while resume already finished the shot.
        await refreshShotFromServer(shot.id)
        const current = activeEpisode.value?.shots?.find(s => s.id === shot.id)
        if (current?.status === 'done') {
          if (data.archivedResource && active.value) mergeNewResources([data.archivedResource])
          if (data.videoResource && active.value) mergeNewResources([data.videoResource])
          return
        }
        if (data.shot) replaceShot(normalizeShot(data.shot))
        else {
          const fallback = current || shot
          replaceShot(normalizeShot({ ...fallback, status: 'error', errorMessage: data.error || `请求失败（HTTP ${r.status}）` }))
        }
        throw new Error(data.error || `请求失败（HTTP ${r.status}）`)
      }
      const updated = normalizeShot(data.shot ?? data)
      replaceShot(updated)
      if (data.archivedResource && active.value) mergeNewResources([data.archivedResource])
      if (data.videoResource && active.value) mergeNewResources([data.videoResource])
      if (active.value && (data.archivedResource || data.videoResource)) await refreshProjectResources()
    } catch (e: any) {
      await refreshShotFromServer(shot.id)
      const current = activeEpisode.value?.shots?.find(s => s.id === shot.id)
      if (current?.status === 'done') {
        // Backend finished (possibly via resume); suppress transient client/network errors.
        return
      }
      error.value = e.message
    } finally {
      window.clearInterval(poll)
      generating.value = null
      // Re-sync from server so a completed download isn't stuck as「生成中」after client timeout.
      await refreshShotFromServer(shot.id)
      notifyStudioSync({ type: 'shot', projectId: active.value?.id, shotId: shot.id, status: 'settled' })
    }
  }

  async function refreshShotFromServer(shotId: number) {
    if (!active.value) return
    try {
      const raw = await api(`/shots/${shotId}`)
      const latest = normalizeShot(raw)
      const hostEp = active.value.episodes.find(ep => ep.id === latest.episodeId)
        || (activeEpisode.value?.id === latest.episodeId ? activeEpisode.value : null)
      if (activeEpisode.value?.id === latest.episodeId) {
        const local = activeEpisode.value.shots.find(s => s.id === shotId)
        if (local) {
          replaceShot(mergeShotPreservingDirty(local, latest))
        }
      }
      if (hostEp && active.value) {
        const idx = active.value.episodes.findIndex(e => e.id === hostEp.id)
        if (idx >= 0 && activeEpisode.value?.id === hostEp.id) {
          active.value.episodes[idx] = {
            ...active.value.episodes[idx],
            shots: activeEpisode.value.shots,
            shotTotal: shotTotal.value,
          }
        }
      }
    } catch {
      // ignore sync errors
    }
  }
  async function onShotVideoFile(shot: Shot, e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file || !active.value) return
    expandShot(shot.id)
    uploadingShot.value = shot.id
    error.value = ''
    try {
      if (await cosEnabled()) {
        const presign = await api(`/shots/${shot.id}/direct-upload-video`, {
          method: 'POST',
          body: JSON.stringify({ filename: file.name, contentType: file.type || 'video/mp4' }),
        }) as CosPresign & { ext: string }
        await putFileToCos(presign, file)
        const data = await api(`/shots/${shot.id}/confirm-video`, {
          method: 'POST',
          body: JSON.stringify({ ext: presign.ext, key: presign.key }),
        })
        replaceShot(normalizeShot(data.shot ?? data))
        if (data.archivedResource) mergeNewResources([data.archivedResource])
        if (data.videoResource) mergeNewResources([data.videoResource])
        uploadingShot.value = null
        if (active.value && (data.archivedResource || data.videoResource)) await refreshProjectResources()
        return
      }
      const ctrl = new AbortController()
      const timer = window.setTimeout(() => ctrl.abort(), 3 * 60 * 1000)
      try {
        const fd = new FormData()
        fd.append('video', file)
        const r = await fetch(`/api/shots/${shot.id}/upload-video`, { method: 'POST', body: fd, signal: ctrl.signal })
        const raw = await r.text()
        let data: any = {}
        try { data = raw ? JSON.parse(raw) : {} } catch { throw new Error('后端服务不可用，请确认后端已启动') }
        if (!r.ok) throw new Error(data.error || `请求失败（HTTP ${r.status}）`)
        replaceShot(normalizeShot(data.shot ?? data))
        if (data.archivedResource) mergeNewResources([data.archivedResource])
        if (data.videoResource) mergeNewResources([data.videoResource])
        uploadingShot.value = null
        if (active.value && (data.archivedResource || data.videoResource)) await refreshProjectResources()
      } finally {
        window.clearTimeout(timer)
      }
    } catch (e: any) {
      if (e?.name === 'AbortError') error.value = '上传超时（大视频同步云存储可能较慢），请稍后重试'
      else error.value = e.message
    } finally {
      uploadingShot.value = null
      input.value = ''
    }
  }
  async function previewShotPrompt(shot: Shot) {
    previewingPrompt.value = shot.id
    error.value = ''
    try {
      await saveShot(shot)
      const data = await api(`/shots/${shot.id}/prompt-preview`)
      promptPreview.value = { shotId: shot.id, ...data }
    } catch (e: any) { error.value = e.message }
    finally { previewingPrompt.value = null }
  }

  /** Explicit `场景：…` lines from script (last one wins for ranking). */
  function extractShotSceneHints(text: string): string[] {
    const hints: string[] = []
    for (const m of text.matchAll(/(?:^|\n)\s*\**\s*场景\s*[：:]\s*([^\n]+)/g)) {
      let raw = String(m[1] || '')
        .replace(/\*+/g, '')
        .replace(/[【\[】\]]/g, '')
        .replace(/[，,]\s*UE5[\s\S]*$/i, '')
        .replace(/\s*UE5[\s\S]*$/i, '')
        .trim()
      if (!raw) continue
      for (const part of raw.split(/[/／|、]/).map(s => s.trim()).filter(Boolean)) {
        const cleaned = cleanRefAlias(part.replace(/[，,].*$/, '').trim())
        if (cleaned.length >= 2 && cleaned.length <= 40) hints.push(cleaned)
      }
    }
    return hints
  }

  /** Pure arena / stage names — not corridors / exits that merely contain 擂台. */
  function isPureArenaSceneName(name: string): boolean {
    const n = normalizeEntityName(name)
    if (!n) return false
    if (/离场|长廊|走廊|侧廊|通道|回廊|巷/.test(n)) return false
    return /^(比试)?擂台$|^演武[台场]$|^比武[台场]$|^演武$/.test(n)
  }

  /** Suffixes that turn an entity name into a different place/attribute (小七→小七识海). */
  const COMPOUND_PLACE_SUFFIX_RE =
    /^(识海|识界|梦境|居所|内室|卧室|离场|长廊|走廊|侧廊|通道|回廊|府邸|洞府|秘境|空间|领域|世界|幻境|魂海|意识海)/u

  /**
   * Higher = better match. Blocks weak hits like 擂台 → 擂台离场长廊 when
   * the script scene line says 比试擂台 / 演武台; and 小七 → 小七识海.
   */
  function scoreResourceNameAgainstText(
    baseRaw: string,
    haystack: string,
    mentions: string[],
    sceneHints: string[],
  ): number {
    const base = cleanRefAlias(baseRaw)
    const core = normalizeEntityName(base)
    if (core.length < 2) return 0

    let best = 0
    const latestHint = sceneHints.length ? sceneHints[sceneHints.length - 1] : ''
    const latestHintCore = normalizeEntityName(latestHint)

    // Explicit scene line match (strongest for scenes)
    for (const hint of sceneHints) {
      const h = normalizeEntityName(hint)
      if (!h) continue
      if (core === h || base === hint) best = Math.max(best, 1200 + core.length * 10)
      else if (core.includes(h) || h.includes(core)) {
        // Avoid 擂台 ⊂ 擂台离场长廊 via short scene-token alone
        const ratio = Math.min(core.length, h.length) / Math.max(core.length, h.length)
        if (ratio >= 0.5) best = Math.max(best, 1000 + Math.min(core.length, h.length) * 10)
      } else if (isPureArenaSceneName(hint) && isPureArenaSceneName(base)) {
        best = Math.max(best, 950 + core.length * 5)
      }
    }

    // Full resource name appears in text
    if (haystack.includes(base) || haystack.includes(core)) {
      best = Math.max(best, 900 + core.length * 10)
    }
    // Stem match: 「5米长的赤鳞蜈蚣」↔ 文案「赤鳞蜈蚣」；「拳头大小的小七」↔「小七」
    const stem = entityStem(base)
    if (stem.length >= 2 && stem !== core && haystack.includes(stem)) {
      best = Math.max(best, 880 + stem.length * 10)
    }

    for (const mention of mentions) {
      if (mention.length < 2) continue
      const m = normalizeEntityName(mention)
      if (m.length < 2) continue
      if (core === m || base === mention) {
        best = Math.max(best, 850 + m.length * 10)
        continue
      }
      if (m.includes(core) && core.length >= 2) {
        best = Math.max(best, 800 + core.length * 10)
        continue
      }
      if (core.startsWith(m) || base.startsWith(mention)) {
        const tail = core.slice(m.length)
        // 小七 + 识海 → different entity; do not treat as near-match
        if (COMPOUND_PLACE_SUFFIX_RE.test(tail)) continue
        const extra = tail.length
        if (extra <= 1) best = Math.max(best, 700 + m.length * 10)
        else if (extra <= 2 && !/离场|长廊|走廊|识海|居所|内室/.test(tail)) {
          best = Math.max(best, 450 + m.length * 8)
        }
        continue
      }
      // Resource contains short mention (擂台 ⊂ 擂台离场长廊) — require coverage
      if (core.includes(m) || base.includes(mention)) {
        const ratio = m.length / core.length
        if (ratio >= 0.6) best = Math.max(best, 500 + m.length * 10)
        else if (ratio >= 0.45 && m.length >= 3) best = Math.max(best, 220 + m.length * 5)
        // else reject: e.g. 擂台 → 擂台离场长廊
      }
    }

    for (const part of base.split(/[+／/、,，\s]+/).map(s => s.trim()).filter(s => s.length >= 2)) {
      const partCore = normalizeEntityName(part)
      if (haystack.includes(part) || (partCore.length >= 2 && haystack.includes(partCore))) {
        best = Math.max(best, 600 + partCore.length * 8)
      }
    }

    // Penalize corridor/exit / 识海 scenes when text only mentions the entity stem
    if (
      latestHintCore
      && isPureArenaSceneName(latestHint)
      && /离场|长廊|走廊|侧廊|通道|回廊/.test(core)
      && !haystack.includes(core)
      && !sceneHints.some(h => normalizeEntityName(h).includes('离场') || normalizeEntityName(h).includes('长廊'))
    ) {
      best = Math.min(best, 80)
    }

    return best
  }

  /** Prefer angle / frame labels for grid cells when matching by text. */
  function libraryMatchBaseName(r: Resource): string {
    if (r.genType === 'scene_grid_cell') {
      return sceneGridCellRefLabel(r) || cleanRefAlias(resourceEditableName(r) || r.name)
    }
    if (r.genType === 'motion_grid_cell' && r.gridCell) {
      return `帧${r.gridCell}`
    }
    return cleanRefAlias(resourceEditableName(r) || r.name)
  }

  type LibraryNameMatch = {
    resource: Resource
    base: string
    type: Resource['type']
    score: number
  }

  /** Shared library name matching used by video shot refs and positioning refs. */
  function matchLibraryResourcesByText(haystackRaw: string): LibraryNameMatch[] {
    const haystack = (haystackRaw || '').trim()
    if (!haystack) return []

    const mentionCandidates = extractShotMentionNames(haystack)
    const sceneHints = extractShotSceneHints(haystack)
    const resources = (active.value?.resources || []).filter(r => {
      if (r.type === 'video') return false
      if (isPositioningLikeResource(r)) return false
      if (r.genType === 'scene_grid' || r.genType === 'motion_grid') return false
      if (r.genType === 'scene_reverse' || r.genType === 'scene_reverse_skeleton') return false
      return !!(r.imageUrl || r.stylizedImageUrl)
    })

    type NameGroup = { base: string; type: Resource['type']; items: Resource[] }
    const groups = new Map<string, NameGroup>()
    for (const r of resources) {
      const base = libraryMatchBaseName(r)
      if (base.length < 2) continue
      const key = `${r.type}:${base}:${r.genType || ''}:${r.gridId || 0}`
      const g = groups.get(key)
      if (g) g.items.push(r)
      else groups.set(key, { base, type: r.type, items: [r] })
    }

    const typeOrder = (t: Resource['type']) => (
      t === 'scene' ? 0 : t === 'character' ? 1 : t === 'prop' ? 2 : 3
    )

    const scored: LibraryNameMatch[] = []
    for (const g of groups.values()) {
      const best = pickBestLibraryResource(g.items)
      if (!best) continue
      let score = scoreLibraryResourceAgainstText(best, g.base, haystack, mentionCandidates, sceneHints)
      if (score < 200) continue
      // Prefer recently used assets when ranking auto-picked refs
      const recentIdx = recentResourceIndex(best.id)
      if (recentIdx >= 0) score += Math.max(0, 80 - recentIdx)
      scored.push({ resource: best, base: g.base, type: g.type, score })
    }

    return dedupeOverlappingNameMatches(scored).sort((a, b) => {
      if (b.score !== a.score) return b.score - a.score
      const ra = recentResourceIndex(a.resource.id)
      const rb = recentResourceIndex(b.resource.id)
      const aRecent = ra >= 0
      const bRecent = rb >= 0
      if (aRecent !== bRecent) return aRecent ? -1 : 1
      if (aRecent && bRecent && ra !== rb) return ra - rb
      const od = typeOrder(a.type) - typeOrder(b.type)
      if (od !== 0) return od
      return b.base.length - a.base.length
    })
  }

  /** Broader candidate pool for LLM disambiguation (includes 小七识海 when text has 小七). */
  function collectLibraryMatchCandidates(haystackRaw: string): LibraryNameMatch[] {
    const haystack = (haystackRaw || '').trim()
    if (!haystack) return []
    const mentions = extractShotMentionNames(haystack)
    const sceneHints = extractShotSceneHints(haystack)
    const resources = (active.value?.resources || []).filter(r => {
      if (r.type === 'video') return false
      if (isPositioningLikeResource(r)) return false
      if (r.genType === 'scene_grid' || r.genType === 'motion_grid') return false
      if (r.genType === 'scene_reverse' || r.genType === 'scene_reverse_skeleton') return false
      return !!(r.imageUrl || r.stylizedImageUrl)
    })

    type NameGroup = { base: string; type: Resource['type']; items: Resource[] }
    const groups = new Map<string, NameGroup>()
    for (const r of resources) {
      const base = libraryMatchBaseName(r)
      if (base.length < 2) continue
      const key = `${r.type}:${base}:${r.genType || ''}:${r.gridId || 0}`
      const g = groups.get(key)
      if (g) g.items.push(r)
      else groups.set(key, { base, type: r.type, items: [r] })
    }

    const softRelated = (base: string): boolean => {
      const core = normalizeEntityName(base)
      const stem = entityStem(base)
      if (core.length < 2 && stem.length < 2) return false
      if (haystack.includes(base) || haystack.includes(core) || (stem.length >= 2 && haystack.includes(stem))) return true
      for (const hint of sceneHints) {
        const h = normalizeEntityName(hint)
        if (h && (core.includes(h) || h.includes(core) || core === h || stem === h || stem.includes(h) || h.includes(stem))) return true
      }
      for (const mention of mentions) {
        if (mention.length < 2) continue
        const m = normalizeEntityName(mention)
        const ms = entityStem(mention)
        if (m.length < 2 && ms.length < 2) continue
        if (core === m || stem === m || stem === ms || core.includes(m) || m.includes(core)
          || (stem.length >= 2 && (stem.includes(ms) || ms.includes(stem)))) {
          return true
        }
      }
      return false
    }

    const scored: LibraryNameMatch[] = []
    for (const g of groups.values()) {
      const best = pickBestLibraryResource(g.items)
      if (!best) continue
      let score = scoreLibraryResourceAgainstText(best, g.base, haystack, mentions, sceneHints)
      if (score < 150 && !softRelated(g.base) && !softRelated(shortGridSceneBase(best.name))) continue
      const recentIdx = recentResourceIndex(best.id)
      if (recentIdx >= 0) score += Math.max(0, 80 - recentIdx)
      scored.push({ resource: best, base: g.base, type: g.type, score: Math.max(score, 50) })
    }

    return [...scored]
      .sort((a, b) => {
        if (b.score !== a.score) return b.score - a.score
        const ra = recentResourceIndex(a.resource.id)
        const rb = recentResourceIndex(b.resource.id)
        if (ra >= 0 || rb >= 0) {
          if (ra < 0) return 1
          if (rb < 0) return -1
          if (ra !== rb) return ra - rb
        }
        return b.base.length - a.base.length
      })
      .slice(0, 40)
  }

  function shotRefFromLibraryMatch(item: LibraryNameMatch): ShotRef | null {
    const kind = (item.resource.type === 'character' || item.resource.type === 'scene'
      || item.resource.type === 'prop' || item.resource.type === 'other')
      ? item.resource.type
      : null
    if (!kind) return null
    const variant = preferredShotRefVariant(item.resource)
    const identity = item.resource.genType === 'scene_grid_cell'
      ? sceneGridCellRefLabel(item.resource)
      : (resourceIdentityName(item.resource) || item.base)
    return kind === 'prop'
      ? { kind: 'prop', id: item.resource.id, label: identity }
      : { kind, id: item.resource.id, variant: variant || 'stylized', label: identity }
  }

  /** Ask lite text model to pick refs from a compact candidate list; falls back to local scoring. */
  /** Refs from the previous N shots whose entity stems still appear in the current script. */
  function previousShotsMatchingRefs(shot: Shot, scriptText: string, maxPrev = 10): ShotRef[] {
    const haystack = (scriptText || '').trim()
    if (!haystack || !activeEpisode.value?.shots) return []
    const priors = (activeEpisode.value.shots || [])
      .filter(s => s.id !== shot.id && (s.sortOrder < shot.sortOrder || (s.sortOrder === shot.sortOrder && s.id < shot.id)))
      .sort((a, b) => b.sortOrder - a.sortOrder || b.id - a.id)
      .slice(0, maxPrev)

    const out: ShotRef[] = []
    const usedIds = new Set<number>()
    for (const prev of priors) {
      for (const ref of prev.refs || []) {
        if (usedIds.has(ref.id)) continue
        const r = resourceById(ref.id)
        if (!r || isPositioningLikeResource(r)) continue
        if (!(r.imageUrl || r.stylizedImageUrl)) continue
        // Skip whole grids / motion grids — cells and characters/props/scenes only
        if (r.genType === 'scene_grid' || r.genType === 'motion_grid') continue
        const label = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
        if (label.length < 2) continue
        const stem = entityStem(label)
        const core = normalizeEntityName(label)
        const sceneHit = r.type === 'scene' && scenePlaceTokens(shortGridSceneBase(label) || core).some(t => t.length >= 2 && haystack.includes(t))
        const hit = (stem.length >= 2 && haystack.includes(stem))
          || (core.length >= 2 && haystack.includes(core))
          || haystack.includes(label)
          || sceneHit
        if (!hit) continue
        usedIds.add(ref.id)
        out.push(withDefaultShotRefLabel({
          ...ref,
          label: label || ref.label,
        }))
      }
    }
    return out
  }

  async function autoPickShotRefsWithAI(shot: Shot, scriptText: string): Promise<number> {
    const haystack = (scriptText || shot.script || '').trim()
    if (!haystack) return shot.refs.length

    // Seed from previous shots first — user's manual picks on prior shots are the strongest continuity signal
    const prevRefs = previousShotsMatchingRefs(shot, haystack, 10)
    if (prevRefs.length) {
      shot.refs = collapseOverlappingShotRefs([...prevRefs, ...(shot.refs || [])])
    }

    const candidates = collectLibraryMatchCandidates(haystack)
    // Ensure current refs stay in the candidate set for the model to keep/drop.
    const byID = new Map(candidates.map(c => [c.resource.id, c]))
    for (const ref of shot.refs || []) {
      const r = resourceById(ref.id)
      if (!r || isPositioningLikeResource(r)) continue
      if (byID.has(r.id)) continue
      const base = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
      if (base.length < 2) continue
      byID.set(r.id, { resource: r, base, type: r.type, score: 60 })
    }
    // Also force-include previous-shot continuity refs into the model candidate pool
    for (const ref of prevRefs) {
      const r = resourceById(ref.id)
      if (!r || isPositioningLikeResource(r)) continue
      if (byID.has(r.id)) {
        const cur = byID.get(r.id)!
        cur.score = Math.max(cur.score, 900)
        continue
      }
      const base = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
      if (base.length < 2) continue
      byID.set(r.id, { resource: r, base, type: r.type, score: 900 })
    }
    for (const name of extractScriptCharacterNames(haystack)) {
      const r = findCharacterResourceForName(name)
      if (!r || isPositioningLikeResource(r)) continue
      const base = libraryMatchBaseName(r)
      const cur = byID.get(r.id)
      if (cur) {
        cur.score = Math.max(cur.score, 5000)
        continue
      }
      byID.set(r.id, { resource: r, base, type: r.type, score: 5000 })
    }
    pinSceneGridCellsIntoPool(byID, haystack)
    const pool = [...byID.values()]
      .sort((a, b) => b.score - a.score)
      .slice(0, 40)

    if (pool.length === 0) {
      return autoPickShotRefs(shot, scriptText)
    }

    try {
      const result = await api(`/shots/${shot.id}/match-refs`, {
        method: 'POST',
        body: JSON.stringify({
          script: haystack.length > 1800 ? `${haystack.slice(0, 1800)}…` : haystack,
          candidates: pool.map(c => ({
            id: c.resource.id,
            type: c.type,
            name: c.base,
          })),
        }),
      })
      const picks = Array.isArray(result?.refs) ? result.refs as { id?: number; label?: string }[] : []
      if (!picks.length) {
        return autoPickShotRefs(shot, scriptText)
      }

      const mentions = extractShotMentionNames(haystack)
      const sceneHints = extractShotSceneHints(haystack)
      // Start from already-seeded refs (incl. previous-shot continuity), then fill/replace
      const next: ShotRef[] = collapseOverlappingShotRefs([...(shot.refs || [])])
      const usedKeys = new Set(next.map(r => shotRefKey(r)))
      const usedIds = new Set(next.map(r => r.id))

      const existingLabel = (ref: ShotRef) =>
        cleanRefAlias(ref.label || '') || cleanRefAlias(resourceById(ref.id)?.name || '') || ''
      const overlapsExisting = (kind: ShotRef['kind'], label: string): boolean =>
        next.some(r => refDedupeGroup(r.kind) === refDedupeGroup(kind) && namesOverlapEntity(existingLabel(r), label))

      for (const pick of picks) {
        if (next.length >= maxShotRefs) break
        const id = Number(pick.id)
        if (!Number.isFinite(id)) continue
        const item = pool.find(c => c.resource.id === id)
        const raw = item?.resource || resourceById(id)
        if (!raw || isPositioningLikeResource(raw)) continue
        const r = preferRecentResourceForName(raw)
        if (!(r.imageUrl || r.stylizedImageUrl)) continue
        const base = cleanRefAlias(pick.label || '') || item?.base || cleanRefAlias(resourceEditableName(r) || r.name)
        const kind = (r.type === 'character' || r.type === 'scene' || r.type === 'prop' || r.type === 'other')
          ? r.type
          : null
        if (!kind) continue
        // Drop compound place when text only needs the stem (model sometimes still errs)
        const core = normalizeEntityName(base)
        const stemHit = mentions.some(m => {
          const nm = normalizeEntityName(m)
          return nm.length >= 2 && core.startsWith(nm) && COMPOUND_PLACE_SUFFIX_RE.test(core.slice(nm.length))
        })
        const fullInText = haystack.includes(base) || haystack.includes(core)
          || sceneHints.some(h => normalizeEntityName(h) === core)
        if (stemHit && !fullInText && kind === 'scene') continue

        const variant = preferredShotRefVariant(r)
        const shotRef: ShotRef = kind === 'prop'
          ? { kind: 'prop', id: r.id, label: base }
          : { kind, id: r.id, variant: variant || 'stylized', label: base }
        const keyed = withDefaultShotRefLabel(shotRef)
        const key = shotRefKey(keyed)
        if (usedKeys.has(key) || usedIds.has(keyed.id)) continue
        if (base && overlapsExisting(keyed.kind, base)) continue
        next.push(keyed)
        usedKeys.add(key)
        usedIds.add(keyed.id)
        rememberShotRef(keyed)
      }

      for (const name of extractScriptCharacterNames(haystack)) {
        const r = findCharacterResourceForName(name)
        if (!r) continue
        const shotRef = shotRefFromLibraryMatch({
          resource: r,
          base: resourceIdentityName(r) || libraryMatchBaseName(r) || name,
          type: 'character',
          score: 5000,
        })
        if (!shotRef) continue
        const keyed = withDefaultShotRefLabel(shotRef)
        if (usedKeys.has(shotRefKey(keyed)) || usedIds.has(keyed.id)) continue
        if (keyed.label && overlapsExisting(keyed.kind, keyed.label)) continue
        next.push(keyed)
        usedKeys.add(shotRefKey(keyed))
        usedIds.add(keyed.id)
        rememberShotRef(keyed)
      }

      // Keep library matches the model may have skipped — named people in the script all get a sheet
      for (const item of matchLibraryResourcesByText(haystack)) {
        if (next.length >= maxShotRefs) break
        if (item.type === 'character' && /杀手|路人|群演|群众|宾客|保镖|手下/.test(item.base)) continue
        const namedChar = item.type === 'character' && item.score >= 400
        if (!namedChar && item.score < 850) continue
        const shotRef = shotRefFromLibraryMatch(item)
        if (!shotRef) continue
        const keyed = withDefaultShotRefLabel(shotRef)
        if (usedKeys.has(shotRefKey(keyed)) || usedIds.has(keyed.id)) continue
        if (keyed.label && overlapsExisting(keyed.kind, keyed.label)) continue
        next.push(keyed)
        usedKeys.add(shotRefKey(keyed))
        usedIds.add(keyed.id)
        rememberShotRef(keyed)
      }

      // Prefer recently used refs that still match, filling remaining slots
      for (const ref of recentShotRefsMatchingText(haystack, mentions, sceneHints)) {
        if (next.length >= maxShotRefs) break
        const keyed = withDefaultShotRefLabel(ref)
        if (usedKeys.has(shotRefKey(keyed)) || usedIds.has(keyed.id)) continue
        if (keyed.label && overlapsExisting(keyed.kind, keyed.label)) continue
        next.push(keyed)
        usedKeys.add(shotRefKey(keyed))
        usedIds.add(keyed.id)
        rememberShotRef(keyed)
      }

      shot.refs = pinScriptCharacterRefs(next, haystack)
      ensureSceneGridCellRefs(shot, haystack)
      trimCrowdCharacterRefs(shot, haystack)
      return shot.refs.length
    } catch {
      return autoPickShotRefs(shot, scriptText)
    }
  }

  /** Keep existing shot.refs; append library matches; replace stale scenes after optimize. */
  function autoPickShotRefs(shot: Shot, scriptText: string): number {
    const haystack = (scriptText || shot.script || '').trim()
    if (!haystack) return shot.refs.length

    const prevRefs = previousShotsMatchingRefs(shot, haystack, 10)
    if (prevRefs.length) {
      shot.refs = collapseOverlappingShotRefs([...prevRefs, ...(shot.refs || [])])
    }

    const mentions = extractShotMentionNames(haystack)
    const sceneHints = extractShotSceneHints(haystack)
    const libraryMatches = matchLibraryResourcesByText(haystack)

    const existingLabel = (ref: ShotRef) =>
      cleanRefAlias(ref.label || '') || cleanRefAlias(resourceById(ref.id)?.name || '') || ''

    const labelScore = (ref: ShotRef) =>
      scoreResourceNameAgainstText(existingLabel(ref), haystack, mentions, sceneHints)

    // Drop stale refs (esp. old 场景) when script now points at a better library match
    const next = collapseOverlappingShotRefs([...(shot.refs || [])]).filter((ref) => {
      const score = labelScore(ref)
      if (score >= 300) return true
      const rival = libraryMatches.find(m => m.type === ref.kind && m.score >= 400 && m.score > score + 100)
      if (rival) return false
      if (ref.kind === 'scene' && score < 250 && libraryMatches.some(m => m.type === 'scene' && m.score >= 400)) {
        return false
      }
      return true
    })
    const usedKeys = new Set(next.map(r => shotRefKey(r)))
    const usedIds = new Set(next.map(r => r.id))

    const overlapsExisting = (kind: ShotRef['kind'], label: string): boolean =>
      next.some(r => refDedupeGroup(r.kind) === refDedupeGroup(kind) && namesOverlapEntity(existingLabel(r), label))

    const pushRef = (ref: ShotRef | null): boolean => {
      if (!ref) return false
      if (next.length >= maxShotRefs) return false
      const r = resourceById(ref.id)
      if (!r || isPositioningLikeResource(r)) return false
      if (!(r.imageUrl || r.stylizedImageUrl)) return false
      const keyed = withDefaultShotRefLabel(ref)
      const key = shotRefKey(keyed)
      if (usedKeys.has(key) || usedIds.has(keyed.id)) return false
      const label = cleanRefAlias(keyed.label || '') || cleanRefAlias(r.name)
      if (label && overlapsExisting(keyed.kind, label)) return false
      next.push(keyed)
      usedKeys.add(key)
      usedIds.add(keyed.id)
      rememberShotRef(keyed)
      return true
    }

    for (const name of extractScriptCharacterNames(haystack)) {
      const r = findCharacterResourceForName(name)
      if (!r) continue
      pushRef(shotRefFromLibraryMatch({
        resource: r,
        base: resourceIdentityName(r) || libraryMatchBaseName(r) || name,
        type: 'character',
        score: 5000,
      }))
    }

    // Prefer recently used refs that still match the script, then broader library matches
    for (const ref of recentShotRefsMatchingText(haystack, mentions, sceneHints)) {
      pushRef(ref)
      if (next.length >= maxShotRefs) break
    }

    for (const item of libraryMatches) {
      pushRef(shotRefFromLibraryMatch(item))
      if (next.length >= maxShotRefs) break
    }

    shot.refs = pinScriptCharacterRefs(next, haystack)
    ensureSceneGridCellRefs(shot, haystack)
    trimCrowdCharacterRefs(shot, haystack)
    return shot.refs.length
  }

  function normalizeEntityName(name: string): string {
    return cleanRefAlias(name)
      .replace(/^(手绘的|手绘|写实的|写实)/u, '')
      .replace(/\s+/g, '')
      .trim()
  }

  /**
   * Strip size/measure prefixes so 「指甲盖大小的小七」「拳头大小的小七」「5米长的赤鳞蜈蚣」
   * collapse to the same entity stem for matching / dedupe.
   */
  function entityStem(name: string): string {
    let s = normalizeEntityName(name)
    if (!s) return ''
    // 反复剥前缀：5米长的 / 拳头大小的 / 指甲盖大小的 / 手掌般大的 …
    for (let i = 0; i < 3; i++) {
      const next = s
        .replace(/^\d+(?:\.\d+)?\s*(?:米|厘米|mm|cm|m)\s*长的?/u, '')
        .replace(/^(?:指甲盖|拳头|手掌|巴掌|指尖|米粒|豆粒|拇指|花生米)(?:般|一样)?\s*大小的?/u, '')
        .replace(/^(?:约|大约|将近)?\s*\d+(?:\.\d+)?\s*(?:米|厘米)?\s*(?:长|高|大)?的?/u, '')
        .replace(/^(?:巨大的?|庞大的?|微小的?|迷你的?)/u, '')
      if (next === s) break
      s = next
    }
    return s.trim()
  }

  function namesOverlapEntity(a: string, b: string): boolean {
    const na = normalizeEntityName(a)
    const nb = normalizeEntityName(b)
    if (na.length < 2 || nb.length < 2) return false
    if (na === nb) return true
    if (na.includes(nb) || nb.includes(na)) return true
    const sa = entityStem(a)
    const sb = entityStem(b)
    if (sa.length >= 2 && sb.length >= 2) {
      if (sa === sb) return true
      if (sa.includes(sb) || sb.includes(sa)) return true
    }
    return false
  }

  /** 角色/其他 视为同一实体组做名称去重；场景独立（场景名常含角色名，如小七识海）。道具仍按自身类型。 */
  function refDedupeGroup(kind: ShotRef['kind']): string {
    if (kind === 'character' || kind === 'other') return 'entity'
    return kind
  }

  /** Same type + overlapping names (裁判长老 / 手绘的裁判长老) → keep one. Prefer higher score / entity stem over compound place. */
  function dedupeOverlappingNameMatches<T extends { base: string; type: Resource['type']; resource: Resource; score?: number }>(
    items: T[],
  ): T[] {
    const sorted = [...items].sort((a, b) => {
      if (a.type !== b.type) return 0
      const sa = a.score ?? 0
      const sb = b.score ?? 0
      if (sb !== sa) return sb - sa
      const na = normalizeEntityName(a.base)
      const nb = normalizeEntityName(b.base)
      // 小七 vs 小七识海：同前缀时优先更短的本体名
      if (na !== nb && (na.startsWith(nb) || nb.startsWith(na))) {
        const longer = na.length >= nb.length ? na : nb
        const shorter = na.length < nb.length ? na : nb
        const tail = longer.slice(shorter.length)
        if (COMPOUND_PLACE_SUFFIX_RE.test(tail) || /离场|长廊|走廊|居所|内室/.test(tail)) {
          return na.length - nb.length
        }
      }
      const score = (x: T) => {
        let s = x.base.length
        const recentIdx = recentResourceIndex(x.resource.id)
        if (recentIdx >= 0) s += 200 - Math.min(recentIdx, 50)
        if (x.resource.isGroupPrimary || !parseCandidateName(x.resource.name)) s += 100
        if (x.resource.stylizedImageUrl) s += 20
        if (/^手绘/.test(x.base)) s += 5
        // Prefer character/prop over scene when names collide across types — handled separately
        return s
      }
      return score(b) - score(a)
    })
    const kept: T[] = []
    for (const item of sorted) {
      if (kept.some(k => {
        if (k.type !== item.type) return false
        const kCell = k.resource.genType === 'scene_grid_cell'
        const iCell = item.resource.genType === 'scene_grid_cell'
        if (kCell && iCell) {
          return sceneGridAngleOf(k.resource) === sceneGridAngleOf(item.resource)
        }
        if (kCell !== iCell && (k.type === 'scene' || item.type === 'scene')) {
          // Keep both; ensureSceneGridCellRefs will drop the master plate when cells exist.
          return false
        }
        return namesOverlapEntity(k.base, item.base)
      })) continue
      kept.push(item)
    }
    return kept
  }

  /** Names mentioned in script for looser library matching (dialogue speakers, etc.). */
  function extractShotMentionNames(text: string): string[] {
    const set = new Set<string>()
    for (const m of text.matchAll(/「([^」：:]{1,24})[：:]/g)) {
      const n = m[1].trim()
      if (n) set.add(n)
    }
    for (const m of text.matchAll(/图\d+为\s*([^\n，,；;]+)/g)) {
      const n = cleanRefAlias(m[1] || '')
      if (n.length >= 2) set.add(n)
    }
    for (const hint of extractShotSceneHints(text)) set.add(hint)
    for (const m of text.matchAll(/([\u4e00-\u9fff]{2,12})[（(](?:左前|中前|右前|左中|中中|右中|左后|中后|右后)[）)]/g)) {
      const n = m[1].trim()
      if (n.length >= 2) set.add(n)
    }
    // Common role / prop / creature nouns that often appear without full resource names
    for (const noun of ['裁判', '长老', '观众', '演武场', '演武台', '比试擂台', '擂台', '灵镜', '令旗', '玉角鹿', '小七', '鹿', '蜈蚣', '赤鳞蜈蚣', '血无痕']) {
      if (text.includes(noun)) set.add(noun)
    }
    // Pull size-prefixed entity stems that appear in text (赤鳞蜈蚣 / 小七)
    for (const m of text.matchAll(/(?:指甲盖|拳头|手掌|巴掌)?(?:大小的?)?(\d+(?:\.\d+)?\s*米长的?)?([\u4e00-\u9fff]{2,12})/g)) {
      const stem = entityStem(m[0])
      if (stem.length >= 2) set.add(stem)
    }
    return [...set]
  }

  function extractScriptCharacterNames(haystack: string): string[] {
    const names = new Set<string>()
    for (const m of haystack.matchAll(/([\u4e00-\u9fff]{2,12})[（(](?:左前|中前|右前|左中|中中|右中|左后|中后|右后)[）)]/g)) {
      const n = (m[1] || '').trim()
      if (n.length >= 2 && !/杀手|路人|群演|群众|宾客|保镖|手下/.test(n)) names.add(n)
    }
    for (const m of haystack.matchAll(/([\u4e00-\u9fff]{2,12})\s*说[：:「]/g)) {
      const n = (m[1] || '').trim()
      if (n.length >= 2 && !/杀手|路人|群演|群众|宾客|保镖|手下/.test(n)) names.add(n)
    }
    return [...names]
  }

  function findCharacterResourceForName(name: string): Resource | null {
    const want = normalizeEntityName(name)
    if (want.length < 2) return null
    const chars = (active.value?.resources || []).filter(r =>
      r.type === 'character'
      && !isPositioningLikeResource(r)
      && !!(r.imageUrl || r.stylizedImageUrl),
    )
    const exact: Resource[] = []
    for (const r of chars) {
      const base = normalizeEntityName(libraryMatchBaseName(r))
      const ident = normalizeEntityName(resourceIdentityName(r) || '')
      const stem = entityStem(base) || entityStem(ident)
      const parent = normalizeEntityName(r.parentName || '')
      if (base === want || ident === want || stem === want || parent === want) exact.push(r)
    }
    return pickBestLibraryResource(exact)
  }

  /** Prose before the first timing/beat header — keep as "小说内容". */
  function extractShotNovelPreface(script: string): string {
    const text = (script || '').replace(/\r\n/g, '\n').trim()
    if (!text) return ''
    const lines = text.split('\n')
    const timingRe = /^(?:【\s*)?\d+\s*[-–—~～到至]\s*\d+\s*秒|\*\*\s*\d+\s*[-–—~～到至]\s*\d+\s*秒|^\s*镜头\s*[：:].*运镜/
    let cut = -1
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i].trim()
      if (!line) continue
      if (timingRe.test(line) || /^【\d/.test(line)) {
        cut = i
        break
      }
    }
    if (cut <= 0) return ''
    return lines.slice(0, cut).join('\n').trim()
  }

  function stripShotTimingBeat(script: string): string {
    const text = sanitizeOptimizedScript(script)
    if (!text) return ''
    const lines = text.split('\n')
    const timingRe = /^(?:【\s*)?\d+\s*[-–—~～到至]\s*\d+\s*秒|\*\*\s*\d+\s*[-–—~～到至]\s*\d+\s*秒|^【\d/
    let start = -1
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i].trim()
      if (!line) continue
      if (timingRe.test(line)) {
        start = i
        break
      }
    }
    if (start < 0) return text
    return lines.slice(start).join('\n').trim()
  }

  /** Keep original novel preface; use model timing beats (and model preface only if original had none). */
  function composeOptimizedShotScript(original: string, optimized: string): string {
    const cleanOriginal = sanitizeOptimizedScript(original)
    const cleanOptimized = sanitizeOptimizedScript(optimized)
    const origNovel = extractShotNovelPreface(cleanOriginal)
    let optBeat = stripShotTimingBeat(cleanOptimized) || cleanOptimized
    optBeat = stripMarkdownBold(optBeat)
    if (origNovel) {
      // Prefer original novel paragraph; if model also returned novel, drop model's preface.
      return `${origNovel}\n\n${optBeat}`.trim()
    }
    return stripMarkdownBold(cleanOptimized)
  }

  /** Remove malformed JSON wrappers/tails from model output before composing it. */
  function sanitizeOptimizedScript(text: string): string {
    let out = (text || '').replace(/\r\n/g, '\n').trim()
    out = out.replace(/^```(?:json|text|markdown)?\s*/i, '').replace(/\s*```$/, '').trim()
    out = out.replace(/^\{\s*["']script["']\s*:\s*["']/, '')
    const tail = out.search(/["']\s*,\s*["']angles["']\s*:/)
    if (tail >= 0) out = out.slice(0, tail)
    // Also catch the fallback form after the leading JSON wrapper was already removed.
    const looseTail = out.search(/,\s*["']angles["']\s*:/)
    if (looseTail >= 0) out = out.slice(0, looseTail)
    return out
      .replace(/\\n/g, '\n')
      .replace(/\\"/g, '"')
      .replace(/["']\s*\}\s*$/, '')
      .trim()
  }

  /** Strip markdown bold / heading markers the model likes to scatter into the script. */
  function stripMarkdownBold(text: string): string {
    return (text || '').replace(/\*\*/g, '').replace(/__/g, '')
  }

  /** Fixed 3×3 camera matrix for scene 9-grids (row-major, 1..9) — mirrors backend SceneGridAngles. */
  const SCENE_GRID_ANGLES = ['正面全景', '正面近景', '侧面全景', '侧面近景', '背面全景', '背面近景', '俯视全景', '俯视近景', '斜向高位总览']

  /** Angle label for a scene grid cell: derived from gridCell (canonical), name parse as fallback. */
  function sceneGridAngleOf(r: Resource): string {
    const cell = r.gridCell || 0
    if (r.genType === 'scene_grid_cell' && cell >= 1 && cell <= 9) return SCENE_GRID_ANGLES[cell - 1]
    const last = (r.name.split('·').pop() || '').trim()
    if ((SCENE_GRID_ANGLES as readonly string[]).includes(last)) return last
    return ''
  }

  /** Strip " · 9宫格…" / cell suffixes to get the base scene name. */
  function shortGridSceneBase(name: string): string {
    const n = (name || '').trim()
    for (const sep of [' · 9宫格', ' · 9帧图']) {
      const i = n.indexOf(sep)
      if (i > 0) return n.slice(0, i).trim()
    }
    // 短格式格子名「场景名·机位名[·候选N]」：剥掉尾部候选标与机位名
    let s = n.replace(/·\s*候选\s*\d+\s*$/, '')
    const parts = s.split('·')
    if (parts.length > 1 && (SCENE_GRID_ANGLES as readonly string[]).includes(parts[parts.length - 1].trim())) {
      s = parts.slice(0, -1).join('·')
    }
    return s.trim()
  }

  /** Scene-grid cell label for refs/prompt: "场景名·机位名". */
  function sceneGridCellRefLabel(r: Resource): string {
    if (r.genType !== 'scene_grid_cell') return cleanRefAlias(resourceEditableName(r) || r.name)
    const parent = r.gridId ? resourceById(r.gridId) : undefined
    const base = cleanRefAlias(parent ? shortGridSceneBase(parent.name) : shortGridSceneBase(r.name))
    const angle = sceneGridAngleOf(r) || (r.gridCell ? `格${r.gridCell}` : '')
    if (base && angle) return `${base}·${angle}`
    return angle || base
  }

  const SCENE_PLACE_TOKENS = [
    '包厢', '雅间', '卡座', '会所', '更衣室', '走廊', '长廊', '擂台', '拳台', '拳馆',
    '大厅', '客厅', '卧室', '房间', '办公室', '酒吧', '别墅', '街道', '天台', '停车场',
    '赛场', '厨房', '浴室', '阳台', '教室', '病房', '仓库', '地下室', '屋顶', '码头',
    '沙滩', '森林', '山洞', '洞府', '厢房', '审讯室', '夜店', '沙发',
  ]

  function scenePlaceTokens(name: string): string[] {
    const core = normalizeEntityName(name)
    if (core.length < 2) return []
    const out = new Set<string>([core])
    for (const token of SCENE_PLACE_TOKENS) {
      if (core.includes(token)) out.add(token)
    }
    if (core.length >= 4) out.add(core.slice(-2))
    return [...out]
  }

  function scoreSceneFamilyAgainstText(
    family: string,
    haystack: string,
    mentions: string[],
    sceneHints: string[],
  ): number {
    let best = scoreResourceNameAgainstText(family, haystack, mentions, sceneHints)
    const core = normalizeEntityName(family)
    if (core.length >= 2 && haystack.includes(core)) {
      best = Math.max(best, 900 + core.length * 10)
    }
    for (const token of scenePlaceTokens(family)) {
      if (token.length < 2 || !haystack.includes(token)) continue
      const bonus = token === core ? 900 : token.length >= 3 ? 740 : 660
      best = Math.max(best, bonus + token.length * 8)
    }
    return best
  }

  function inferWantedSceneAngles(haystack: string): string[] {
    const wanted: string[] = []
    const push = (...angles: string[]) => {
      for (const a of angles) {
        if (a && (SCENE_GRID_ANGLES as readonly string[]).includes(a) && !wanted.includes(a)) wanted.push(a)
      }
    }
    for (const m of haystack.matchAll(/机位：\s*([^\n，。；】]+)/g)) {
      const a = String(m[1] || '').trim().replace(/[（(].*$/, '').trim()
      if ((SCENE_GRID_ANGLES as readonly string[]).includes(a)) push(a)
    }
    const beats = haystack.match(/【[^】]*秒】[^【]*/g) || [haystack]
    for (const beat of beats) {
      if (wanted.length >= 3) break
      if (/俯视|鸟瞰|高位/.test(beat)) push('俯视近景', '斜向高位总览')
      else if (/背面|背影/.test(beat)) push('背面近景', '背面全景')
      else if (/侧面/.test(beat)) push('侧面近景', '侧面全景')
      else if (/特写|近景|中近景/.test(beat)) push('正面近景')
      else if (/全景|大全景|远景|空镜/.test(beat)) push('正面全景', '侧面全景')
      else if (/中景/.test(beat)) push('正面近景', '侧面全景')
    }
    if (!wanted.length) push('正面近景')
    return wanted.slice(0, 3)
  }

  function scoreLibraryResourceAgainstText(
    r: Resource,
    base: string,
    haystack: string,
    mentions: string[],
    sceneHints: string[],
  ): number {
    if (r.type === 'scene' || r.genType === 'scene_grid_cell' || r.genType === 'scene_grid') {
      const family = (r.genType === 'scene_grid_cell' || r.genType === 'scene_grid')
        ? shortGridSceneBase(r.name)
        : base
      let score = scoreSceneFamilyAgainstText(family || base, haystack, mentions, sceneHints)
      if (r.genType === 'scene_grid_cell') {
        const angle = sceneGridAngleOf(r)
        if (angle && haystack.includes(angle)) score = Math.max(score, 1100)
        if (angle && inferWantedSceneAngles(haystack).includes(angle)) score += 80
      }
      return score
    }
    return scoreResourceNameAgainstText(base, haystack, mentions, sceneHints)
  }

  function collectSceneGridCellsForFamily(family: string): Resource[] {
    const fam = normalizeEntityName(family)
    if (fam.length < 2) return []
    const cells = (active.value?.resources || []).filter(r =>
      r.genType === 'scene_grid_cell'
      && !r.deletedAt
      && !!(r.imageUrl || r.stylizedImageUrl)
      && namesOverlapEntity(shortGridSceneBase(r.name), family),
    )
    const byGrid = new Map<number, Resource[]>()
    for (const c of cells) {
      const gid = c.gridId || 0
      const arr = byGrid.get(gid) || []
      arr.push(c)
      byGrid.set(gid, arr)
    }
    let best: Resource[] = []
    for (const arr of byGrid.values()) {
      if (arr.length > best.length) best = arr
    }
    return best.sort((a, b) => (a.gridCell || 0) - (b.gridCell || 0))
  }

  function inferShotSceneFamily(shot: Shot, haystack: string): string {
    const hints = extractShotSceneHints(haystack)
    if (hints.length) return hints[hints.length - 1]
    for (const ref of shot.refs || []) {
      if (ref.kind !== 'scene') continue
      const r = resourceById(ref.id)
      if (!r) continue
      if (r.genType === 'scene_grid_cell' || r.genType === 'scene_grid') {
        const base = shortGridSceneBase(r.name)
        if (base) return base
      }
      const name = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
      if (name.length >= 2) return name
    }
    for (const item of matchLibraryResourcesByText(haystack)) {
      if (item.type !== 'scene') continue
      if (item.resource.genType === 'scene_grid_cell' || item.resource.genType === 'scene_grid') {
        const base = shortGridSceneBase(item.resource.name)
        if (base) return base
      }
      if (item.base.length >= 2) return item.base
    }
    return ''
  }

  /** Prefer split 9-grid cells over the empty master scene plate, by 景别 / 机位. */
  function ensureSceneGridCellRefs(shot: Shot, haystack: string) {
    const family = inferShotSceneFamily(shot, haystack)
    if (!family) return
    const cells = collectSceneGridCellsForFamily(family)
    if (!cells.length) return
    const picked: Resource[] = []
    for (const angle of inferWantedSceneAngles(haystack)) {
      const cell = cells.find(c => sceneGridAngleOf(c) === angle)
      if (cell && !picked.some(p => p.id === cell.id)) picked.push(cell)
      if (picked.length >= 3) break
    }
    if (!picked.length) {
      const fallback = cells.find(c => sceneGridAngleOf(c) === '正面近景') || cells[0]
      if (fallback) picked.push(fallback)
    }
    const cellRefs = picked.map(c => withDefaultShotRefLabel({
      kind: 'scene',
      id: c.id,
      variant: 'original',
      label: sceneGridCellRefLabel(c),
    }))
    const rest = (shot.refs || []).filter((ref) => {
      if (ref.kind !== 'scene') return true
      if (cellRefs.some(c => c.id === ref.id)) return false
      const r = resourceById(ref.id)
      if (!r) return true
      if (r.genType === 'scene_grid_cell') {
        return !namesOverlapEntity(shortGridSceneBase(r.name), family)
      }
      if (r.genType === 'scene_grid') return false
      // Drop master empty plate for this family once angle cells are hung.
      const name = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
      return !namesOverlapEntity(name, family)
    })
    shot.refs = collapseOverlappingShotRefs([...cellRefs, ...rest]).slice(0, maxShotRefs)
    for (const ref of cellRefs) rememberShotRef(ref)
  }

  /** Pin matching 9-grid cells into the AI candidate pool so rematch can see them. */
  function pinSceneGridCellsIntoPool(
    byID: Map<number, LibraryNameMatch>,
    haystack: string,
  ) {
    const hints = extractShotSceneHints(haystack)
    const family = hints.length ? hints[hints.length - 1] : ''
    if (!family) {
      // Fall back: any cell family that scores against the text.
      for (const r of active.value?.resources || []) {
        if (r.genType !== 'scene_grid_cell' || !(r.imageUrl || r.stylizedImageUrl)) continue
        const fam = shortGridSceneBase(r.name)
        if (fam.length >= 2 && scoreSceneFamilyAgainstText(fam, haystack, extractShotMentionNames(haystack), hints) >= 400) {
          pinSceneGridFamilyIntoPool(byID, fam, haystack)
          return
        }
      }
      return
    }
    pinSceneGridFamilyIntoPool(byID, family, haystack)
  }

  function pinSceneGridFamilyIntoPool(
    byID: Map<number, LibraryNameMatch>,
    family: string,
    haystack: string,
  ) {
    const cells = collectSceneGridCellsForFamily(family)
    if (!cells.length) return
    const wanted = new Set(inferWantedSceneAngles(haystack))
    for (const c of cells) {
      const angle = sceneGridAngleOf(c)
      const base = sceneGridCellRefLabel(c)
      const score = wanted.has(angle) ? 4800 : 1200
      const cur = byID.get(c.id)
      if (cur) {
        cur.score = Math.max(cur.score, score)
        continue
      }
      byID.set(c.id, { resource: c, base, type: 'scene', score })
    }
  }

  /** Normalize 机位标注 in optimized script to reference concrete 图N. */
  function bindScriptAnglesToRefs(
    script: string,
    refs: ShotRef[],
    lookup: (id: number) => Resource | undefined,
  ): string {
    const angleToIdx = new Map<string, number>()
    refs.forEach((ref, i) => {
      const r = lookup(ref.id)
      if (!r || r.genType !== 'scene_grid_cell') return
      const label = (ref.label || sceneGridCellRefLabel(r) || '').trim()
      const angle = label.split('·').pop()?.trim()
      if (angle && !angleToIdx.has(angle)) angleToIdx.set(angle, i + 1)
      // also map bare angle like 格3
      if (r.gridCell && !angleToIdx.has(`格${r.gridCell}`)) angleToIdx.set(`格${r.gridCell}`, i + 1)
    })
    if (!angleToIdx.size) return script
    return script.replace(/机位：([^\n，。；】]+)/g, (_, angleName) => {
      const key = String(angleName || '').trim()
      const idx = angleToIdx.get(key) || angleToIdx.get(key.replace(/（.*）/, '').trim())
      return idx ? `机位参考图${idx}（${key}）` : `机位：${key}`
    })
  }

  /** Drop duplicate scene_grid_cell refs that share the same angle (keeps first). */
  function dedupeAngleCellRefs(refs: ShotRef[]): ShotRef[] {
    const seenAngle = new Set<string>()
    return refs.filter(ref => {
      const r = resourceById(ref.id)
      if (!r || r.genType !== 'scene_grid_cell') return true
      const label = (ref.label || sceneGridCellRefLabel(r) || '').trim()
      const angle = label.split('·').pop()?.trim() || `格${r.gridCell}`
      if (seenAngle.has(angle)) return false
      seenAngle.add(angle)
      return true
    })
  }

  function applyShotLocalPatch(shotId: number, patch: Partial<Shot>) {
    if (!activeEpisode.value?.shots) return
    activeEpisode.value.shots = activeEpisode.value.shots.map(s =>
      s.id === shotId ? { ...s, ...patch } : s,
    )
    syncEpisode()
  }

  function collapseOverlappingShotRefs(refs: ShotRef[]): ShotRef[] {
    const childParents = new Set<number>()
    for (const ref of refs) {
      if (ref.kind !== 'character') continue
      const parentId = resourceById(ref.id)?.parentId
      if (parentId) childParents.add(parentId)
    }
    const scoped = childParents.size
      ? refs.filter(ref => !(ref.kind === 'character' && childParents.has(ref.id)))
      : refs
    const kept: ShotRef[] = []
    for (const ref of scoped) {
      const label = cleanRefAlias(ref.label || '')
        || cleanRefAlias(resourceIdentityName(resourceById(ref.id)) || resourceById(ref.id)?.name || '')
      if (label && kept.some((k) => {
        if (refDedupeGroup(k.kind) !== refDedupeGroup(ref.kind)) return false
        const kr = resourceById(k.id)
        const rr = resourceById(ref.id)
        const kCell = kr?.genType === 'scene_grid_cell'
        const rCell = rr?.genType === 'scene_grid_cell'
        if (k.kind === 'scene' && ref.kind === 'scene' && (kCell || rCell)) {
          if (kCell && rCell) {
            return sceneGridAngleOf(kr) === sceneGridAngleOf(rr)
              || (!!kr?.gridCell && kr.gridCell === rr?.gridCell)
          }
          return false
        }
        const other = cleanRefAlias(k.label || '')
          || cleanRefAlias(resourceIdentityName(resourceById(k.id)) || resourceById(k.id)?.name || '')
        return namesOverlapEntity(other, label)
      })) continue
      kept.push(ref)
    }
    return kept
  }

  function forceReplaceShot(shot: Shot) {
    if (!activeEpisode.value) return
    if (!activeEpisode.value.shots) activeEpisode.value.shots = []
    const next = normalizeShot(shot)
    activeEpisode.value.shots = activeEpisode.value.shots.map(s =>
      s.id === next.id ? next : s,
    )
    syncEpisode()
  }

  function bumpShotScriptEpoch(shotId: number) {
    shotScriptEpoch.value = {
      ...shotScriptEpoch.value,
      [shotId]: (shotScriptEpoch.value[shotId] || 0) + 1,
    }
  }

  /** Fetch resources for name matching without refreshing the library grid (avoids duplicate page fetches). */
  async function refreshProjectResourcesForPick() {
    if (!active.value) return
    // Backend caps pageSize at 48; paginate so the whole library joins the match pool.
    const pageSize = 48
    const maxPages = 30
    let merged = active.value.resources
    for (let page = 1; page <= maxPages; page++) {
      const resources = await api(`/projects/${active.value.id}/resources?page=${page}&pageSize=${pageSize}&type=all&enrich=0&includeDerivatives=1`)
      const items = Array.isArray(resources?.items)
        ? resources.items as Resource[]
        : (Array.isArray(resources) ? resources as Resource[] : [])
      if (!items.length) break
      merged = mergeResourceLists(merged, items)
      const total = Number(resources?.total) || 0
      if (items.length < pageSize || (total > 0 && page * pageSize >= total)) break
    }
    // type=all pagination can bury characters/scenes under 9-grid cells; pull them separately.
    for (const typ of ['character', 'scene']) {
      for (let page = 1; page <= maxPages; page++) {
        const resources = await api(`/projects/${active.value.id}/resources?page=${page}&pageSize=${pageSize}&type=${typ}&enrich=0&includeDerivatives=1`)
        const items = Array.isArray(resources?.items)
          ? resources.items as Resource[]
          : (Array.isArray(resources) ? resources as Resource[] : [])
        if (!items.length) break
        merged = mergeResourceLists(merged, items)
        const total = Number(resources?.total) || 0
        if (items.length < pageSize || (total > 0 && page * pageSize >= total)) break
      }
    }
    active.value = { ...active.value, resources: merged }
  }

  async function optimizeShotScript(shot: Shot) {
    if (!shot.script.trim()) {
      error.value = '请先填写当前分镜文案'
      return
    }
    if (optimizingScripts.value.has(shot.id)) return
    optimizingScripts.value = new Set(optimizingScripts.value).add(shot.id)
    error.value = ''
    const originalScript = shot.script
    try {
      await saveShot(shot, { replace: false })
      await refreshProjectResourcesForPick().catch(() => {})
      const result = await api(`/shots/${shot.id}/optimize-script`, { method: 'POST', body: '{}' })
      const rawOptimized = String(result.script || '').trim()
      if (!rawOptimized) throw new Error('未返回优化文案')
      const script = composeOptimizedShotScript(originalScript, rawOptimized)

      const suggested = Array.isArray(result.suggestedRefs)
        ? result.suggestedRefs as { id?: number; label?: string; kind?: string; variant?: string; beats?: string }[]
        : []
      const angleRefs: ShotRef[] = []
      for (const s of suggested) {
        const id = Number(s.id)
        if (!Number.isFinite(id) || id <= 0) continue
        const r = resourceById(id)
        if (!r || !(r.imageUrl || r.stylizedImageUrl)) continue
        const label = sceneGridCellRefLabel(r) || cleanRefAlias(s.label || '')
        angleRefs.push({
          kind: 'scene',
          id,
          variant: 'original',
          label: label || undefined,
        })
      }

      const working: Shot = {
        ...shot,
        script,
        refs: collapseOverlappingShotRefs([
          ...angleRefs.map(r => withDefaultShotRefLabel(r)),
          ...(shot.refs || []).filter(ref => {
            // Drop plain scene refs that the angle cells replace (same scene family)
            if (ref.kind !== 'scene' || !angleRefs.length) return true
            const r = resourceById(ref.id)
            if (!r) return true
            if (r.genType === 'scene_grid_cell' || r.genType === 'scene_grid') return true
            return !angleRefs.some(a => {
              const cell = resourceById(a.id)
              if (!cell) return false
              const sceneBase = shortGridSceneBase(cell.name)
              const refBase = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
              return !!sceneBase && namesOverlapEntity(sceneBase, refBase)
            })
          }),
        ]),
      }
      await autoPickShotRefsWithAI(working, script)
      working.refs = collapseOverlappingShotRefs(working.refs)
      // Ensure suggested angle cells stay (auto-pick may have overwritten with whole scenes)
      for (const angle of angleRefs) {
        const keyed = withDefaultShotRefLabel(angle)
        if (working.refs.some(r => r.id === keyed.id)) continue
        if (working.refs.length >= maxShotRefs) break
        // Prefer angle cell over overlapping plain scene
        working.refs = working.refs.filter(ref => {
          if (ref.kind !== 'scene') return true
          const r = resourceById(ref.id)
          if (!r || r.genType === 'scene_grid_cell') return true
          const sceneBase = shortGridSceneBase(resourceById(keyed.id)?.name || '')
          const refBase = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
          return !(sceneBase && namesOverlapEntity(sceneBase, refBase))
        })
        working.refs = [keyed, ...working.refs].slice(0, maxShotRefs)
        rememberShotRef(keyed)
      }
      // Bind 机位标注 to concrete 图N（按最终 refs 顺序），视频模型更好对应
      working.script = bindScriptAnglesToRefs(working.script, working.refs, resourceById)
      working.refs = dedupeAngleCellRefs(working.refs)

      clearShotDirty(shot.id)
      const updated = await api(`/shots/${shot.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          label: working.label || '',
          script: working.script,
          note: working.note || '',
          visualStyle: working.visualStyle,
          imageRefs: working.imageRefs,
          refs: working.refs,
          duration: working.duration,
          resolution: working.resolution,
          videoModelId: working.videoModelId || null,
          positioningPrompt: working.positioningPrompt || '',
          positioningRefs: working.positioningRefs || [],
          motionGridPrompt: working.motionGridPrompt || '',
          motionGridRefs: working.motionGridRefs || [],
        }),
      })
      clearShotDirty(shot.id)
      forceReplaceShot({
        ...normalizeShot(updated),
        refs: working.refs,
      })
      applyPackedShots(updated)
      bumpShotScriptEpoch(shot.id)
      ElNotification({
        title: '已按质检核对',
        message: `分镜 ${shot.label || shot.id} 已按质检规则核对并改稿`,
        type: 'success',
        position: 'bottom-right',
        duration: 3000,
      })
    } catch (e: any) {
      error.value = e.message || '优化分镜文案失败'
    } finally {
      const next = new Set(optimizingScripts.value)
      next.delete(shot.id)
      optimizingScripts.value = next
    }
  }

  function isHelperShotRef(ref: ShotRef): boolean {
    const r = resourceById(ref.id)
    const blob = `${ref.label || ''} ${r?.name || ''} ${r?.genType || ''}`
    if (r?.genType === 'positioning' || r?.genType === 'transition_frame'
      || r?.genType === 'motion_grid' || r?.genType === 'motion_grid_cell') {
      return true
    }
    return /站位|尾帧|9帧|收势/.test(blob)
  }

  function shotCharacterIdentityKey(ref: ShotRef): string {
    const r = resourceById(ref.id)
    if (r?.parentId) return `p:${r.parentId}`
    return `id:${ref.id}`
  }

  function firstBeatBody(haystack: string): string {
    const re = /【\d+\s*[-–—~到至]\s*\d+\s*秒】/g
    const marks = [...haystack.matchAll(re)]
    if (!marks.length) return haystack
    const start = (marks[0].index || 0) + marks[0][0].length
    const end = marks[1]?.index ?? haystack.length
    return haystack.slice(start, end)
  }

  function characterGridSlot(stem: string, haystack: string): string {
    if (stem.length < 2) return ''
    const slots = ['左前', '中前', '右前', '左中', '中中', '右中', '左后', '中后', '右后']
    for (const slot of slots) {
      if (haystack.includes(`${stem}(${slot})`) || haystack.includes(`${stem}（${slot}）`) || haystack.includes(`${stem} (${slot})`)) {
        return slot
      }
    }
    return ''
  }

  function analyzeCharacterFocus(ref: ShotRef, haystack: string): {
    speaker: boolean
    firstBeat: boolean
    focusGrid: boolean
    backGrid: boolean
    crowd: boolean
    mentions: number
  } {
    const r = resourceById(ref.id)
    const label = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceIdentityName(r) || r?.name || '')
    const stem = entityStem(label) || normalizeEntityName(label)
    if (stem.length < 2) {
      return { speaker: false, firstBeat: false, focusGrid: false, backGrid: false, crowd: false, mentions: 0 }
    }
    const slot = characterGridSlot(stem, haystack)
    return {
      speaker: haystack.includes(`${stem}说`) || haystack.includes(`${stem} 说`),
      firstBeat: firstBeatBody(haystack).includes(stem),
      focusGrid: !!slot && !slot.endsWith('后'),
      backGrid: !!slot && slot.endsWith('后'),
      crowd: /杀手|路人|群演|群众|宾客|保镖|手下/.test(label),
      mentions: haystack.split(stem).length - 1,
    }
  }

  /** Keep named script characters when the 12-ref ceiling would drop them. */
  function pinScriptCharacterRefs(refs: ShotRef[], haystack: string): ShotRef[] {
    const collapsed = collapseOverlappingShotRefs(refs || [])
    if (collapsed.length <= maxShotRefs) return collapsed
    const namedIds = new Set<number>()
    for (const name of extractScriptCharacterNames(haystack)) {
      const r = findCharacterResourceForName(name)
      if (!r) continue
      namedIds.add(r.id)
      if (r.parentId) namedIds.add(r.parentId)
    }
    const pinned: ShotRef[] = []
    const rest: ShotRef[] = []
    for (const ref of collapsed) {
      const r = resourceById(ref.id)
      const isNamedChar = ref.kind === 'character' && (
        namedIds.has(ref.id)
        || namedIds.has(r?.id || 0)
        || namedIds.has(r?.parentId || 0)
      )
      if (isNamedChar || ref.kind === 'scene' || isHelperShotRef(ref)) pinned.push(ref)
      else rest.push(ref)
    }
    return [...pinned, ...rest].slice(0, maxShotRefs)
  }

  /** Drop 杀手/路人/群演 face sheets; named people in the script all stay. */
  function trimCrowdCharacterRefs(shot: Shot, haystack: string): number {
    const chars = (shot.refs || []).filter(r => r.kind === 'character')
    if (!chars.length) return 0
    const dropIds = new Set<number>()
    const seen = new Map<string, ShotRef[]>()
    for (const ref of chars) {
      const key = shotCharacterIdentityKey(ref)
      const arr = seen.get(key) || []
      arr.push(ref)
      seen.set(key, arr)
    }
    let dropped = 0
    for (const [, refs] of seen) {
      const crowd = refs.some(r => analyzeCharacterFocus(r, haystack).crowd)
      if (!crowd) continue
      dropped++
      for (const r of refs) dropIds.add(r.id)
    }
    if (!dropIds.size) return 0
    shot.refs = (shot.refs || []).filter(r => r.kind !== 'character' || !dropIds.has(r.id))
    return dropped
  }

  function shotHasPositioningRef(shot: Shot): boolean {
    return (shot.refs || []).some(ref => {
      if (isHelperShotRef(ref) && /站位/.test(`${ref.label || ''} ${resourceById(ref.id)?.name || ''}`)) return true
      const r = resourceById(ref.id)
      return r?.genType === 'positioning' || /站位/.test(r?.name || '')
    })
  }

  async function rematchShotRefs(shot: Shot) {
    if (!shot.script.trim()) {
      error.value = '请先填写当前分镜文案'
      return
    }
    if (matchingShotRefs.value.has(shot.id) || optimizingScripts.value.has(shot.id)) return
    matchingShotRefs.value = new Set(matchingShotRefs.value).add(shot.id)
    error.value = ''
    try {
      await saveShot(shot, { replace: false })
      await refreshProjectResourcesForPick().catch(() => {})
      const helpers = (shot.refs || []).filter(isHelperShotRef).map(r => withDefaultShotRefLabel(r))
      const working: Shot = { ...shot, refs: helpers }
      await autoPickShotRefsWithAI(working, shot.script)
      working.refs = pinScriptCharacterRefs(working.refs, shot.script)
      const droppedCrowd = trimCrowdCharacterRefs(working, shot.script)
      clearShotDirty(shot.id)
      const updated = await api(`/shots/${shot.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          label: working.label || '',
          script: working.script,
          note: working.note || '',
          visualStyle: working.visualStyle,
          imageRefs: working.imageRefs,
          refs: working.refs,
          duration: working.duration,
          resolution: working.resolution,
          videoModelId: working.videoModelId || null,
          positioningPrompt: working.positioningPrompt || '',
          positioningRefs: working.positioningRefs || [],
          motionGridPrompt: working.motionGridPrompt || '',
          motionGridRefs: working.motionGridRefs || [],
        }),
      })
      clearShotDirty(shot.id)
      forceReplaceShot({
        ...normalizeShot(updated),
        refs: working.refs,
      })
      applyPackedShots(updated)
      const chars = working.refs.filter(r => r.kind === 'character').length
      const picked = working.refs.filter(r => !isHelperShotRef(r)).length
      const hasPos = shotHasPositioningRef(working)
      let message = picked
        ? `分镜 ${shot.label || shot.id} 已按当前文案匹配 ${picked} 张`
        : `分镜 ${shot.label || shot.id} 未在资源库匹配到角色/场景/道具`
      if (droppedCrowd > 0) {
        message = hasPos
          ? `焦点角色 ${chars} 人，其余人按已有站位图出画`
          : `焦点角色 ${chars} 人；其余 ${droppedCrowd} 人请点「生成站位图」表示群体`
      }
      ElNotification({
        title: '已重新选择参考图',
        message,
        type: picked ? 'success' : 'warning',
        position: 'bottom-right',
        duration: 4000,
      })
    } catch (e: any) {
      error.value = e.message || '重新选择参考图失败'
    } finally {
      const next = new Set(matchingShotRefs.value)
      next.delete(shot.id)
      matchingShotRefs.value = next
    }
  }

  async function extractPreviousFrame(shot: Shot) {
    extractingFrame.value = shot.id
    error.value = ''
    try {
      const data = await api(`/shots/${shot.id}/previous-frame`, { method: 'POST', body: '{}' })
      if (data.resource && active.value) mergeNewResources([data.resource as Resource])
      if (data.shot) {
        replaceShot(normalizeShot(data.shot))
        notifyStudioSync({ type: 'shot', projectId: active.value?.id, shotId: shot.id, status: 'settled' })
      }
      if (active.value) await refreshProjectResources()
      // 后台人脸标注完成后会覆盖同一张图；定时刷新几次拿到标注版（URL 带版本号）
      if (data.annotating) {
        const shotId = shot.id
        const projectId = active.value?.id
        for (const delay of [30_000, 90_000, 180_000]) {
          window.setTimeout(() => {
            if (!active.value || active.value.id !== projectId) return
            void refreshProjectResources()
            void refreshShotFromServer(shotId)
          }, delay)
        }
      }
    } catch (e: any) {
      error.value = e.message || '提取上一镜尾帧失败'
    } finally {
      extractingFrame.value = null
    }
  }

  function closePromptPreview() { promptPreview.value = null }
  function refKindLabel(kind: string, variant?: string) {
    if (kind === 'scene') return variant === 'original' ? '场景·原图' : '场景·非真人'
    if (kind === 'prop') return '道具'
    if (kind === 'other') return variant === 'original' ? '其他·原图' : '其他·非真人'
    return variant === 'original' ? '角色·真人' : '角色·非真人'
  }
  function replaceShot(updated: Shot) {
    const remote = normalizeShot(updated)
    if (!activeEpisode.value) return
    if (!activeEpisode.value.shots) activeEpisode.value.shots = []
    const local = activeEpisode.value.shots.find(s => s.id === remote.id)
    const shot = mergeShotPreservingDirty(local, remote)
    const exists = !!local
    if (exists) {
      activeEpisode.value.shots = activeEpisode.value.shots.map(s => s.id === shot.id ? shot : s)
    }
    syncEpisode()
  }
  function applyPackedShots(payload: any, opts?: { preserveDirtyIds?: number[] }) {
    const extras = Array.isArray(payload?.packedShots) ? payload.packedShots : []
    if (!extras.length || !activeEpisode.value) return
    const preserve = new Set(opts?.preserveDirtyIds || [])
    for (const raw of extras) {
      const next = normalizeShot(raw)
      if (!next.id) continue
      const local = activeEpisode.value.shots.find(s => s.id === next.id)
      const exists = !!local
      if (exists) {
        if (preserve.has(next.id) || dirtyShotIds.value.has(next.id)) {
          replaceShot(mergeShotPreservingDirty(local, next))
          continue
        }
        clearShotDirty(next.id)
        replaceShot(next)
        continue
      }
      activeEpisode.value.shots = [...activeEpisode.value.shots, next].sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id)
      shotTotal.value += 1
    }
    syncEpisode()
  }
  function syncEpisode() {
    if (!active.value || !activeEpisode.value) return
    active.value.episodes = active.value.episodes.map(e => e.id === activeEpisode.value!.id ? activeEpisode.value! : e)
  }
  function shotRefKey(ref: ShotRef) {
    if (ref.kind === 'character') return `c-${ref.id}-${ref.variant || 'stylized'}`
    if (ref.kind === 'scene') return `s-${ref.id}-${ref.variant || 'stylized'}`
    if (ref.kind === 'prop') return `p-${ref.id}`
    if (ref.kind === 'other') return `o-${ref.id}-${ref.variant || 'stylized'}`
    return `s-${ref.id}`
  }
  function normalizeStoredShotRef(raw: unknown): ShotRef | null {
    if (!raw || typeof raw !== 'object') return null
    const r = raw as Partial<ShotRef>
    if (r.kind !== 'character' && r.kind !== 'scene' && r.kind !== 'prop' && r.kind !== 'other') return null
    if (typeof r.id !== 'number' || !Number.isFinite(r.id)) return null
    if (r.kind === 'prop') return { kind: 'prop', id: r.id }
    const variant = r.variant === 'original' ? 'original' : 'stylized'
    return { kind: r.kind, id: r.id, variant }
  }
  function loadRecentShotRefsFromStorage(projectId: number): ShotRef[] {
    try {
      const raw = localStorage.getItem(recentShotRefsStorageKey(projectId))
      if (!raw) return []
      const parsed = JSON.parse(raw)
      if (!Array.isArray(parsed)) return []
      const seen = new Set<string>()
      const out: ShotRef[] = []
      for (const item of parsed) {
        const ref = normalizeStoredShotRef(item)
        if (!ref) continue
        const key = shotRefKey(ref)
        if (seen.has(key)) continue
        seen.add(key)
        out.push(ref)
        if (out.length >= maxRecentShotRefs) break
      }
      return out
    } catch {
      return []
    }
  }
  function collectRecentShotRefsFromProject(project: Project): ShotRef[] {
    const seen = new Set<string>()
    const out: ShotRef[] = []
    const episodes = [...project.episodes].sort((a, b) => b.number - a.number)
    for (const ep of episodes) {
      const shots = [...ep.shots].sort((a, b) => {
        const at = a.updatedAt || a.createdAt || ''
        const bt = b.updatedAt || b.createdAt || ''
        if (at !== bt) return bt.localeCompare(at)
        return b.sortOrder - a.sortOrder
      })
      for (const shot of shots) {
        for (const ref of [...(shot.refs || [])].reverse()) {
          const normalized = normalizeStoredShotRef(ref)
          if (!normalized) continue
          const key = shotRefKey(normalized)
          if (seen.has(key)) continue
          seen.add(key)
          out.push(normalized)
          if (out.length >= maxRecentShotRefs) return out
        }
      }
    }
    return out
  }
  function persistRecentShotRefs(projectId: number, refs: ShotRef[]) {
    recentShotRefsStored.value = refs
    try {
      localStorage.setItem(recentShotRefsStorageKey(projectId), JSON.stringify(refs))
    } catch { /* ignore quota */ }
  }
  function hydrateRecentShotRefs(project: Project) {
    const stored = loadRecentShotRefsFromStorage(project.id)
    if (stored.length) {
      recentShotRefsStored.value = stored
      return
    }
    const seeded = collectRecentShotRefsFromProject(project)
    persistRecentShotRefs(project.id, seeded)
  }
  function rememberShotRef(ref: ShotRef) {
    if (!active.value) return
    const normalized = normalizeStoredShotRef(ref)
    if (!normalized) return
    const key = shotRefKey(normalized)
    const next = [
      normalized,
      ...recentShotRefsStored.value.filter(r => shotRefKey(r) !== key),
    ].slice(0, maxRecentShotRefs)
    persistRecentShotRefs(active.value.id, next)
  }
  function recentRefStillAvailable(ref: ShotRef) {
    const r = resourceById(ref.id)
    if (!r || r.deletedAt) return false
    if (ref.kind === 'prop') return !!r.imageUrl
    if (ref.kind === 'scene') return !!sceneImage(r, ref.variant || 'stylized')
    if (ref.kind === 'other') return !!otherImage(r, ref.variant || 'stylized')
    return !!characterImage(r, ref.variant || 'stylized')
  }
  const recentShotRefs = computed(() =>
    recentShotRefsStored.value.filter(recentRefStillAvailable),
  )
  function recentResourceIndex(resourceId: number): number {
    return recentShotRefsStored.value.findIndex(r => r.id === resourceId)
  }
  function recentShotRefForResource(resourceId: number): ShotRef | null {
    return recentShotRefsStored.value.find(r => r.id === resourceId) || null
  }
  /** If a same-name library asset was used recently, prefer that over a random/primary candidate. */
  function preferRecentResourceForName(resource: Resource): Resource {
    const base = cleanRefAlias(resourceEditableName(resource) || resource.name)
    if (base.length < 2) return resource
    const stem = entityStem(base)
    let best: Resource | null = null
    let bestIdx = Number.POSITIVE_INFINITY
    for (const ref of recentShotRefsStored.value) {
      const r = resourceById(ref.id)
      if (!r) continue
      // character/other are interchangeable for creature refs like 小七
      const sameGroup = r.type === resource.type
        || ((r.type === 'character' || r.type === 'other') && (resource.type === 'character' || resource.type === 'other'))
      if (!sameGroup) continue
      if (!(r.imageUrl || r.stylizedImageUrl)) continue
      const name = cleanRefAlias(resourceEditableName(r) || r.name)
      if (name !== base && !namesOverlapEntity(name, base) && entityStem(name) !== stem) continue
      const idx = recentResourceIndex(r.id)
      if (idx >= 0 && idx < bestIdx) {
        best = r
        bestIdx = idx
      }
    }
    return best || resource
  }
  /** Recently used refs whose names still match the current script text. */
  function recentShotRefsMatchingText(
    haystack: string,
    mentions: string[],
    sceneHints: string[],
  ): ShotRef[] {
    const out: ShotRef[] = []
    for (const ref of recentShotRefs.value) {
      const r = resourceById(ref.id)
      if (!r || isPositioningLikeResource(r)) continue
      if (!(r.imageUrl || r.stylizedImageUrl)) continue
      const label = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
      if (label.length < 2) continue
      const score = scoreResourceNameAgainstText(label, haystack, mentions, sceneHints)
      if (score < 200) continue
      out.push(withDefaultShotRefLabel({
        ...ref,
        label: label || ref.label,
      }))
    }
    return out
  }
  function resourceById(id: number) { return active.value?.resources.find(r => r.id === id) }
  function resourceIdentityName(item?: Resource | null) {
    if (!item) return ''
    const p = parseCandidateName(item.name)
    const base = p && item.isGroupPrimary ? p.base : item.name
    const parentName = (item.parentName || '').trim()
      || (item.parentId ? (resourceById(item.parentId)?.name || libraryParent.value?.name || '') : '')
    if (parentName && !String(base || '').includes(parentName)) {
      return `${parentName} · ${base}`
    }
    return base
  }
  function effectiveShotRefVariant(ref: ShotRef): 'stylized' | 'original' | undefined {
    if (ref.kind === 'prop') return undefined
    const r = resourceById(ref.id)
    const hasOriginal = !!r?.imageUrl
    const hasStylized = !!r?.stylizedImageUrl
    if (ref.variant === 'original') {
      if (hasOriginal) return 'original'
      if (hasStylized) return 'stylized'
      return 'original'
    }
    if (ref.variant === 'stylized') {
      if (hasStylized) return 'stylized'
      if (hasOriginal) return 'original'
      return 'stylized'
    }
    if (ref.kind === 'character') {
      if (hasStylized) return 'stylized'
      if (hasOriginal) return 'original'
      return 'stylized'
    }
    if (ref.kind === 'scene') return 'original'
    return (r ? preferredShotRefVariant(r) : 'stylized') || 'stylized'
  }
  function refThumb(ref: ShotRef) {
    const r = resourceById(ref.id)
    if (!r) return ''
    const variant = effectiveShotRefVariant(ref)
    if (ref.kind === 'scene') return sceneImage(r, variant || 'original')
    if (ref.kind === 'prop') return r.imageUrl || ''
    if (ref.kind === 'other') return otherImage(r, variant || 'stylized')
    return characterImage(r, variant || 'stylized')
  }
  function refDisplayName(ref: ShotRef) {
    const r = resourceById(ref.id)
    const identity = r ? (resourceIdentityName(r) || r.name) : ''
    const custom = (ref.label || '').trim()
    if (custom && r?.parentId) {
      const parent = (r.parentName || '').trim() || resourceById(r.parentId)?.name || libraryParent.value?.name || ''
      const base = r.name
      if (parent && (custom === base || !custom.includes(parent))) {
        return identity
      }
    }
    if (custom) return custom
    return identity || (ref.kind === 'scene' ? '场景' : ref.kind === 'prop' ? '道具' : ref.kind === 'other' ? '其他' : '角色')
  }
  function refLabel(ref: ShotRef) {
    const name = refDisplayName(ref)
    const variant = effectiveShotRefVariant(ref)
    if (ref.kind === 'scene') {
      return `${name} · ${variantLabel(variant || 'original', 'scene')}`
    }
    if (ref.kind === 'prop') {
      return `${name} · 参考`
    }
    if (ref.kind === 'other') {
      return `${name} · ${variantLabel(variant || 'stylized', 'other')}`
    }
    return `${name} · ${variantLabel(variant || 'stylized')}`
  }
  function refTag(ref: ShotRef, index: number) { return `@图${index + 1}` }
  function hasShotRef(shot: Shot, ref: ShotRef) {
    return shot.refs.some(r => shotRefKey(r) === shotRefKey(ref))
  }
  function withDefaultShotRefLabel(ref: ShotRef): ShotRef {
    if ((ref.label || '').trim()) return ref
    return { ...ref, label: refDisplayName(ref) }
  }
  function addShotRef(shot: Shot, ref: ShotRef) {
    const next = withDefaultShotRefLabel(ref)
    if (hasShotRef(shot, next) || shot.refs.length >= maxShotRefs) return
    shot.refs = [...shot.refs, next]
    rememberShotRef(next)
    saveShot(shot)
  }
  function renameShotRef(shot: Shot, index: number, label: string) {
    if (index < 0 || index >= shot.refs.length) return
    const trimmed = label.trim()
    const refs = [...shot.refs]
    const current = refs[index]
    const nextLabel = trimmed || refDisplayName({ ...current, label: undefined })
    if ((current.label || '') === nextLabel) {
      markShotDirty(shot.id)
      return
    }
    refs[index] = { ...current, label: nextLabel }
    shot.refs = refs
    markShotDirty(shot.id)
    saveShot(shot)
  }

  /** Live-edit ref label while typing (poll-safe). Does not save until commit. */
  function updateShotRefLabel(shot: Shot, index: number, label: string) {
    if (index < 0 || index >= shot.refs.length) return
    const refs = [...shot.refs]
    const current = refs[index]
    if ((current.label || '') === label) {
      markShotDirty(shot.id)
      return
    }
    refs[index] = { ...current, label }
    shot.refs = refs
    markShotDirty(shot.id)
  }

  function seedShotRefLabelIfEmpty(shot: Shot, index: number) {
    if (index < 0 || index >= shot.refs.length) return
    const current = shot.refs[index]
    if ((current.label || '').trim()) return
    updateShotRefLabel(shot, index, refDisplayName(current))
  }
  function removeShotRef(shot: Shot, ref: ShotRef) {
    shot.refs = shot.refs.filter(r => shotRefKey(r) !== shotRefKey(ref))
    saveShot(shot)
  }
  function sceneImage(s: Resource, variant: 'stylized' | 'original' = 'stylized') {
    return variant === 'original' ? (s.imageUrl || '') : (s.stylizedImageUrl || s.imageUrl || '')
  }
  function characterImage(c: Resource, variant: 'stylized' | 'original') {
    return variant === 'original' ? (c.imageUrl || '') : (c.stylizedImageUrl || c.imageUrl || '')
  }
  function otherImage(o: Resource, variant: 'stylized' | 'original' = 'stylized') {
    return variant === 'original' ? (o.imageUrl || '') : (o.stylizedImageUrl || o.imageUrl || '')
  }
  function variantLabel(v: 'stylized' | 'original', kind: 'character' | 'scene' | 'other' = 'character') {
    if (v === 'original') return kind === 'character' ? '真人' : '原图'
    return '非真人'
  }
  function pickerShot(): Shot | null {
    if (picker.value == null || !activeEpisode.value) return null
    return activeEpisode.value.shots.find(s => s.id === picker.value) ?? null
  }
  function openPicker(shot: Shot) {
    picker.value = shot.id
    pickerReplaceIndex.value = null
    expandShot(shot.id)
    void refreshProjectResources()
  }
  function openReplacePicker(shot: Shot, index: number) {
    picker.value = shot.id
    pickerReplaceIndex.value = index
    expandShot(shot.id)
    void refreshProjectResources()
  }
  function closePicker() {
    picker.value = null
    pickerReplaceIndex.value = null
  }
  function pickerRefSelected(ref: ShotRef) {
    const shot = pickerShot()
    if (!shot) return false
    if (pickerReplaceIndex.value != null) {
      const current = shot.refs[pickerReplaceIndex.value]
      return current ? shotRefKey(current) === shotRefKey(ref) : false
    }
    return hasShotRef(shot, ref)
  }
  function pickerRefDisabled(ref: ShotRef) {
    const shot = pickerShot()
    if (!shot) return true
    if (pickerReplaceIndex.value != null) {
      const idx = pickerReplaceIndex.value
      const current = shot.refs[idx]
      if (current && shotRefKey(current) === shotRefKey(ref)) return true
      return shot.refs.some((r, i) => i !== idx && shotRefKey(r) === shotRefKey(ref))
    }
    return hasShotRef(shot, ref) || shot.refs.length >= maxShotRefs
  }
  function applyShotRef(ref: ShotRef) {
    const shot = pickerShot()
    if (!shot) return
    const next = withDefaultShotRefLabel(ref)
    if (pickerReplaceIndex.value != null) {
      const idx = pickerReplaceIndex.value
      const refs = [...shot.refs]
      if (refs.some((r, i) => i !== idx && shotRefKey(r) === shotRefKey(next))) {
        error.value = '该参考图已在列表中'
        return
      }
      if (refs[idx] && shotRefKey(refs[idx]) === shotRefKey(next)) {
        closePicker()
        return
      }
      // Keep previous custom alias when replacing image at same slot.
      const prevLabel = (refs[idx]?.label || '').trim()
      refs[idx] = prevLabel ? { ...next, label: prevLabel } : next
      shot.refs = refs
      rememberShotRef(next)
      saveShot(shot)
      closePicker()
      return
    }
    addShotRef(shot, next)
  }
  function pickerResourceName(item: Resource) {
    return resourceDisplayName(item)
  }
  async function usePrimaryResource(item: Resource) {
    const existing = findExistingLibraryBase(resourceBaseName(item.name), item.type)
    if (item.isGroupPrimary && !item.deletedAt && (!existing || existing.id === item.id)) return
    applyingPrimary.value = item.id
    error.value = ''
    try {
      const result = await api(`/resources/${item.id}/use-primary`, { method: 'POST' })
      const kept = result.resource as Resource
      await refreshProjectResources()
      if (kept.type === 'video' && kept.shotId) await refreshShotFromServer(kept.shotId)
      const gk = candidateGroupKey(kept)
      if (gk) resourceTrash.value = resourceTrash.value.filter(r => candidateGroupKey(r) !== gk)
      else resourceTrash.value = resourceTrash.value.filter(r => r.id !== kept.id)
      trashTotal.value = Math.max(0, trashTotal.value - 1)
      if (!resourceTrash.value.length && trashTotal.value > 0) await loadResourceTrash()
    } catch (e: any) { error.value = e.message }
    finally { applyingPrimary.value = null }
  }
  async function loadResourceTrash(opts?: { resetPage?: boolean }) {
    if (!active.value) return
    if (opts?.resetPage) {
      suppressTrashPageWatch = true
      trashPage.value = 1
      suppressTrashPageWatch = false
    }
    const projectId = active.value.id
    const token = ++trashLoadToken
    trashLoading.value = true
    try {
      const q = resourceQuery.value.trim()
      const qs = new URLSearchParams({
        page: String(trashPage.value),
        pageSize: String(trashPageSize),
      })
      if (q) qs.set('q', q)
      const data = await api(`/projects/${projectId}/resources/trash?${qs.toString()}`)
      if (token !== trashLoadToken || active.value?.id !== projectId) return
      const items = Array.isArray(data?.items)
        ? data.items as Resource[]
        : (Array.isArray(data) ? data as Resource[] : [])
      resourceTrash.value = items
      trashTotal.value = typeof data?.total === 'number' ? data.total : items.length
      const maxPage = Math.max(1, Math.ceil(trashTotal.value / trashPageSize) || 1)
      if (trashPage.value > maxPage) {
        suppressTrashPageWatch = true
        trashPage.value = maxPage
        suppressTrashPageWatch = false
        if (maxPage !== Number(data?.page || trashPage.value)) {
          await loadResourceTrash()
        }
      }
    } catch {
      if (token === trashLoadToken) {
        resourceTrash.value = []
        trashTotal.value = 0
      }
    } finally {
      if (token === trashLoadToken) trashLoading.value = false
    }
  }
  async function refreshProjectResources() {
    if (!active.value) return
    try {
      const resources = await api(`/projects/${active.value.id}/resources?page=1&pageSize=200&type=all&enrich=0`)
      const items = Array.isArray(resources?.items)
        ? resources.items as Resource[]
        : (Array.isArray(resources) ? resources as Resource[] : [])
      // Keep a broader cache for pickers / shot refs; library grid uses paged loadLibraryPage.
      if (items.length) {
        active.value = { ...active.value, resources: mergeResourceLists(active.value.resources, items) }
      }
      await loadLibraryPage()
    } catch { /* keep local state */ }
  }

  function mergeResourceLists(existing: Resource[], incoming: Resource[]) {
    const byID = new Map<number, Resource>()
    for (const r of existing) byID.set(r.id, r)
    for (const r of incoming) byID.set(r.id, { ...(byID.get(r.id) || r), ...r })
    return [...byID.values()].sort((a, b) => b.id - a.id)
  }
  function canHaveDerivatives(item: Resource) {
    return !item.parentId && (item.type === 'character' || item.type === 'scene')
  }
  function defaultResourceFormType(): 'character' | 'scene' | 'prop' | 'video' {
    const parentType = libraryParent.value?.type
    if (parentType === 'character' || parentType === 'scene' || parentType === 'prop') return parentType
    if (resourceFilter.value === 'all' || resourceFilter.value === 'other') return 'character'
    return resourceFilter.value
  }
  function toggleAddResourceForm() {
    if (showAddForm.value) {
      showAddForm.value = false
      return
    }
    const busy = visibleImageGenJobs.value.some(j => j.status === 'pending' || j.status === 'running')
    const hasCandidates = characterCandidates.value.length > 0
    // Keep in-flight / finished candidate session instead of wiping it.
    if (!busy && !hasCandidates) {
      switchResourceType(defaultResourceFormType())
      candidateCount.value = 1
      if (libraryParentId.value) ensureDerivativeParentRef(libraryParentId.value)
    }
    showAddForm.value = true
  }
  /** Reopen add-resource dialog for a specific job (or the focused / latest one). */
  function openResourceGenerateJob(jobId?: number) {
    const id = jobId ?? focusedImageJobId.value ?? visibleImageGenJobs.value[0]?.id
    const job = id ? imageGenJobs.value.find(j => j.id === id) : null
    if (job && isSpecialImageJobType(job.type)) {
      focusImageJob(job.id)
      return
    }
    studioTab.value = 'resources'
    resourceLibraryTab.value = 'library'
    if (id) focusImageJob(id)
    showAddForm.value = true
  }
  const visibleImageGenJobs = computed(() =>
    imageGenJobs.value.filter(j => !isImageJobDismissed(j.id)),
  )
  const focusedImageJob = computed(() =>
    visibleImageGenJobs.value.find(j => j.id === focusedImageJobId.value) || null,
  )
  const imageGenProgress = computed<ImageGenProgressState>(() => {
    const job = focusedImageJob.value
    if (!job || (job.status !== 'pending' && job.status !== 'running')) return null
    return {
      progress: job.progress,
      message: job.message,
      doneCount: job.doneCount,
      totalCount: job.totalCount,
    }
  })
  const isAnyResourceGenerating = computed(
    () => submittingImageGen.value
      || visibleImageGenJobs.value.some(j => j.status === 'pending' || j.status === 'running'),
  )
  const hasReadyImageJob = computed(() =>
    visibleImageGenJobs.value.some(j => j.status === 'completed' && (j.images?.length || 0) > 0)
    || characterCandidates.value.length > 0,
  )
  function openResourceLibrary() {
    libraryParentId.value = null
    libraryParent.value = null
    resourceLibraryTab.value = 'library'
  }
  function openResourceTrash() {
    libraryParentId.value = null
    libraryParent.value = null
    resourceLibraryTab.value = 'trash'
    void loadResourceTrash({ resetPage: true })
  }
  async function purgeResourceTrash(item: Resource) {
    if (!confirm(`彻底删除「${item.name}」？此操作不可恢复`)) return
    await api(`/resources/${item.id}/permanent`, { method: 'DELETE' })
    trashTotal.value = Math.max(0, trashTotal.value - 1)
    if (resourceTrash.value.length <= 1 && trashPage.value > 1) {
      trashPage.value -= 1
      await loadResourceTrash()
    } else {
      resourceTrash.value = resourceTrash.value.filter(r => r.id !== item.id)
      if (!resourceTrash.value.length) await loadResourceTrash()
    }
  }
  function pickCharacterRef(id: number, variant: 'stylized' | 'original') {
    applyShotRef({ kind: 'character', id, variant })
  }
  function pickSceneRef(id: number, variant: 'stylized' | 'original') {
    applyShotRef({ kind: 'scene', id, variant })
  }
  function pickPropRef(id: number) {
    applyShotRef({ kind: 'prop', id })
  }
  function pickOtherRef(id: number, variant: 'stylized' | 'original' = 'stylized') {
    applyShotRef({ kind: 'other', id, variant })
  }
  function pickRecentRef(ref: ShotRef) {
    applyShotRef(ref)
  }
  function readFileAsDataURL(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result || ''))
      reader.onerror = () => reject(new Error('读取图片失败'))
      reader.readAsDataURL(file)
    })
  }
  function shotRefResourceName(filename?: string) {
    const base = (filename || '').replace(/\.[^.]+$/, '').trim()
    if (base) return base
    const n = (others.value.length + 1).toString().padStart(2, '0')
    return `参考图 ${n}`
  }
  async function uploadShotRefImage(shot: Shot, imageData: string, name: string) {
    if (!active.value) return
    if (shot.refs.length >= maxShotRefs) {
      error.value = `最多 ${maxShotRefs} 张参考图`
      return
    }
    uploadingShotRef.value = shot.id
    error.value = ''
    expandShot(shot.id)
    try {
      const item = await api(`/projects/${active.value.id}/resources`, {
        method: 'POST',
        body: JSON.stringify({
          type: 'other',
          name,
          description: '分镜参考图',
          imageData,
        }),
      })
      await refreshProjectResources()
      addShotRef(shot, { kind: 'other', id: item.id })
    } catch (e: any) {
      error.value = e.message
    } finally {
      uploadingShotRef.value = null
    }
  }
  async function uploadShotRefFromFile(shot: Shot, file: File) {
    if (!file.type.startsWith('image/')) {
      error.value = '请选择图片文件'
      return
    }
    if (!active.value) return
    if (shot.refs.length >= maxShotRefs) {
      error.value = `最多 ${maxShotRefs} 张参考图`
      return
    }
    uploadingShotRef.value = shot.id
    error.value = ''
    expandShot(shot.id)
    try {
      if (await cosEnabled()) {
        const name = shotRefResourceName(file.name)
        const presign = await api(`/projects/${active.value.id}/resources/direct-upload`, {
          method: 'POST',
          body: JSON.stringify({
            type: 'other',
            name,
            description: '分镜参考图',
            filename: file.name,
            contentType: file.type || 'image/jpeg',
          }),
        }) as CosPresign & { resourceId: number; ext: string }
        await putFileToCos(presign, file)
        const item = await api(`/resources/${presign.resourceId}/confirm-image`, {
          method: 'POST',
          body: JSON.stringify({ ext: presign.ext, key: presign.key }),
        })
        await refreshProjectResources()
        addShotRef(shot, { kind: 'other', id: item.id })
        return
      }
      const imageData = await readFileAsDataURL(file)
      await uploadShotRefImage(shot, imageData, shotRefResourceName(file.name))
    } catch (e: any) {
      error.value = e.message
    } finally {
      uploadingShotRef.value = null
    }
  }
  async function onShotRefFiles(shot: Shot, e: Event) {
    const input = e.target as HTMLInputElement
    const files = input.files ? Array.from(input.files) : []
    input.value = ''
    if (!files.length) return
    for (const file of files) {
      if (shot.refs.length >= maxShotRefs) {
        error.value = `最多 ${maxShotRefs} 张参考图`
        break
      }
      await uploadShotRefFromFile(shot, file)
    }
  }
  async function onShotComposerPaste(shot: Shot, e: ClipboardEvent) {
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of items) {
      if (!item.type.startsWith('image/')) continue
      e.preventDefault()
      const file = item.getAsFile()
      if (file) await uploadShotRefFromFile(shot, file)
      return
    }
  }
  function toggleAdvanced(shotId: number) { showAdvanced.value[shotId] = !showAdvanced.value[shotId] }
  function shotVideoSrc(shot: Shot) {
    if (!shot.videoUrl) return ''
    const v = shot.activeVideoResourceId || shot.updatedAt || shot.id
    return `${shot.videoUrl}${shot.videoUrl.includes('?') ? '&' : '?'}v=${v}`
  }
  function shotVideoVersions(shotId: number) {
    return (active.value?.resources || [])
      .filter(r => r.type === 'video' && r.shotId === shotId)
      .sort((a, b) => b.id - a.id)
  }
  function shotActiveVideoResource(shot: Shot): Resource | undefined {
    const versions = shotVideoVersions(shot.id)
    if (!versions.length) return undefined
    if (shot.activeVideoResourceId) {
      return versions.find(v => v.id === shot.activeVideoResourceId) || versions[0]
    }
    return versions[0]
  }
  function shotVideoScript(shot: Shot) {
    const video = shotActiveVideoResource(shot)
    return (video?.genScript || video?.description || shot.script).trim()
  }
  function shotSummary(shot: Shot) {
    const text = shotVideoScript(shot).trim()
    return text || '暂无描述'
  }
  function formatShotDateTime(iso?: string) {
    if (!iso) return ''
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return ''
    return d.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
  }
  function shotVideoProducedAt(shot: Shot): string | undefined {
    const video = shotActiveVideoResource(shot)
    return video?.createdAt
  }
  function shotTimeLabel(shot: Shot) {
    if (shot.videoUrl || shot.status === 'done' || shot.status === 'error') {
      const ts = shotVideoProducedAt(shot)
      if (ts) return `制作于 ${formatShotDateTime(ts)}`
    }
    if (shot.createdAt) return `创建于 ${formatShotDateTime(shot.createdAt)}`
    return ''
  }
  function shotVideoMeta(shot: Shot) {
    const video = shotActiveVideoResource(shot)
    if (!video) return null
    const styles = [video.genProjectStyle, video.genVisualStyle].map(s => (s || '').trim()).filter(Boolean)
    const style = [...new Set(styles)].join(' · ')
    const model = (video.genModelName || '').trim() || (video.source === 'upload' ? '本地上传' : '')
    const modelDetail = video.genModelId && model ? `${model} · ${video.genModelId}` : model
    if (!style && !modelDetail) return null
    return {
      style,
      model: modelDetail,
      provider: (video.genProviderName || '').trim(),
    }
  }
  function isActiveShotVideo(item: Resource) {
    if (!item.shotId || !active.value) return false
    for (const ep of active.value.episodes) {
      const shot = (ep.shots || []).find(s => s.id === item.shotId)
      if (shot?.activeVideoResourceId === item.id) return true
    }
    return false
  }
  async function useShotVideo(item: Resource) {
    if (!item.shotId || isActiveShotVideo(item)) return
    applyingShotVideo.value = item.id
    error.value = ''
    try {
      const result = await api(`/shots/${item.shotId}/use-video`, {
        method: 'POST',
        body: JSON.stringify({ resourceId: item.id }),
      })
      replaceShot(normalizeShot(result.shot ?? result))
      studioTab.value = 'episodes'
    } catch (e: any) { error.value = e.message }
    finally { applyingShotVideo.value = null }
  }
  async function goToShot(item: Resource) {
    if (!active.value || !item.shotId) return
    try {
      const raw = await api(`/shots/${item.shotId}`)
      const shot = normalizeShot(raw)
      const ep = active.value.episodes.find(e => e.id === shot.episodeId)
      if (!ep) return
      studioTab.value = 'episodes'
      activeEpisode.value = normalizeEpisode({ ...ep, shots: [] })
      const page = Math.floor(Math.max(0, shot.sortOrder - 1) / shotPageSize) + 1
      expandShot(shot.id)
      await loadShotPage(page, { force: true })
    } catch (e: any) {
      error.value = e.message || '无法定位分镜'
    }
  }
  function resourceDisplayName(item: Resource) {
    const p = parseCandidateName(item.name)
    const base = p && item.isGroupPrimary ? p.base : item.name
    if (libraryParentId.value && item.parentId) return base
    if (item.parentName?.trim()) return `${item.parentName.trim()} · ${base}`
    return base
  }
  function resourceEditableName(item: Resource) {
    const p = parseCandidateName(item.name)
    if (p) return p.base
    return item.name
  }
  function buildResourceName(item: Resource, name: string) {
    const trimmed = name.trim()
    const p = parseCandidateName(item.name)
    if (p) return `${trimmed} · 候选${p.index}`
    return trimmed
  }
  function resourceTypeLabel(type: Resource['type']) {
    return resourceFormTypeLabel(type)
  }
  function resourceSourceLabel(source: Resource['source']) {
    if (source === 'ai') return 'AI 生成'
    if (source === 'upload') return '手动上传'
    return ''
  }
  function providerName(id: number) { return providers.value.find(p => p.id === id)?.name || '' }
  function videoModelLabel(m: AIModel, opts?: { prefix?: string; showDefault?: boolean }) {
    const base = `${providerName(m.providerId)} · ${m.name} · ${m.modelId}`
    const defaultTag = opts?.showDefault !== false && m.isDefault ? ' · 默认' : ''
    return opts?.prefix ? `${opts.prefix} · ${base}${defaultTag}` : `${base}${defaultTag}`
  }
  function imageModelLabel(m: AIModel, opts?: { prefix?: string; showDefault?: boolean }) {
    const base = `${providerName(m.providerId)} · ${m.name}`
    const defaultTag = opts?.showDefault !== false && m.isDefault ? ' · 默认' : ''
    return opts?.prefix ? `${opts.prefix} · ${base}${defaultTag}` : `${base}${defaultTag}`
  }
  function isNanoBananaImageModel(m: AIModel | null | undefined): boolean {
    if (!m) return false
    return /nano[\s_-]*banana/i.test(`${m.name} ${m.modelId}`)
  }

  function looksLikeSceneGridPrompt(text: string): boolean {
    return /同一(建筑|空间)连续摄影|【九宫格摄影机矩阵】|ArchViz|【建筑主体】|【空间主体】/.test(text || '')
  }

  function looksLikeLegacySceneGridPrompt(text: string): boolean {
    return /同一建筑连续摄影|ArchViz|屋顶结构|山体结构|【建筑主体】/.test(text || '')
  }

  function looksLikeStaleSceneGridPrompt(text: string): boolean {
    if (looksLikeLegacySceneGridPrompt(text)) return true
    // Current matrix already bans CAD in overhead cells.
    if (/瓶口朝镜头/.test(text || '') && /格7/.test(text || '')
      && (/禁止白底黑线/.test(text || '') || /禁止 CAD/.test(text || '') || /建筑平面图/.test(text || ''))) {
      return false
    }
    // Older matrix had 格7 but still let Seedream paste the floor-plan CAD into overhead cells.
    if (/瓶口朝镜头/.test(text || '') && /格7/.test(text || '')) return true
    return /【九宫格摄影机矩阵】|房间内部绕主活动区|同一空间连续摄影/.test(text || '')
  }

  function extractSceneGridSubject(text: string): string {
    const m = (text || '').match(/【(?:建筑|空间)主体】\s*([\s\S]*?)(?=\n【|$)/)
    let raw = (m?.[1] || '').trim()
    raw = raw.replace(/同一(建筑|空间)连续摄影[\s\S]*/, '').trim()
    return raw
  }

  function sceneNameWithoutGridSuffix(name: string): string {
    return (name || '')
      .replace(/\s*·\s*(反打骨架|反打|9宫格)\s*$/u, '')
      .replace(/\s*·\s*候选\s*\d+\s*$/u, '')
      .trim()
  }

  function sceneGridIdentityFromResource(item?: Resource): { name: string; description: string } {
    const name = sceneNameWithoutGridSuffix(
      (item ? (resourceEditableName(item) || item.name) : '') || '',
    )
    let desc = (item?.description || '').trim()
    if (looksLikeSceneGridPrompt(desc)) {
      desc = extractSceneGridSubject(desc)
      if (looksLikeSceneGridPrompt(desc)) desc = ''
    }
    if (name && desc.startsWith(`${name}：`)) desc = desc.slice(name.length + 1).trim()
    return { name, description: desc }
  }

  function refillSceneGridPromptFromIdentity(name: string, description: string) {
    return buildSceneGridPromptTemplate(name, description, active.value?.style || '')
  }
  /** Effective image model currently selected for gen / stylize. */
  function effectiveImageModel(): AIModel | null {
    const id = effectiveImageModelId.value
    if (id) {
      const hit = imageModels.value.find(m => m.id === id)
      if (hit) return hit
    }
    return defaultImageModel.value
  }
  function ensureShotModel(shot: Shot) {
    if (!shot.videoModelId && defaultVideoModelId.value) shot.videoModelId = defaultVideoModelId.value
  }
  function sceneReferenceKey(ref: SceneReference) {
    return ref.key
  }
  function sceneRefFromShotRef(ref: ShotRef): SceneReference | null {
    const preview = refThumb(ref)
    if (!preview) return null
    const variant = ref.kind === 'prop' ? undefined : (ref.variant || 'stylized')
    const key = ref.kind === 'prop'
      ? `resource:${ref.id}`
      : `resource:${ref.id}:${variant}`
    return {
      key,
      source: 'resource',
      resourceId: ref.id,
      kind: ref.kind,
      variant,
      previewUrl: preview,
      label: (ref.label || '').trim() || refLabel(ref),
    }
  }
  function isSceneRefSelected(ref: ShotRef) {
	if (refPickerTarget.value === 'positioning' && positioningPickingSkeleton.value) return false
    const item = sceneRefFromShotRef(ref)
    if (!item) return false
    const replaceIdx = activeReplaceIndex()
    if (refPickerTarget.value !== 'resource' && replaceIdx != null) {
      const cur = refListForTarget(refPickerTarget.value).value[replaceIdx]
      return !!cur && cur.key === item.key
    }
    return refPickerReferences.value.some(r => r.key === item.key)
  }
  function isSceneRefDisabled(ref: ShotRef) {
	if (refPickerTarget.value === 'positioning' && positioningPickingSkeleton.value) return false
    if (isSceneRefSelected(ref)) return false
    const replaceIdx = activeReplaceIndex()
    if (refPickerTarget.value !== 'resource' && replaceIdx != null) {
      const item = sceneRefFromShotRef(ref)
      if (!item) return true
      return refListForTarget(refPickerTarget.value).value.some((r, i) => i !== replaceIdx && r.key === item.key)
    }
    return refPickerReferences.value.length >= refPickerMax.value
  }
  function syncPositioningPromptLegend() {
    const modal = positioningModal.value
    if (!modal) return
    const { body } = splitPositioningPrompt(modal.prompt || '')
    positioningModal.value = {
      ...modal,
      prompt: joinPositioningPrompt(body, positioningRefs.value),
    }
  }

  function splitPositioningPrompt(prompt: string): { body: string; legend: string } {
    const text = (prompt || '').trim()
    if (!text) return { body: '', legend: '' }
    const lines = text.split('\n')
    for (let i = lines.length - 1; i >= 0; i--) {
      const line = lines[i].trim()
      if (!line) continue
      if (/^参考图[：:]/.test(line)) {
        return {
          body: lines.slice(0, i).join('\n').trim(),
          legend: line,
        }
      }
      break
    }
    const inline = text.match(/^(.*?)([。.!！\s]*)(参考图[：:].+)\s*$/s)
    if (inline && inline[1].trim() && inline[3]) {
      return { body: inline[1].trim(), legend: inline[3].trim() }
    }
    return { body: text, legend: '' }
  }

  function buildPositioningLegend(refs: SceneReference[]): string {
    if (!refs.length) return ''
    const parts = refs.map((r, i) => `图${i + 1}为${r.label}`)
    return `参考图：${parts.join('，')}`
  }

  function joinPositioningPrompt(body: string, refs: SceneReference[]): string {
    const cleaned = (body || '').trim()
    const legend = buildPositioningLegend(refs)
    if (!cleaned) return legend
    if (!legend) return cleaned
    return `${cleaned}\n\n${legend}`
  }

  function renamePositioningRef(index: number, label: string) {
    if (index < 0 || index >= positioningRefs.value.length) return
    const trimmed = label.trim()
    const current = positioningRefs.value[index]
    const nextLabel = trimmed || current.label
    if (current.label === nextLabel) return
    positioningRefLabelOverrides.set(current.key, nextLabel)
    const next = [...positioningRefs.value]
    next[index] = { ...current, label: nextLabel }
    positioningRefs.value = next
    syncPositioningPromptLegend()
  }

  /** Live-edit while typing so status polls don't wipe the input. */
  function updatePositioningRefLabel(index: number, label: string) {
    if (index < 0 || index >= positioningRefs.value.length) return
    const current = positioningRefs.value[index]
    if (current.label === label) return
    positioningRefLabelOverrides.set(current.key, label)
    const next = [...positioningRefs.value]
    next[index] = { ...current, label }
    positioningRefs.value = next
    syncPositioningPromptLegend()
  }

  function positioningPromptLegendLines(): string[] {
    return positioningRefs.value.map((r, i) => `图${i + 1}为${r.label}`)
  }

  function syncActivePickerLegend() {
    if (refPickerTarget.value === 'positioning') syncPositioningPromptLegend()
    else if (refPickerTarget.value === 'motionGrid') syncMotionGridPromptLegend()
  }
  function addToActiveRefList(ref: SceneReference) {
	if (refPickerTarget.value === 'positioning' && positioningPickingSkeleton.value) {
		const modal = positioningModal.value
		if (!modal) return
		positioningModal.value = {
			...modal,
			skeleton: { url: ref.previewUrl, resourceId: ref.resourceId },
		}
		positioningPickingSkeleton.value = false
		sceneRefPickerOpen.value = false
		return
	}
    if (refPickerTarget.value === 'sceneGrid' && sceneGridPickingOverhead.value) {
      if (!sceneGridModal.value) return
      sceneGridModal.value = {
        ...sceneGridModal.value,
        overheadSketch: {
          url: ref.previewUrl,
          resourceId: ref.resourceId,
          imageData: ref.imageData,
        },
      }
      sceneGridPickingOverhead.value = false
      sceneRefPickerOpen.value = false
      return
    }
    if (refPickerTarget.value === 'sceneReverse' && sceneReversePickingSkeleton.value) {
      const modal = sceneReverseModal.value
      const resource = ref.resourceId ? resourceById(ref.resourceId) : undefined
      if (!modal) return
      if (resource && resource.genType !== 'scene_reverse_skeleton') {
        error.value = '请选择“反打骨架”类型的线稿图'
        return
      }
      sceneReverseModal.value = {
        ...modal,
        skeleton: { url: ref.previewUrl, resourceId: ref.resourceId },
        prompt: buildSceneReversePrompt(
          sceneNameWithoutGridSuffix(modal.name),
          sceneReverseSubjectFromPrompt(modal.prompt, modal.name),
        ),
      }
      sceneReversePickingSkeleton.value = false
      sceneRefPickerOpen.value = false
      return
    }
    const max = refPickerMax.value
    const list = refListForTarget(refPickerTarget.value)
    const named = {
      ...ref,
      label: (ref.label || '').trim() || '参考图',
    }
    const replaceIdx = activeReplaceIndex()
    if (refPickerTarget.value !== 'resource' && replaceIdx != null) {
      const idx = replaceIdx
      if (idx < 0 || idx >= list.value.length) {
        setActiveReplaceIndex(null)
        return
      }
      if (list.value.some((r, i) => i !== idx && r.key === named.key)) {
        error.value = '该参考图已在列表中'
        return
      }
      const prevLabel = (list.value[idx]?.label || '').trim()
      const next = [...list.value]
      next[idx] = prevLabel ? { ...named, label: prevLabel } : named
      list.value = next
      setActiveReplaceIndex(null)
      sceneRefPickerOpen.value = false
      syncActivePickerLegend()
      return
    }
    if (list.value.length >= max) {
      error.value = `最多添加 ${max} 张参考图`
      return
    }
    if (list.value.some(r => r.key === named.key)) return
    list.value = [...list.value, named]
    syncActivePickerLegend()
    if (refPickerTarget.value === 'resource') {
      characterCandidates.value = []
      selectedCandidate.value = ''
    }
  }
  function addSceneReference(ref: SceneReference) {
    const prev = refPickerTarget.value
    refPickerTarget.value = 'resource'
    addToActiveRefList(ref)
    refPickerTarget.value = prev
  }
  function removeSceneReference(key: string) {
    sceneReferences.value = sceneReferences.value.filter(r => r.key !== key)
  }
  function clearSceneReferences() {
    sceneReferences.value = []
    characterCandidates.value = []
    selectedCandidate.value = ''
  }
  function removePositioningReference(key: string) {
    positioningRefs.value = positioningRefs.value.filter(r => r.key !== key)
    if (positioningReplaceIndex.value != null) {
      if (positioningReplaceIndex.value >= positioningRefs.value.length) {
        positioningReplaceIndex.value = null
      }
    }
    syncPositioningPromptLegend()
  }
  function clearPositioningReferences() {
    positioningRefs.value = []
    positioningReplaceIndex.value = null
    syncPositioningPromptLegend()
  }
  function toggleSceneShotRef(ref: ShotRef) {
    const item = sceneRefFromShotRef(ref)
    if (!item) return
    if (refPickerTarget.value === 'sceneGrid' && sceneGridPickingOverhead.value) {
      addToActiveRefList(item)
      rememberShotRef(ref)
      return
    }
    if (refPickerTarget.value !== 'resource' && activeReplaceIndex() != null) {
      addToActiveRefList(item)
      rememberShotRef(ref)
      return
    }
    if (isSceneRefSelected(ref)) {
      if (refPickerTarget.value === 'positioning') removePositioningReference(item.key)
      else if (refPickerTarget.value === 'motionGrid') removeMotionGridReference(item.key)
      else if (refPickerTarget.value === 'sceneGrid') removeSceneGridReference(item.key)
      else if (refPickerTarget.value === 'sceneReverse') removeSceneReverseReference(item.key)
      else removeSceneReference(item.key)
      return
    }
    addToActiveRefList(item)
    rememberShotRef(ref)
  }
  function pickSceneCharacterRef(id: number, variant: 'stylized' | 'original') {
    toggleSceneShotRef({ kind: 'character', id, variant })
  }
  function pickSceneSceneRef(id: number, variant: 'stylized' | 'original') {
    if (refPickerTarget.value === 'sceneReverse') {
      const r = resourceById(id)
      if (r?.genType === 'scene_grid') {
        void selectSceneReverseGrid(r)
        sceneRefPickerOpen.value = false
        return
      }
      if (r?.genType === 'scene_grid_cell' && r.gridId) {
        const grid = resourceById(r.gridId)
        if (grid) {
          void selectSceneReverseGrid(grid)
          sceneRefPickerOpen.value = false
          return
        }
      }
    }
    toggleSceneShotRef({ kind: 'scene', id, variant })
  }
  function pickScenePropRef(id: number) {
    toggleSceneShotRef({ kind: 'prop', id })
  }
  function pickSceneOtherRef(id: number, variant: 'stylized' | 'original' = 'stylized') {
    toggleSceneShotRef({ kind: 'other', id, variant })
  }
  function pickSceneRecentRef(ref: ShotRef) {
    toggleSceneShotRef(ref)
  }
  async function addSceneRefFromFile(file: File, labelPrefix = '本地上传') {
    if (!file.type.startsWith('image/')) {
      error.value = '请选择图片文件'
      return false
    }
    if (sceneReferences.value.length >= maxSceneReferences) {
      error.value = `最多添加 ${maxSceneReferences} 张参考图`
      return false
    }
    const imageData = await readFileAsDataURL(file)
    const label = file.name
      ? `${labelPrefix} · ${file.name}`
      : `${labelPrefix} · 粘贴图片`
    addSceneReference({
      key: `upload:${file.name || 'paste'}:${Date.now()}:${Math.random()}`,
      source: 'upload',
      imageData,
      previewUrl: imageData,
      label,
    })
    return true
  }
  function onSceneRefFiles(e: Event) {
    const input = e.target as HTMLInputElement
    const files = input.files ? Array.from(input.files) : []
    input.value = ''
    if (!files.length) return
    void (async () => {
      let added = 0
      for (const file of files) {
        if (sceneReferences.value.length >= maxSceneReferences) break
        if (await addSceneRefFromFile(file)) added++
      }
      if (added < files.length) error.value = `最多添加 ${maxSceneReferences} 张参考图`
    })()
  }
  async function onSceneRefPaste(e: ClipboardEvent) {
    if (!['character', 'scene', 'prop'].includes(resourceForm.value.type)) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of items) {
      if (!item.type.startsWith('image/')) continue
      e.preventDefault()
      const file = item.getAsFile()
      if (!file) return
      await addSceneRefFromFile(file, '粘贴')
      return
    }
  }
  async function onResourceFormPaste(e: ClipboardEvent) {
    if (resourceForm.value.type === 'video') return
    if (!['character', 'scene', 'prop'].includes(resourceForm.value.type)) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of items) {
      if (!item.type.startsWith('image/')) continue
      e.preventDefault()
      const file = item.getAsFile()
      if (!file) return
      resourceImageFile.value = file
      resourceForm.value.imageData = await readFileAsDataURL(file)
      selectedCandidate.value = ''
      characterCandidates.value = []
      return
    }
  }
  async function onResourceFile(e: Event) {
    const file = (e.target as HTMLInputElement).files?.[0]
    if (!file) return
    resourceImageFile.value = file
    resourceForm.value.imageData = await readFileAsDataURL(file)
    selectedCandidate.value = ''
    characterCandidates.value = []
    videoFiles.value = []
  }
  function clearResourceImage() {
    resourceForm.value.imageData = ''
    resourceImageFile.value = null
    characterCandidates.value = []
    selectedCandidate.value = ''
  }
  function openSceneRefPicker() {
    refPickerTarget.value = 'resource'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  function openPositioningRefPicker() {
	positioningPickingSkeleton.value = false
    positioningReplaceIndex.value = null
    refPickerTarget.value = 'positioning'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  function openPositioningReplacePicker(index: number) {
    if (index < 0 || index >= positioningRefs.value.length) return
	positioningPickingSkeleton.value = false
    positioningReplaceIndex.value = index
    refPickerTarget.value = 'positioning'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  function openPositioningSkeletonPicker() {
	if (!positioningModal.value) return
	positioningReplaceIndex.value = null
	positioningPickingSkeleton.value = true
	refPickerTarget.value = 'positioning'
	sceneRefPickerOpen.value = true
	void refreshProjectResources()
  }
  function onPositioningSkeletonFile(e: Event) {
	const input = e.target as HTMLInputElement
	const file = input.files?.[0]
	input.value = ''
	if (!file) return
	if (!file.type.startsWith('image/')) {
		error.value = '请选择图片文件'
		return
	}
	void (async () => {
		const imageData = await readFileAsDataURL(file)
		const modal = positioningModal.value
		if (!modal) return
		positioningModal.value = {
			...modal,
			skeleton: { url: imageData },
		}
	})()
  }
  function closeSceneRefPicker() {
    sceneRefPickerOpen.value = false
	positioningReplaceIndex.value = null
	positioningPickingSkeleton.value = false
    motionGridReplaceIndex.value = null
    sceneGridReplaceIndex.value = null
    sceneGridPickingOverhead.value = false
    sceneReverseReplaceIndex.value = null
    sceneReversePickingSkeleton.value = false
  }
  function onVideoFiles(e: Event) {
    const input = e.target as HTMLInputElement
    videoFiles.value = input.files ? Array.from(input.files) : []
    resourceForm.value.imageData = ''
    characterCandidates.value = []
    selectedCandidate.value = ''
  }
  async function uploadVideos() {
    if (!active.value || !videoFiles.value.length) return
    uploadingVideos.value = true
    error.value = ''
    try {
      if (await cosEnabled()) {
        const data = await api(`/projects/${active.value.id}/resources/direct-upload-videos`, {
          method: 'POST',
          body: JSON.stringify({
            name: resourceForm.value.name.trim() || '上传视频',
            description: resourceForm.value.description,
            remark: resourceForm.value.description,
            files: videoFiles.value.map(f => ({
              filename: f.name,
              contentType: f.type || 'video/mp4',
            })),
          }),
        })
        const items = (data.items || []) as Array<CosPresign & { resourceId: number; ext: string; filename?: string }>
        const saved: Resource[] = []
        for (let i = 0; i < items.length; i++) {
          const file = videoFiles.value[i]
          const item = items[i]
          if (!file || !item) continue
          await putFileToCos(item, file)
          const resource = await api(`/resources/${item.resourceId}/confirm-video`, {
            method: 'POST',
            body: JSON.stringify({ ext: item.ext, key: item.key }),
          })
          saved.push(resource)
        }
        mergeNewResources(saved)
        await refreshProjectResources()
        resetResourceForm()
        return
      }
      const fd = new FormData()
      fd.append('name', resourceForm.value.name.trim() || '上传视频')
      fd.append('description', resourceForm.value.description)
      fd.append('remark', resourceForm.value.description)
      for (const f of videoFiles.value) fd.append('videos', f)
      const r = await fetch(`/api/projects/${active.value.id}/resources/upload-videos`, { method: 'POST', body: fd })
      const raw = await r.text()
      let data: any = {}
      try { data = raw ? JSON.parse(raw) : {} } catch { throw new Error('后端服务不可用，请确认后端已启动') }
      if (!r.ok) throw new Error(data.error || `请求失败（HTTP ${r.status}）`)
      mergeNewResources(data.resources || [])
      await refreshProjectResources()
      resetResourceForm()
    } catch (e: any) { error.value = e.message }
    finally { uploadingVideos.value = false }
  }
  function submitResourceForm() {
    if (resourceForm.value.type === 'video') uploadVideos()
    else createResource()
  }
  async function pollImageGenerationJob(projectId: number, jobId: number) {
    const token = (imageGenPollTokens.get(jobId) || 0) + 1
    imageGenPollTokens.set(jobId, token)
    imageGenPolling.add(jobId)
    try {
      while (true) {
        if (imageGenPollTokens.get(jobId) !== token || active.value?.id !== projectId) {
          throw new Error('生成任务已取消')
        }
        const job = await api(`/projects/${projectId}/resources/generate-jobs/${jobId}`)
        upsertImageJobFromApi(job)
        syncGeneratingFlags()
        if (job.status === 'completed') return job
        if (job.status === 'failed') throw new Error(job.error || job.message || '图像生成失败')
        await new Promise(resolve => setTimeout(resolve, 1500))
      }
    } finally {
      if (imageGenPollTokens.get(jobId) === token) imageGenPolling.delete(jobId)
    }
  }

  function imageJobDismissKey(jobId: number) {
    return `novaly:img-job-dismissed:${Number(jobId)}`
  }
  function isImageJobDismissed(jobId: number) {
    const id = Number(jobId)
    if (!id) return false
    try {
      const key = imageJobDismissKey(id)
      if (localStorage.getItem(key) === '1') return true
      // Migrate dismissals made before this fix from session-only storage.
      if (sessionStorage.getItem(key) === '1') {
        localStorage.setItem(key, '1')
        return true
      }
      return false
    } catch { return false }
  }
  function persistDismissedImageJob(jobId: number) {
    const id = Number(jobId)
    if (!id || !active.value) return
    void api(`/projects/${active.value.id}/resources/generate-jobs/${id}/dismiss`, { method: 'POST' }).catch(() => {})
  }
  function dismissImageJob(jobId: number | null) {
    const id = Number(jobId)
    if (!id) return
    try {
      const key = imageJobDismissKey(id)
      localStorage.setItem(key, '1')
      sessionStorage.setItem(key, '1')
    } catch { /* ignore */ }
    persistDismissedImageJob(id)
    imageGenJobs.value = imageGenJobs.value.filter(j => j.id !== id)
    if (focusedImageJobId.value === id) {
      focusedImageJobId.value = visibleImageGenJobs.value[0]?.id ?? null
    }
  }
  function dismissImageJobFromPanel(jobId: number, event?: Event) {
    event?.stopPropagation()
    dismissImageJob(jobId)
    if (!visibleImageGenJobs.value.some(j => j.id === focusedImageJobId.value)) {
      characterCandidates.value = []
      selectedCandidate.value = ''
    }
  }
  function clearTransientImageGenState() {
    imageGenResumeToken++
    for (const id of imageGenPollTokens.keys()) {
      imageGenPollTokens.set(id, (imageGenPollTokens.get(id) || 0) + 1)
    }
    imageGenPolling.clear()
    imageGenJobs.value = []
    focusedImageJobId.value = null
    submittingImageGen.value = false
    generatingCharacter.value = false
    generatingScene.value = false
    generatingProp.value = false
    characterCandidates.value = []
    selectedCandidate.value = ''
    lastCharacterPrompt.value = ''
    lastScenePrompt.value = ''
    lastPropPrompt.value = ''
  }
  function syncGeneratingFlags() {
    generatingCharacter.value = imageGenJobs.value.some(j => j.type === 'character' && (j.status === 'pending' || j.status === 'running'))
    generatingScene.value = imageGenJobs.value.some(j => j.type === 'scene' && (j.status === 'pending' || j.status === 'running'))
    generatingProp.value = imageGenJobs.value.some(j => j.type === 'prop' && (j.status === 'pending' || j.status === 'running'))
  }
  function resourceTypeLabelShort(type: string) {
    return ({
      character: '角色',
      scene: '场景',
      prop: '道具',
      positioning: '站位图',
      positioning_skeleton: '火柴人骨架',
      scene_grid: '场景9宫格',
      scene_reverse: '反打图',
      scene_reverse_skeleton: '反打骨架',
      scene_panorama: '场景全景',
      scene_panorama_view: '全景机位',
      motion_grid: '9帧图',
      motion_grid_cell: '帧画面',
      scene_grid_cell: '格画面',
    } as Record<string, string>)[type] || type
  }
  function snapshotSceneRefsForJob(): ResourceGenRef[] {
    return sceneReferences.value.map((r, i) => ({
      id: r.resourceId || 0,
      variant: r.variant || (r.source === 'upload' ? 'original' : ''),
      kind: r.kind || (r.source === 'upload' ? 'other' : ''),
      label: r.label || `参考 ${i + 1}`,
      imageUrl: r.previewUrl || '',
    }))
  }
  function snapshotPositioningRefsForJob(): ResourceGenRef[] {
    return positioningRefs.value.map((r, i) => ({
      id: r.resourceId || 0,
      variant: r.variant || (r.source === 'upload' ? 'original' : ''),
      kind: r.kind || (r.source === 'upload' ? 'other' : ''),
      label: r.label || `参考 ${i + 1}`,
      imageUrl: r.previewUrl || '',
    }))
  }
  function normalizeImageJob(job: any): ImageGenJobView {
    const input = job?.input || {}
    const resourceRefs = Array.isArray(input.resourceRefs)
      ? (input.resourceRefs as ResourceGenRef[])
      : undefined
    const description = typeof input.description === 'string' ? input.description : undefined
    return {
      id: Number(job.id) || job.id,
      projectId: job.projectId,
      type: job.type,
      status: job.status,
      progress: job.progress ?? 0,
      message: job.message || (job.status === 'failed' ? (job.error || '生成失败') : '生成中…'),
      doneCount: job.doneCount ?? 0,
      totalCount: job.totalCount ?? 1,
      name: (typeof input.name === 'string' && input.name.trim()) ? input.name.trim() : resourceTypeLabelShort(job.type),
      prompt: job.prompt || undefined,
      description,
      resourceRefs,
      error: job.error || undefined,
      images: job.images || undefined,
      resources: job.resources || undefined,
      shotId: typeof input.shotId === 'number' ? input.shotId : undefined,
      targetResourceId: typeof input.targetResourceId === 'number' ? input.targetResourceId : undefined,
    }
  }
  function isSceneGridOverheadJob(job: Partial<ImageGenJobView> | any) {
    const name = typeof job?.name === 'string' ? job.name : ''
    return job?.type === 'scene' && /(俯视布局线稿|二维建筑平面布局图)/.test(name)
  }
  function sceneNameFromOverheadJobName(name: string) {
    return sceneNameWithoutGridSuffix((name || '')
      .replace(/\s*·\s*俯视布局线稿\s*$/u, '')
      .replace(/\s*·\s*二维建筑平面布局图\s*$/u, '')
      .trim())
  }
  function sceneNamesMatchLoose(a: string, b: string) {
    const x = sceneNameWithoutGridSuffix(a || '').replace(/\s+/g, '')
    const y = sceneNameWithoutGridSuffix(b || '').replace(/\s+/g, '')
    if (!x || !y) return false
    if (x === y) return true
    return x.includes(y) || y.includes(x)
  }
  function sceneGridLegendLocalKey(resourceId?: number) {
    const pid = active.value?.id || 0
    const rid = resourceId || 0
    if (!pid || !rid) return ''
    return `novaly:scene-grid-legend:${pid}:${rid}`
  }
  function loadSceneGridLegendLocal(resourceId?: number) {
    const key = sceneGridLegendLocalKey(resourceId)
    if (!key) return ''
    try {
      return String(localStorage.getItem(key) || '').trim()
    } catch {
      return ''
    }
  }
  function saveSceneGridLegendLocal(resourceId: number | undefined, legend: string) {
    const key = sceneGridLegendLocalKey(resourceId)
    const text = (legend || '').trim()
    if (!key || !text) return
    try {
      localStorage.setItem(key, text)
    } catch { /* ignore quota */ }
  }
  function overheadCandidatesFromJob(job: Partial<ImageGenJobView> | any) {
    const fromResources = ((job?.resources || []) as Resource[])
      .map(r => ({
        url: r.imageUrl || r.stylizedImageUrl || '',
        resourceId: r.id,
        imageData: undefined as string | undefined,
      }))
      .filter(c => !!c.url)
    if (fromResources.length) return fromResources
    return (Array.isArray(job?.images) ? job.images : [])
      .map((x: any) => ({
        url: typeof x?.url === 'string' ? x.url : '',
        resourceId: typeof x?.resourceId === 'number' ? x.resourceId : undefined,
        imageData: undefined as string | undefined,
      }))
      .filter((c: any) => !!c.url)
  }
  function jobMatchesSceneGridModal(job: Partial<ImageGenJobView> | any, modal: NonNullable<SceneGridModalState>) {
    const modalScene = sceneNameWithoutGridSuffix(modal.name || '')
    const jobScene = sceneNameFromOverheadJobName(typeof job?.name === 'string' ? job.name : '')
    if (sceneNamesMatchLoose(modalScene, jobScene)) return true
    const rootId = sceneLibraryRootId(modal.resourceId) || modal.resourceId
    if (!rootId) return false
    const resources = (job?.resources || []) as Resource[]
    if (resources.some(r => r.parentId === rootId || r.id === rootId)) return true
    const inputParent = Number((job as any)?.input?.parentId || (job as any)?.parentId || 0)
    return inputParent > 0 && inputParent === rootId
  }
  function applySceneGridOverheadFromJob(job: Partial<ImageGenJobView> | any) {
    const modal = sceneGridModal.value
    if (!modal || !isSceneGridOverheadJob(job)) return
    if (!jobMatchesSceneGridModal(job, modal)) return

    const candidates = overheadCandidatesFromJob(job)
    if (!candidates.length) return
    const sameSketch = modal.overheadSketch?.url === candidates[0]?.url
      && (modal.overheadSketchCandidates?.length || 0) === candidates.length
      && modal.overheadSketchCandidates?.every((c, i) => c.url === candidates[i]?.url)
    if (sameSketch) {
      if (modal.overheadSubmitting) {
        sceneGridModal.value = { ...modal, overheadSubmitting: false }
      }
      return
    }
    sceneGridModal.value = {
      ...modal,
      overheadSubmitting: false,
      overheadSketchCandidates: candidates,
      overheadSketch: modal.overheadSketch?.url ? modal.overheadSketch : candidates[0],
    }
  }

  function syncSceneGridOverheadFromJobs() {
    const modal = sceneGridModal.value
    if (!modal) return
    const scene = sceneNameWithoutGridSuffix(modal.name || '')
    if (!scene) return
    const running = imageGenJobs.value.find(j =>
      isSceneGridOverheadJob(j)
      && (j.status === 'pending' || j.status === 'running')
      && jobMatchesSceneGridModal(j, modal),
    )
    if (running) {
      if (!modal.overheadSubmitting) {
        sceneGridModal.value = { ...modal, overheadSubmitting: true }
      }
      return
    }
    if (modal.overheadSketch?.url || modal.overheadSketchCandidates?.length) {
      if (modal.overheadSubmitting) {
        sceneGridModal.value = { ...modal, overheadSubmitting: false }
      }
      return
    }
    const done = imageGenJobs.value.find(j =>
      isSceneGridOverheadJob(j)
      && j.status === 'completed'
      && jobMatchesSceneGridModal(j, modal)
      && overheadCandidatesFromJob(j).length > 0,
    )
    if (done) {
      applySceneGridOverheadFromJob(done)
      return
    }
    // Fallback: library already has a floor-plan child for this scene.
    const fromLib = findSceneGridOverheadResource(modal.resourceId, modal.name)
    if (fromLib?.imageUrl || fromLib?.stylizedImageUrl) {
      sceneGridModal.value = {
        ...modal,
        overheadSubmitting: false,
        overheadSketch: {
          url: fromLib.imageUrl || fromLib.stylizedImageUrl || '',
          resourceId: fromLib.id,
        },
      }
    }
  }

  watch(imageGenJobs, () => {
    if (sceneGridModal.value) syncSceneGridOverheadFromJobs()
  }, { deep: true })

  function upsertImageJobFromApi(job: any) {
    const next = normalizeImageJob(job)
    const idx = imageGenJobs.value.findIndex(j => j.id === next.id)
    if (idx >= 0) {
      const copy = [...imageGenJobs.value]
      // Keep earlier input/refs if poll payload omits them
      const prev = copy[idx]
      copy[idx] = {
        ...prev,
        ...next,
        description: next.description ?? prev.description,
        resourceRefs: (() => {
          const a = next.resourceRefs || []
          const b = prev.resourceRefs || []
          if (a.length >= b.length && a.length > 0) return a
          if (b.length > 0) return b
          return a.length ? a : undefined
        })(),
        shotId: next.shotId ?? prev.shotId,
        targetResourceId: next.targetResourceId ?? prev.targetResourceId,
      }
      imageGenJobs.value = copy
    } else {
      imageGenJobs.value = [next, ...imageGenJobs.value]
    }
  }
  function applyImageJobForm(job: ImageGenJobView | any) {
    const t = job?.type
    if (t === 'character' || t === 'scene' || t === 'prop') {
      resourceForm.value.type = t
    }
    const name = typeof job?.name === 'string' ? job.name : ''
    if (name.trim()) {
      resourceForm.value.name = name.replace(/\s*·\s*站位图$/, '') || name
    }
    const description = (
      (typeof job?.description === 'string' && job.description)
      || (typeof job?.prompt === 'string' && job.prompt)
      || ''
    ).trim()
    if (description) {
      resourceForm.value.description = description
    }
    if (typeof job?.totalCount === 'number' && job.totalCount >= 1) {
      candidateCount.value = job.totalCount
    }
    regenerateResourceId.value = typeof job?.targetResourceId === 'number' ? job.targetResourceId : 0
    const refs = job?.resourceRefs as ResourceGenRef[] | undefined
    // A job owns an isolated reference snapshot. Never merge references left in another form session.
    if (Array.isArray(refs)) {
      seedSceneReferencesFromGenRefs(refs)
    }
    if (description) {
      if (t === 'character') lastCharacterPrompt.value = description
      else if (t === 'scene' || t === 'positioning') lastScenePrompt.value = description
      else if (t === 'prop') lastPropPrompt.value = description
    }
  }
  function isSpecialImageJobType(type: string) {
    return type === 'positioning'
      || type === 'positioning_skeleton'
      || type === 'motion_grid'
      || type === 'scene_grid'
      || type === 'scene_reverse'
      || type === 'scene_reverse_skeleton'
      || type === 'scene_panorama'
  }
  function applyImageJobResult(job: ImageGenJobView | any) {
    if (isSpecialImageJobType(job?.type)) {
      // 站位图/骨架/9帧图/9宫格/反打图不回填「添加资源」表单；点任务面板时再打开对应弹窗
      mergeNewResources(job.resources || [])
      return
    }
    applyImageJobForm(job)
    characterCandidates.value = job.images || []
    mergeNewResources(job.resources || [])
    selectDefaultCandidate()
    if (job.prompt) {
      if (job.type === 'character') lastCharacterPrompt.value = job.prompt
      else if (job.type === 'scene') lastScenePrompt.value = job.prompt
      else if (job.type === 'prop') lastPropPrompt.value = job.prompt
    }
  }
  function commitGeneratedLibraryAsset(job: ImageGenJobView | any) {
    if (job?.type !== 'character' && job?.type !== 'scene' && job?.type !== 'prop') return
    if (isSceneGridOverheadJob(job)) return
    const resources = Array.isArray(job?.resources) ? job.resources as Resource[] : []
    const images = Array.isArray(job?.images) ? job.images : []
    const hasImage = resources.some(r => !!(r.imageUrl || r.stylizedImageUrl))
      || images.some((img: { url?: string }) => !!img?.url)
    if (!hasImage) return
    const name = String(job?.name || resourceForm.value.name || '').trim()
    ElMessage.success(name ? `「${name}」已写入资产库` : '图片已写入资产库')
    const targetId = typeof job?.targetResourceId === 'number' ? job.targetResourceId : 0
    const sameForm = showAddForm.value && (
      focusedImageJobId.value === job.id
      || (targetId > 0 && regenerateResourceId.value === targetId)
    )
    if (!sameForm) return
    showAddForm.value = false
    regenerateResourceId.value = 0
    characterCandidates.value = []
    selectedCandidate.value = ''
  }
  function openPositioningJob(job: ImageGenJobView) {
    positioningReplaceIndex.value = null
    const refs = job.resourceRefs || []
    const shotId = job.shotId
    const shot = shotId
      ? (activeEpisode.value?.shots.find(s => s.id === shotId)
        || active.value?.episodes.flatMap(e => e.shots).find(s => s.id === shotId))
      : undefined
    if (refs.length > 0) {
      const keptUploads = positioningRefs.value.filter(r => r.source === 'upload')
      seedPositioningRefsFromGenRefs(refs)
      if (keptUploads.length) {
        const next = [...positioningRefs.value]
        for (const u of keptUploads) {
          if (!next.some(r => r.key === u.key || (u.previewUrl && r.previewUrl === u.previewUrl))) {
            next.push(u)
          }
        }
        positioningRefs.value = next.slice(0, maxPositioningRefs)
      }
    } else if (shot?.positioningRefs?.length) {
      seedPositioningRefsFromGenRefs(shot.positioningRefs)
    }
    let rawPrompt = (job.description || '').trim()
    if (!rawPrompt && shot?.positioningPrompt) rawPrompt = shot.positioningPrompt.trim()
    if (!rawPrompt) rawPrompt = (job.prompt || '').trim()
    const { body } = splitPositioningPrompt(rawPrompt)
    const prompt = rawPrompt
      ? joinPositioningPrompt(body || rawPrompt, positioningRefs.value)
      : ''
    let shotLabel = job.name || '站位图'
    if (shot) {
      shotLabel = shot.label || `分镜 ${shot.sortOrder}`
      expandShot(shot.id)
    }
    const results = job.status === 'completed'
      ? (
          (job.images?.length
            ? job.images.map((img, i) => ({
              url: img.url,
              resourceId: img.resourceId,
              label: `候选 ${i + 1}`,
            }))
            : (job.resources || [])
              .filter(r => r.imageUrl)
              .map((r, i) => ({
                url: r.imageUrl,
                resourceId: r.id,
                label: r.name || `候选 ${i + 1}`,
              }))
          )
        )
      : undefined
    if (job.status === 'completed') {
      mergeNewResources(job.resources || [])
    }
    studioTab.value = 'episodes'
    showAddForm.value = false
    const completedImages = job.status === 'completed' ? results : undefined
    const isSkeletonJob = job.type === 'positioning_skeleton'
    const skeleton = isSkeletonJob && completedImages?.[0]
      ? { url: completedImages[0].url, resourceId: completedImages[0].resourceId }
      : (isSkeletonJob ? undefined : findPositioningSkeletonOnShotId(shotId || 0))
    positioningModal.value = {
      shotId: shotId || 0,
      shotLabel,
      prompt,
      analyzing: false,
      submitting: false,
      skeleton,
      results: isSkeletonJob ? undefined : completedImages,
    }
  }
  function focusImageJob(jobId: number) {
    const job = imageGenJobs.value.find(j => j.id === jobId)
    if (!job) return
    focusedImageJobId.value = jobId
    if (isSpecialImageJobType(job.type)) {
      // Special workflow dialogs are standalone. Never leave the generic image form
      // open above them when a task card is clicked.
      showAddForm.value = false
    }
    if (isSceneGridOverheadJob(job)) {
      openSceneGridOverheadJob(job)
      return
    }
    if (job.type === 'positioning' || job.type === 'positioning_skeleton') {
      openPositioningJob(job)
      return
    }
    if (job.type === 'motion_grid') {
      openMotionGridJob(job)
      return
    }
    if (job.type === 'scene_grid') {
      openSceneGridJob(job)
      return
    }
    if (job.type === 'scene_reverse' || job.type === 'scene_reverse_skeleton') {
      openSceneReverseJob(job)
      return
    }
    if (job.type === 'scene_panorama') {
      openScenePanoramaJob(job)
      return
    }
    applyImageJobForm(job)
    if (job.status === 'completed') {
      characterCandidates.value = job.images || []
      selectedCandidate.value = characterCandidates.value[0]?.url || ''
      if (job.prompt) {
        if (job.type === 'character') lastCharacterPrompt.value = job.prompt
        else if (job.type === 'scene') lastScenePrompt.value = job.prompt
        else if (job.type === 'prop') lastPropPrompt.value = job.prompt
      }
    }
  }

  function openSceneGridOverheadJob(job: ImageGenJobView) {
    const sceneName = sceneNameFromOverheadJobName(job.name || '') || '场景'
    studioTab.value = 'resources'
    resourceLibraryTab.value = 'library'
    if (!sceneGridModal.value || !sceneNamesMatchLoose(sceneGridModal.value.name || '', sceneName)) {
      const parentId = Number((job as any)?.input?.parentId || 0)
      const parent = parentId ? resourceById(parentId) : undefined
      const match = parent || (active.value?.resources || []).find(r =>
        r.type === 'scene'
        && !/(二维建筑平面布局图|俯视布局线稿|9宫格|九宫格)/.test(r.name || '')
        && sceneNamesMatchLoose(resourceEditableName(r) || r.name || '', sceneName),
      )
      openSceneGridModal(match)
      if (sceneGridModal.value) {
        sceneGridModal.value = { ...sceneGridModal.value, name: sceneName }
      }
    }
    if (job.status === 'completed' || job.status === 'running' || job.status === 'pending') {
      applySceneGridOverheadFromJob(job)
      syncSceneGridOverheadFromJobs()
    }
  }
  async function trackImageGenerationJob(
    projectId: number,
    jobId: number,
    applyResult: (result: any) => void,
    emptyMessage: string,
  ) {
    if (imageGenPolling.has(jobId)) return
    focusedImageJobId.value = focusedImageJobId.value ?? jobId
    try {
      const result = await pollImageGenerationJob(projectId, jobId)
      if (active.value?.id !== projectId) return
      upsertImageJobFromApi(result)
      applyResult(result)
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'completed' })
      if (focusedImageJobId.value === jobId) {
        applyImageJobResult(result)
        if (!isSpecialImageJobType(result.type) && !characterCandidates.value.length) {
          error.value = emptyMessage
        }
      } else {
        mergeNewResources(result.resources || [])
      }
      await refreshProjectResources()
      loadResourceTrash()
      commitGeneratedLibraryAsset(result)
    } catch (e: any) {
      if (active.value?.id === projectId && e?.message !== '生成任务已取消') {
        const existing = imageGenJobs.value.find(j => j.id === jobId)
        if (existing) {
          upsertImageJobFromApi({
            ...existing,
            status: 'failed',
            message: e.message,
            error: e.message,
            progress: existing.progress,
          })
        }
        notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'failed' })
        if (focusedImageJobId.value === jobId) error.value = e.message
      }
    } finally {
      syncGeneratingFlags()
    }
  }

  function defaultImageJobApplyResult(result: any) {
    if (result.type === 'positioning_skeleton') {
      mergeNewResources(result.resources || [])
      const shotId = typeof result.shotId === 'number'
        ? result.shotId
        : imageGenJobs.value.find(j => j.id === result.id)?.shotId
      applyPositioningSkeletonToModal(shotId, result)
      return
    }
    if (result.type === 'positioning') {
      mergeNewResources(result.resources || [])
      const shotId = typeof result.shotId === 'number'
        ? result.shotId
        : imageGenJobs.value.find(j => j.id === result.id)?.shotId
      if (shotId) attachPositioningResultToShot(shotId, result.resources || [])
      return
    }
    if (result.type === 'motion_grid') {
      mergeNewResources(result.resources || [])
      const shotId = typeof result.shotId === 'number'
        ? result.shotId
        : imageGenJobs.value.find(j => j.id === result.id)?.shotId
      if (shotId) attachMotionGridResultToShot(shotId, result.resources || [])
      return
    }
    if (result.type === 'scene_grid' || result.type === 'scene_reverse' || result.type === 'scene_panorama') {
      mergeNewResources(result.resources || [])
      return
    }
    if (result.type === 'scene_reverse_skeleton') {
      mergeNewResources(result.resources || [])
      applySceneReverseSkeletonToModal(result)
      return
    }
    if (result.type === 'character') lastCharacterPrompt.value = result.prompt || ''
    else if (result.type === 'scene') {
      lastScenePrompt.value = result.prompt || ''
      if (isSceneGridOverheadJob(result)) {
        mergeNewResources(result.resources || [])
        applySceneGridOverheadFromJob(result)
        return
      }
    }
    else if (result.type === 'prop') lastPropPrompt.value = result.prompt || ''
  }

  async function syncImageGenerationJobs(projectId: number, opts?: { focusRunning?: boolean }) {
    const resumeToken = ++imageGenResumeToken
    try {
      const jobs = await api(`/projects/${projectId}/resources/generate-jobs`)
      if (!Array.isArray(jobs) || active.value?.id !== projectId || resumeToken !== imageGenResumeToken) return

      const seen = new Set<number>()
      for (const raw of jobs) {
        const rawId = Number(raw?.id)
        if (!rawId) continue
        if (isImageJobDismissed(rawId) || raw?.dismissed) {
          persistDismissedImageJob(rawId)
          continue
        }
        seen.add(rawId)
        const prev = imageGenJobs.value.find(j => j.id === raw.id)
        upsertImageJobFromApi(raw)
        const next = normalizeImageJob(raw)
        const becameCompleted = next.status === 'completed' && (!prev || prev.status !== 'completed')
        if (becameCompleted) {
          defaultImageJobApplyResult(next)
          mergeNewResources(next.resources || [])
          applySceneGridOverheadFromJob(next)
          commitGeneratedLibraryAsset(next)
        }
      }

      // Keep locally known jobs that disappeared from the recent list only if still busy.
      imageGenJobs.value = imageGenJobs.value.filter(j =>
        seen.has(j.id) || j.status === 'pending' || j.status === 'running',
      )
      syncGeneratingFlags()

      const running = imageGenJobs.value.filter(j => j.status === 'pending' || j.status === 'running')
      if (opts?.focusRunning && running.length) {
        focusedImageJobId.value = running[0].id
        if (!isSpecialImageJobType(running[0].type)) applyImageJobForm(running[0])
        showAddForm.value = false
      } else if (opts?.focusRunning) {
        const completed = imageGenJobs.value.find(j => j.status === 'completed' && (j.images?.length || 0) > 0)
        if (completed) {
          focusedImageJobId.value = completed.id
          if (!isSpecialImageJobType(completed.type)) applyImageJobResult(completed)
          showAddForm.value = false
        }
      }

      for (const job of running) {
        if (imageGenPolling.has(job.id)) continue
        void trackImageGenerationJob(
          projectId,
          job.id,
          defaultImageJobApplyResult,
          '未生成任何候选图，请检查设置中心的图像模型',
        )
      }
      // Defensive recovery: if overhead job already completed in panel,
      // ensure SceneGrid modal gets its sketch even when local request flow was interrupted.
      if (sceneGridModal.value) syncSceneGridOverheadFromJobs()
    } catch {
      /* ignore sync failures */
    }
  }

  async function resumeImageGenerationJobs(projectId: number) {
    await syncImageGenerationJobs(projectId, { focusRunning: true })
  }

  function notifyStudioSync(payload: Record<string, unknown>) {
    try {
      studioSyncChannel?.postMessage(payload)
    } catch { /* ignore */ }
  }

  async function refreshActiveEpisodeFromServer() {
    if (!active.value || !activeEpisode.value?.id) return
    await loadShotPage(shotPage.value, { force: true })
  }

  async function syncActiveStudioFromServer(reason: 'interval' | 'visibility' | 'broadcast' = 'interval') {
    if (!active.value || studioSyncInFlight) return
    const projectId = active.value.id
    const hasGeneratingShots = (activeEpisode.value?.shots || []).some(s => s.status === 'generating')
    const hasRunningImageJobs = imageGenJobs.value.some(
      j => j.projectId === projectId && (j.status === 'pending' || j.status === 'running'),
    )
    // Quiet interval: skip full episode replace while user is editing,
    // or when nothing is generating (shot/job polls already cover active work).
    if (reason === 'interval') {
      if (dirtyShotIds.value.size > 0) {
        await syncImageGenerationJobs(projectId, { focusRunning: false }).catch(() => {})
        return
      }
      if (!hasGeneratingShots && !hasRunningImageJobs) return
    }

    studioSyncInFlight = true
    try {
      await syncImageGenerationJobs(projectId, { focusRunning: reason !== 'interval' })
      if (reason !== 'interval' || hasGeneratingShots) {
        await refreshActiveEpisodeFromServer()
      }
      if (reason === 'visibility' || reason === 'broadcast') {
        await refreshProjectResources().catch(() => {})
      }
    } finally {
      studioSyncInFlight = false
    }
  }

  function startStudioSyncLoop() {
    stopStudioSyncLoop()
    studioSyncTimer = window.setInterval(() => {
      if (document.visibilityState !== 'visible') return
      if (!active.value) return
      void syncActiveStudioFromServer('interval')
    }, 15000)
  }

  function stopStudioSyncLoop() {
    if (studioSyncTimer != null) {
      window.clearInterval(studioSyncTimer)
      studioSyncTimer = null
    }
  }

  function onStudioVisibilityChange() {
    if (document.visibilityState !== 'visible' || !active.value) return
    void syncActiveStudioFromServer('visibility')
  }

  function onStudioSyncMessage(ev: MessageEvent) {
    const data = ev.data
    if (!data || typeof data !== 'object') return
    if (!active.value || data.projectId !== active.value.id) return
    void syncActiveStudioFromServer('broadcast')
  }

  function unwrapStoredGenPrompt(text: string) {
    const originalMarker = '【原定妆照要求，与本次修改冲突时以修改为准】'
    const revisionMarkers = [
      '【本次修改·必须执行，优先于下文原文】',
      '【本次修改，其余保持不变】',
    ]
    let s = (text || '').trim()
    for (let n = 0; n < 8 && s; n++) {
      let next = s
      const origIdx = next.lastIndexOf(originalMarker)
      if (origIdx >= 0) next = next.slice(origIdx + originalMarker.length).trim()
      let cut = next.length
      for (const m of revisionMarkers) {
        const i = next.indexOf(m)
        if (i >= 0 && i < cut) cut = i
      }
      if (cut < next.length) next = next.slice(0, cut).trim()
      if (next === s) break
      s = next
    }
    return s
  }
  function composeGenPromptKeepBase(base: string, revision: string) {
    const r = (revision || '').trim()
    if (r) return r
    return (base || '').trim()
  }
  function effectiveResourceGenPrompt() {
    const base = unwrapStoredGenPrompt(baseGenPrompt.value || resourceForm.value.description || '')
    return composeGenPromptKeepBase(base, promptRevision.value)
  }
  const isRegeneratingResource = computed(() => regenerateResourceId.value > 0)
  const isAddingDerivative = computed(() => !!libraryParentId.value && regenerateResourceId.value === 0)

  async function runImageGeneration(
    endpoint: string,
    setGenerating: (value: boolean) => void,
    applyResult: (result: any) => void,
    emptyMessage: string,
  ): Promise<number | null> {
    if (!active.value) return null
    const projectId = active.value.id
    const jobType = resourceForm.value.type as 'character' | 'scene' | 'prop'
    submittingImageGen.value = true
    setGenerating(true)
    error.value = ''
    // Keep the add dialog open so progress stays visible where the user clicked.
    // They can still dismiss with「后台生成」; the job list then tracks progress.
    showAddForm.value = true
    try {
      const genPrompt = effectiveResourceGenPrompt()
      const keepPrompt = regenerateResourceId.value > 0
      const revision = promptRevision.value.trim()
      const savedPrompt = unwrapStoredGenPrompt(baseGenPrompt.value)
      const started = await api(`/projects/${projectId}/resources/${endpoint}`, {
        method: 'POST',
        body: JSON.stringify({
          name: resourceForm.value.name,
          description: genPrompt || resourceForm.value.description,
          revision: revision || undefined,
          preservePrompt: keepPrompt || undefined,
          rawPrompt: keepPrompt && !revision ? true : undefined,
          savedPrompt: keepPrompt ? savedPrompt || undefined : undefined,
          count: candidateCount.value,
          resolution: imageResolution.value,
          quality: imageResolution.value,
          modelId: effectiveImageModelId.value || undefined,
          targetResourceId: regenerateResourceId.value
            || findExistingLibraryBase(resourceForm.value.name, resourceForm.value.type)?.id
            || undefined,
          parentId: libraryParentId.value || undefined,
          imageDataList: sceneReferences.value.filter(r => r.source === 'upload').map(r => r.imageData).filter(Boolean),
          resourceRefs: (() => {
            const refs = sceneReferences.value
              .filter(r => r.source === 'resource' && r.resourceId)
              .map(r => ({
                id: r.resourceId as number,
                variant: r.variant || '',
              }))
            const target = regenerateResourceId.value
              ? (libraryPageItems.value.find(r => r.id === regenerateResourceId.value)
                || active.value?.resources.find(r => r.id === regenerateResourceId.value))
              : findExistingLibraryBase(resourceForm.value.name, resourceForm.value.type)
            const parentId = target?.parentId || libraryParentId.value
            if (parentId && !refs.some(r => r.id === parentId)) {
              const parent = libraryResourceById(parentId)
              refs.unshift({
                id: parentId,
                variant: preferredIdentityVariant(parent),
              })
            }
            return refs
          })(),
        }),
      })
      const jobId = started.jobId as number
      // “本次修改”只属于刚提交的这一个任务，不留到下一次重生成。
      promptRevision.value = ''
      upsertImageJobFromApi({
        id: jobId,
        projectId,
        type: jobType,
        status: started.status || 'pending',
        progress: started.progress ?? 0,
        message: started.message || '任务已提交，等待开始…',
        doneCount: 0,
        totalCount: candidateCount.value,
        input: {
          name: resourceForm.value.name,
          description: genPrompt || resourceForm.value.description,
          count: candidateCount.value,
          quality: imageResolution.value,
          resolution: imageResolution.value,
          modelId: effectiveImageModelId.value || undefined,
          resourceRefs: snapshotSceneRefsForJob(),
          targetResourceId: regenerateResourceId.value || undefined,
        },
      })
      focusedImageJobId.value = jobId
      characterCandidates.value = []
      selectedCandidate.value = ''
      syncGeneratingFlags()
      submittingImageGen.value = false
      // Allow starting another job while this one polls in the background.
      setGenerating(false)
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'started' })
      void trackImageGenerationJob(projectId, jobId, applyResult, emptyMessage)
      return jobId
    } catch (e: any) {
      if (e?.message !== '生成任务已取消') error.value = e.message
      submittingImageGen.value = false
      setGenerating(false)
      syncGeneratingFlags()
      return null
    }
  }

  async function generateCharacterImages() {
    if (!active.value) return null
    if (!resourceForm.value.name.trim()) { error.value = '请先填写角色名称'; return null }
    if (!effectiveResourceGenPrompt() && !hasSceneReference.value) {
      error.value = '请填写角色描述，或上传/选择参考图进行图生图'
      return null
    }
    return await runImageGeneration(
      'generate-character',
      v => { generatingCharacter.value = v },
      result => { lastCharacterPrompt.value = result.prompt || '' },
      '未生成任何候选图，请检查设置中心的图像模型',
    )
  }
  async function generateSceneImages() {
    if (!active.value) return null
    if (!resourceForm.value.name.trim()) { error.value = '请先填写场景名称'; return null }
    if (!effectiveResourceGenPrompt() && !hasSceneReference.value) {
      error.value = '请填写场景描述，或上传/选择参考图进行图生图'
      return null
    }
    return await runImageGeneration(
      'generate-scene',
      v => { generatingScene.value = v },
      result => { lastScenePrompt.value = result.prompt || '' },
      '未生成任何候选图，请检查设置中心的图像模型',
    )
  }
  async function generatePropImages() {
    if (!active.value) return null
    if (!resourceForm.value.name.trim()) { error.value = '请先填写道具名称'; return null }
    if (!effectiveResourceGenPrompt() && !hasSceneReference.value) {
      error.value = '请填写道具描述，或上传/选择参考图进行图生图'
      return null
    }
    return await runImageGeneration(
      'generate-prop',
      v => { generatingProp.value = v },
      result => { lastPropPrompt.value = result.prompt || '' },
      '未生成任何候选图，请检查设置中心的图像模型',
    )
  }
  function resetResourceForm() {
    if (focusedImageJobId.value) dismissImageJob(focusedImageJobId.value)
    resourceForm.value = { type: resourceForm.value.type, name: '', description: '', imageData: '' }
    regenerateResourceId.value = 0
    baseGenPrompt.value = ''
    promptRevision.value = ''
    resourceImageFile.value = null
    sceneReferences.value = []
    characterCandidates.value = []
    selectedCandidate.value = ''
    videoFiles.value = []
    lastCharacterPrompt.value = ''
    lastScenePrompt.value = ''
    lastPropPrompt.value = ''
    showAddForm.value = false
  }
  function switchResourceType(type: 'character' | 'scene' | 'prop' | 'video') {
    resourceForm.value = { type, name: '', description: '', imageData: '' }
    regenerateResourceId.value = 0
    baseGenPrompt.value = ''
    promptRevision.value = ''
    resourceImageFile.value = null
    sceneReferences.value = []
    characterCandidates.value = []
    selectedCandidate.value = ''
    videoFiles.value = []
    lastCharacterPrompt.value = ''
    lastScenePrompt.value = ''
    lastPropPrompt.value = ''
  }
  function mergeNewResources(items: Resource[]) {
    if (!active.value || !items?.length) return
    for (const item of items) {
      if (!isGridCellItem(item) && parseCandidateName(item.name) && !item.isGroupPrimary) continue
      upsertResourceInCaches(item)
    }
  }
  /** Keep paged library grid + project cache in sync after edit / stylize. */
  function upsertResourceInCaches(updated: Resource) {
    const cacheBusted = (url: string, updatedAt?: string) => {
      if (!url || !updatedAt) return url
      const ts = Date.parse(updatedAt)
      if (!Number.isFinite(ts) || ts <= 0) return url
      // If already has v=..., don't double-append.
      if (/[?&]v=\d+/.test(url)) return url
      const sep = url.includes('?') ? '&' : '?'
      return `${url}${sep}v=${ts}`
    }

    const prev =
      active.value?.resources.find(r => r.id === updated.id)
      || libraryPageItems.value.find(r => r.id === updated.id)

    const updatedAt = (updated as any)?.updatedAt as string | undefined
    let next = updated
    if (prev && updatedAt) {
      if (prev.imageUrl === updated.imageUrl) {
        next = { ...next, imageUrl: cacheBusted(updated.imageUrl, updatedAt) }
      }
      if (prev.stylizedImageUrl === updated.stylizedImageUrl) {
        next = { ...next, stylizedImageUrl: cacheBusted(updated.stylizedImageUrl, updatedAt) }
      }
    }

    const existed = !!(
      active.value?.resources.some(r => r.id === next.id)
      || libraryPageItems.value.some(r => r.id === next.id)
    )
    if (active.value) {
      const list = active.value.resources
      const idx = list.findIndex(r => r.id === next.id)
      active.value = {
        ...active.value,
        resources: idx >= 0
          ? list.map(r => (r.id === next.id ? { ...r, ...next } : r))
          : [next, ...list],
      }
    }
    if (libraryPageItems.value.some(r => r.id === next.id)) {
      libraryPageItems.value = libraryPageItems.value.map(r =>
        r.id === next.id ? { ...r, ...next } : r,
      )
    } else if (libraryParentId.value && next.parentId === libraryParentId.value) {
      libraryPageItems.value = [next, ...libraryPageItems.value]
      libraryTotal.value += 1
    }
    if (next.parentId && !existed) {
      const bump = (r: Resource) => r.id === next.parentId
        ? { ...r, deriveCount: (r.deriveCount || 0) + 1 }
        : r
      if (active.value) {
        active.value = { ...active.value, resources: active.value.resources.map(bump) }
      }
      libraryPageItems.value = libraryPageItems.value.map(bump)
      if (libraryParent.value?.id === next.parentId) {
        libraryParent.value = bump(libraryParent.value)
      }
    }
    if (resourceTrash.value.some(r => r.id === next.id)) {
      resourceTrash.value = resourceTrash.value.map(r =>
        r.id === next.id ? { ...r, ...next } : r,
      )
    }
  }
  function selectDefaultCandidate() {
    const first = characterCandidates.value[0]
    if (first) selectedCandidate.value = first.url
  }
  function selectCandidate(url: string) {
    selectedCandidate.value = url
    resourceForm.value.imageData = ''
    resourceImageFile.value = null
  }
  function openImagePreview(url: string, label: string, selectUrl?: string) {
    imagePreview.value = { url, label, selectUrl }
  }
  function closeImagePreview() { imagePreview.value = null }
  function openPanoramaViewer(url: string, label: string, initialYawDeg = -90) {
    if (!url) return
    panoramaViewer.value = { url, label, initialYawDeg }
  }
  function closePanoramaViewer() { panoramaViewer.value = null }

  async function resolveDeskPanoramaUrl(url: string): Promise<string> {
    const raw = (url || '').trim()
    if (!raw) return ''
    if (raw.startsWith('data:') || raw.startsWith('blob:')) return raw
    const abs = raw.startsWith('/') ? `${window.location.origin}${raw}` : raw
    try {
      const u = new URL(abs)
      if (u.origin === window.location.origin) return abs
    } catch {
      return abs
    }
    // Cross-origin COS etc.: fetch → data URL so the desk iframe can texture-load it.
    try {
      const res = await fetch(abs, { mode: 'cors' })
      if (!res.ok) return abs
      const blob = await res.blob()
      return await new Promise<string>((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => resolve(String(reader.result || abs))
        reader.onerror = () => reject(reader.error)
        reader.readAsDataURL(blob)
      })
    } catch {
      return abs
    }
  }

  function findScenePanoramaForPositioning(): Resource | undefined {
    const refs = positioningRefs.value
    const relatedIds = new Set<number>()
    for (const ref of refs) {
      if (!ref.resourceId) continue
      const r = resourceById(ref.resourceId)
      if (!r) continue
      relatedIds.add(r.id)
      if (r.parentId) relatedIds.add(r.parentId)
      const root = sceneLibraryRootId(r.id)
      if (root) relatedIds.add(root)
    }
    const panos = (active.value?.resources || []).filter(r =>
      r.genType === 'scene_panorama' && !r.deletedAt && !!(r.imageUrl || r.stylizedImageUrl),
    )
    const scored = panos.map((p) => {
      let score = 0
      if (p.parentId && relatedIds.has(p.parentId)) score += 5
      if (relatedIds.has(p.id)) score += 4
      const plate = p.parentId ? resourceById(p.parentId) : undefined
      for (const id of relatedIds) {
        const r = resourceById(id)
        if (r && plate && sceneGridMatchesPlate(p.name, r.name)) score += 3
        if (r && sceneGridMatchesPlate(p.name, r.name)) score += 2
      }
      return { p, score }
    }).sort((a, b) => b.score - a.score || b.p.id - a.p.id)
    if (scored[0]?.score) return scored[0].p
    return panos.sort((a, b) => b.id - a.id)[0]
  }

  async function openDirectorDeskForPositioning(pano?: Resource | null) {
    const modal = positioningModal.value
    if (!modal) {
      error.value = '请先打开站位图弹窗'
      return
    }
    const target = pano?.genType === 'scene_panorama' ? pano : findScenePanoramaForPositioning()
    const url = target?.imageUrl || target?.stylizedImageUrl || ''
    if (!url) {
      error.value = '未找到场景全景图。请先为该场景生成 2:1 全景，再打开 3D 摆位。'
      ElMessage.warning('请先生成场景全景图')
      return
    }
    try {
      const panoramaUrl = await resolveDeskPanoramaUrl(url)
      directorDeskModal.value = {
        instanceId: `positioning-shot-${modal.shotId || 0}`,
        panoramaUrl,
        panoramaName: target?.name || '场景全景',
        panoramaResourceId: target?.id,
        purpose: 'positioning',
        shotId: modal.shotId,
      }
    } catch (e: any) {
      error.value = e?.message || '打开 3D 导演台失败'
    }
  }

  async function openDirectorDeskBrowse(pano: Resource) {
    const url = pano.imageUrl || pano.stylizedImageUrl || ''
    if (!url || pano.genType !== 'scene_panorama') {
      error.value = '请选择场景全景图'
      return
    }
    try {
      const panoramaUrl = await resolveDeskPanoramaUrl(url)
      directorDeskModal.value = {
        instanceId: `browse-pano-${pano.id}`,
        panoramaUrl,
        panoramaName: pano.name || '场景全景',
        panoramaResourceId: pano.id,
        purpose: 'browse',
      }
    } catch (e: any) {
      error.value = e?.message || '打开 3D 导演台失败'
    }
  }

  function closeDirectorDesk() {
    directorDeskModal.value = null
  }

  function onDirectorDeskReady(post: (type: string, payload?: Record<string, unknown>) => void) {
    const modal = directorDeskModal.value
    if (!modal) return
    post('storyai:director-desk-session', {
      instanceId: modal.instanceId,
      theme: 'dark',
    })
    post('storyai:director-desk-panorama', {
      imageUrl: modal.panoramaUrl,
      fileName: modal.panoramaName || '场景全景.png',
      edgeId: modal.panoramaResourceId ? `resource-${modal.panoramaResourceId}` : 'novaly-pano',
      sourceNodeId: modal.instanceId,
      projectionMode: 'equirectangular',
    })
  }

  function onDirectorDeskCaptures(captures: { dataUrl?: string; fileName?: string }[]) {
    const desk = directorDeskModal.value
    if (!desk || desk.purpose !== 'positioning') return
    const first = captures.find(c => typeof c.dataUrl === 'string' && c.dataUrl.startsWith('data:image'))
    if (!first?.dataUrl) {
      ElMessage.warning('未收到有效截图')
      return
    }
    const pos = positioningModal.value
    if (!pos || (desk.shotId && pos.shotId !== desk.shotId)) {
      ElMessage.warning('站位图弹窗已关闭，截图未写入')
      return
    }
    positioningModal.value = {
      ...pos,
      skeleton: { url: first.dataUrl },
      submitting: false,
      submittingStep: undefined,
    }
    ElMessage.success('已将导演台截图设为站位骨架，确认后可生成正式站位图')
    closeDirectorDesk()
  }

  function previewSelectCandidate() {
    if (imagePreview.value?.selectUrl) selectCandidate(imagePreview.value.selectUrl)
    closeImagePreview()
  }
  function afterCreateResource() {
    if (libraryParentId.value) {
      void loadLibraryPage()
    } else {
      openResourceLibrary()
    }
    resetResourceForm()
  }
  async function createResource() {
    if (!active.value) return
    if (!resourceForm.value.name.trim()) { error.value = '请填写资源名称'; return }
    const persisted = characterCandidates.value.find(c => c.url === selectedCandidate.value && c.resourceId)
    if (persisted?.resourceId && !resourceForm.value.imageData) {
      let item = active.value.resources.find(r => r.id === persisted.resourceId)
        || resourceTrash.value.find(r => r.id === persisted.resourceId)
        || imageGenJobs.value.flatMap(job => job.resources || []).find(r => r.id === persisted.resourceId)
      if (!item) {
        await refreshProjectResources()
        await loadResourceTrash()
        item = active.value.resources.find(r => r.id === persisted.resourceId)
          || resourceTrash.value.find(r => r.id === persisted.resourceId)
          || imageGenJobs.value.flatMap(job => job.resources || []).find(r => r.id === persisted.resourceId)
      }
      if (!item) { error.value = '候选资源不存在'; return }
      const existing = findExistingLibraryBase(resourceForm.value.name, resourceForm.value.type)
      const shouldMerge = (existing && existing.id !== item.id)
        || (parseCandidateName(item.name) && !item.isGroupPrimary)
      if (shouldMerge) {
        saving.value = true
        error.value = ''
        try {
          await usePrimaryResource(item)
          await refreshProjectResources()
          await loadResourceTrash()
          afterCreateResource()
        } catch (e: any) { error.value = e.message }
        finally { saving.value = false }
        return
      }
      await refreshProjectResources()
      afterCreateResource()
      return
    }
    if (!resourceForm.value.imageData && !selectedCandidate.value) {
      error.value = '请选择一张候选图，或手动上传图片'
      return
    }
    error.value = ''
    saving.value = true
    try {
      // In "regenerate" mode: manual upload should replace the existing target,
      // not create a brand-new resource row.
      if (
        regenerateResourceId.value > 0
        && !!resourceForm.value.imageData
        && !selectedCandidate.value
      ) {
        await api(`/resources/${regenerateResourceId.value}`, {
          method: 'PUT',
          body: JSON.stringify({
            name: resourceForm.value.name,
            description: resourceForm.value.description,
            remark: resourceForm.value.description,
            imageData: resourceForm.value.imageData,
          }),
        })
        await refreshProjectResources()
        await loadResourceTrash()
        afterCreateResource()
        return
      }
      if (!selectedCandidate.value && resourceImageFile.value && await cosEnabled()) {
        const file = resourceImageFile.value
        const type = resourceForm.value.type === 'video' ? 'other' : resourceForm.value.type
        const presign = await api(`/projects/${active.value.id}/resources/direct-upload`, {
          method: 'POST',
          body: JSON.stringify({
            type,
            name: resourceForm.value.name,
            description: resourceForm.value.description,
            parentId: libraryParentId.value || undefined,
            filename: file.name,
            contentType: file.type || 'image/jpeg',
          }),
        }) as CosPresign & { resourceId: number; ext: string }
        await putFileToCos(presign, file)
        await api(`/resources/${presign.resourceId}/confirm-image`, {
          method: 'POST',
          body: JSON.stringify({ ext: presign.ext, key: presign.key }),
        })
      } else {
        const payload: any = {
          type: resourceForm.value.type,
          name: resourceForm.value.name,
          description: resourceForm.value.description,
          parentId: libraryParentId.value || undefined,
        }
        if (selectedCandidate.value) payload.imageUrl = selectedCandidate.value
        else payload.imageData = resourceForm.value.imageData
        await api(`/projects/${active.value.id}/resources`, { method: 'POST', body: JSON.stringify(payload) })
      }
      await refreshProjectResources()
      await loadResourceTrash()
      afterCreateResource()
    } catch (e: any) { error.value = e.message } finally { saving.value = false }
  }
  async function stylizeResource(item: Resource, prompt?: string) {
    const next = new Set(stylizingResources.value)
    next.add(item.id)
    stylizingResources.value = next
    error.value = ''
    const jobId = ++stylizeJobSeq
    const displayName = resourceDisplayName(item)
    stylizeJobs.value = [
      {
        id: jobId,
        resourceId: item.id,
        name: displayName,
        status: 'running',
        message: `正在生成「${displayName}」非真人图…`,
      },
      ...stylizeJobs.value.filter(j => j.resourceId !== item.id || j.status === 'running'),
    ]
    try {
      const payload = {
        prompt: prompt !== undefined ? prompt : undefined,
        resolution: imageResolution.value,
        modelId: effectiveImageModelId.value || undefined,
      }
      const updated = await api(`/resources/${item.id}/stylize`, {
        method: 'POST',
        body: JSON.stringify(payload),
      })
      // Ensure browser reloads even if URL path is unchanged.
      if (updated.stylizedImageUrl && !String(updated.stylizedImageUrl).includes('?')) {
        updated.stylizedImageUrl = `${updated.stylizedImageUrl}?v=${Date.now()}`
      } else if (updated.stylizedImageUrl) {
        updated.stylizedImageUrl = `${String(updated.stylizedImageUrl).replace(/([?&])v=\d+/, `$1v=${Date.now()}`)}`
        if (!/[?&]v=/.test(updated.stylizedImageUrl)) {
          updated.stylizedImageUrl += `&v=${Date.now()}`
        }
      }
      upsertResourceInCaches(updated)
      stylizeJobs.value = stylizeJobs.value.map(j =>
        j.id === jobId
          ? { ...j, status: 'completed', message: `「${displayName}」非真人图已更新` }
          : j,
      )
    } catch (e: any) {
      error.value = e.message
      stylizeJobs.value = stylizeJobs.value.map(j =>
        j.id === jobId
          ? { ...j, status: 'failed', message: e.message || '非真人图生成失败', error: e.message }
          : j,
      )
    } finally {
      const done = new Set(stylizingResources.value)
      done.delete(item.id)
      stylizingResources.value = done
    }
  }
  function dismissStylizeJob(jobId: number, event?: Event) {
    event?.stopPropagation()
    stylizeJobs.value = stylizeJobs.value.filter(j => j.id !== jobId)
  }
  function focusStylizeJob(job: StylizeJobView) {
    studioTab.value = 'resources'
    if (job.status === 'completed' || job.status === 'failed') {
      dismissStylizeJob(job.id)
    }
  }
  function isStylizingResource(id: number) {
    return stylizingResources.value.has(id)
  }
  function defaultStylizePrompt(type: Resource['type']) {
    if (type === 'scene') return SCENE_STYLIZE_PROMPT
    if (type === 'other' || type === 'prop') return OTHER_STYLIZE_PROMPT
    return CHARACTER_STYLIZE_PROMPT
  }
  async function addPositioningRefFromFile(file: File, labelPrefix = '本地上传') {
    if (!file.type.startsWith('image/')) {
      error.value = '请选择图片文件'
      return false
    }
    const imageData = await readFileAsDataURL(file)
    const label = file.name
      ? `${labelPrefix} · ${file.name}`
      : `${labelPrefix} · 粘贴图片`
    const ref: SceneReference = {
      key: `upload:${file.name || 'paste'}:${Date.now()}:${Math.random()}`,
      source: 'upload',
      imageData,
      previewUrl: imageData,
      label,
    }
    const prev = refPickerTarget.value
    refPickerTarget.value = 'positioning'
    if (positioningReplaceIndex.value != null) {
      addToActiveRefList(ref)
    } else {
      if (positioningRefs.value.length >= maxPositioningRefs) {
        error.value = `最多添加 ${maxPositioningRefs} 张参考图`
        refPickerTarget.value = prev
        return false
      }
      addToActiveRefList(ref)
    }
    refPickerTarget.value = prev
    return true
  }
  function onPositioningRefFiles(e: Event) {
    const input = e.target as HTMLInputElement
    const files = input.files ? Array.from(input.files) : []
    input.value = ''
    if (!files.length) return
    void (async () => {
      let added = 0
      for (const file of files) {
        if (positioningRefs.value.length >= maxPositioningRefs) break
        if (await addPositioningRefFromFile(file)) added++
      }
      if (added < files.length) error.value = `最多添加 ${maxPositioningRefs} 张参考图`
    })()
  }
  async function onPositioningRefPaste(e: ClipboardEvent) {
    if (!positioningModal.value) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of items) {
      if (!item.type.startsWith('image/')) continue
      e.preventDefault()
      const file = item.getAsFile()
      if (!file) return
      await addPositioningRefFromFile(file, '粘贴')
      return
    }
  }

  function cleanRefAlias(label: string): string {
    const trimmed = (label || '').trim()
    if (!trimmed) return ''
    return trimmed
      .replace(/\s*·\s*(非真人|真人|原图|全彩|参考)\s*$/u, '')
      .replace(/\s*·\s*候选\s*\d+\s*$/u, '')
      .trim() || trimmed
  }

  function buildSceneRefsFromGenRefs(refs: Resource['genRefs'] | Shot['positioningRefs'] | Shot['motionGridRefs']): SceneReference[] {
    const seeded: SceneReference[] = []
    if (!refs?.length) return seeded
    for (const ref of refs) {
      if (!ref.id) {
        if (!ref.imageUrl) continue
        const key = `upload:job:${seeded.length}:${ref.label || ''}`
        if (seeded.some(r => r.previewUrl === ref.imageUrl)) continue
        seeded.push({
          key,
          source: 'upload',
          imageData: ref.imageUrl.startsWith('data:') ? ref.imageUrl : undefined,
          previewUrl: ref.imageUrl,
          label: cleanRefAlias(ref.label || '') || `上传参考 ${seeded.length + 1}`,
        })
        continue
      }
      const kind = (ref.kind || 'other') as ShotRef['kind']
      const variant = (ref.variant === 'original' || ref.variant === 'stylized')
        ? ref.variant
        : (kind === 'prop' ? undefined : 'stylized')
      const shotRef: ShotRef = kind === 'prop'
        ? { kind: 'prop', id: ref.id, label: cleanRefAlias(ref.label || '') || undefined }
        : {
            kind: kind === 'character' || kind === 'scene' || kind === 'other' ? kind : 'other',
            id: ref.id,
            variant: variant || 'stylized',
            label: cleanRefAlias(ref.label || '') || undefined,
          }
      const fromShot = sceneRefFromShotRef(shotRef)
      if (fromShot) {
        if (!seeded.some(r => r.key === fromShot.key)) seeded.push(fromShot)
        continue
      }
      if (!ref.imageUrl) continue
      const key = variant ? `resource:${ref.id}:${variant}` : `resource:${ref.id}`
      if (seeded.some(r => r.key === key)) continue
      seeded.push({
        key,
        source: 'resource',
        resourceId: ref.id,
        kind: shotRef.kind,
        variant,
        previewUrl: ref.imageUrl,
        label: cleanRefAlias(ref.label || '') || `资源 ${ref.id}`,
      })
    }
    return seeded
  }

  function seedPositioningRefsFromGenRefs(refs: Resource['genRefs'] | Shot['positioningRefs']) {
    positioningRefs.value = buildSceneRefsFromGenRefs(refs)
      .map(ref => ({ ...ref, label: positioningRefLabelOverrides.get(ref.key) || ref.label }))
      .slice(0, maxPositioningRefs)
    remapPositioningRefsToPreferredVariants()
    return positioningRefs.value.length > 0
  }

  function findPositioningResourceOnShot(shot: Shot): Resource | null {
    const resources = active.value?.resources || []
    const byId = new Map(resources.map(r => [r.id, r]))
    // Only resources already attached to THIS shot — never borrow another shot's 站位图.
    for (let i = shot.refs.length - 1; i >= 0; i--) {
      const ref = shot.refs[i]
      const r = byId.get(ref.id)
      if (!r) continue
      if (r.genType === 'positioning' || /站位/.test(r.name)) return r
    }
    return null
  }

  function isPositioningLikeResource(r: Resource): boolean {
    return r.genType === 'positioning' || r.genType === 'positioning_skeleton' || /站位|火柴人骨架/.test(r.name || '')
  }

  function findPositioningSkeletonOnShotId(shotId: number): { url: string; resourceId?: number } | undefined {
    if (!shotId) return undefined
    const resources = active.value?.resources || []
    const hits = resources
      .filter(r => r.genType === 'positioning_skeleton' && r.shotId === shotId && r.imageUrl)
      .sort((a, b) => b.id - a.id)
    const r = hits[0]
    if (!r?.imageUrl) return undefined
    return { url: r.imageUrl, resourceId: r.id }
  }

  function applyPositioningSkeletonToModal(shotId: number | undefined, result: any) {
    const img = result?.images?.[0]
    const res = (result?.resources || []).find((r: Resource) => r.imageUrl)
    const url = img?.url || res?.imageUrl
    const resourceId = img?.resourceId || res?.id
    if (!url) return
    const modal = positioningModal.value
    if (!modal || (shotId && modal.shotId !== shotId)) return
    positioningModal.value = {
      ...modal,
      submitting: false,
      submittingStep: undefined,
      skeleton: { url, resourceId },
    }
  }

  function isPositioningSpatialGuideRef(ref: SceneReference): boolean {
    if (/火柴人|骨架|站位图|站位示意/.test(ref.label || '')) return true
    if (ref.resourceId == null) return false
    const r = resourceForPositioningPick(ref.resourceId)
    return !!r && isPositioningLikeResource(r)
  }

  /** Drop previous 站位图 / extra scene angles so they don't fight the stick-figure pass. */
  function trimPositioningRefsForGenerate(refs: SceneReference[]): SceneReference[] {
    const spatial = refs.filter(r => !isPositioningSpatialGuideRef(r))
    const scenes = spatial.filter(r => r.kind === 'scene')
    const others = spatial.filter(r => r.kind !== 'scene')
    const preferredScene = scenes.find((r) => {
      const res = r.resourceId != null ? resourceForPositioningPick(r.resourceId) : undefined
      return !res || res.genType !== 'scene_grid_cell'
    }) || scenes[0]
    const out = preferredScene ? [preferredScene, ...others] : others
    return out.slice(0, maxPositioningRefs)
  }

  function isSceneGridCellRef(ref: SceneReference): boolean {
    if (ref.resourceId == null) return false
    const r = resourceForPositioningPick(ref.resourceId)
    return r?.genType === 'scene_grid_cell'
  }

  function isSceneGridCollageRef(ref: SceneReference): boolean {
    if (ref.resourceId == null) return false
    return resourceForPositioningPick(ref.resourceId)?.genType === 'scene_grid'
  }

  /** 1 scene 9-grid cell (matching 机位) or empty plate — spatial floor for stick-figure pass. */
  function pickPositioningSkeletonSceneRef(shot: Shot | undefined, prompt: string): SceneReference | null {
    const haystack = `${shot?.script || ''}\n${prompt || ''}`
    const wanted = inferWantedSceneAngles(haystack)
    const hung = positioningRefs.value.filter(r =>
      r.kind === 'scene' && !isPositioningSpatialGuideRef(r) && !isSceneGridCollageRef(r),
    )
    const cellAngle = (ref: SceneReference) => {
      if (ref.resourceId == null) return ''
      const r = resourceForPositioningPick(ref.resourceId)
      return r ? sceneGridAngleOf(r) : ''
    }
    for (const angle of wanted) {
      const hit = hung.find(r => isSceneGridCellRef(r) && cellAngle(r) === angle)
      if (hit) return hit
    }
    const hungCell = hung.find(isSceneGridCellRef)
    if (hungCell) return hungCell

    if (shot) {
      for (const ref of shot.refs || []) {
        if (ref.kind !== 'scene') continue
        const r = resourceForPositioningPick(ref.id)
        if (!r || r.genType !== 'scene_grid_cell' || !(r.imageUrl || r.stylizedImageUrl)) continue
        if (wanted.includes(sceneGridAngleOf(r))) {
          const next = sceneRefForPositioningFromLibrary(r, ref.label || undefined)
          if (next) return next
        }
      }
      const family = inferShotSceneFamily(shot, haystack)
      const cells = collectSceneGridCellsForFamily(family)
      for (const angle of wanted) {
        const cell = cells.find(c => sceneGridAngleOf(c) === angle)
        const next = cell ? sceneRefForPositioningFromLibrary(cell) : null
        if (next) return next
      }
      if (cells[0]) {
        const next = sceneRefForPositioningFromLibrary(cells[0])
        if (next) return next
      }
      for (const ref of shot.refs || []) {
        if (ref.kind !== 'scene') continue
        const r = resourceForPositioningPick(ref.id)
        if (!r || r.genType === 'scene_grid' || r.genType === 'scene_grid_cell') continue
        const plate = sceneRefForPositioningFromLibrary(r, ref.label || undefined)
        if (plate) return plate
      }
    }

    return hung.find(r => !isSceneGridCellRef(r)) || null
  }

  function positioningSkeletonSceneRef(): SceneReference | null {
    const modal = positioningModal.value
    if (!modal) return null
    const shot = activeEpisode.value?.shots.find(s => s.id === modal.shotId)
    return pickPositioningSkeletonSceneRef(shot, modal.prompt)
  }

  /** 站位图与写实场景对齐：角色/场景都优先真人原图，没有再退非真人。 */
  function preferredPositioningVariant(r: Resource): 'stylized' | 'original' | undefined {
    if (r.type === 'prop') return undefined
    if (r.imageUrl) return 'original'
    if (r.stylizedImageUrl) return 'stylized'
    return 'original'
  }

  function resourceForPositioningPick(id: number): Resource | undefined {
    return libraryResourceById(id) || resourceById(id)
  }

  /** Remap a shot/video ref to the variant 站位图 should use (真人优先). */
  function shotRefForPositioning(ref: ShotRef): ShotRef {
    if (ref.kind === 'prop') return ref
    const r = resourceForPositioningPick(ref.id)
    if (!r) return { ...ref, variant: ref.variant === 'stylized' ? 'original' : (ref.variant || 'original') }
    const variant = preferredPositioningVariant(r)
    if (!variant || variant === ref.variant) {
      // Still force original when both exist but saved draft said stylized
      if (ref.variant === 'stylized' && r.imageUrl) return { ...ref, variant: 'original' }
      return ref
    }
    return { ...ref, variant }
  }

  function sceneRefForPositioningFromLibrary(r: Resource, label?: string): SceneReference | null {
    if (isPositioningLikeResource(r)) return null
    const kind = (r.type === 'character' || r.type === 'scene' || r.type === 'prop' || r.type === 'other')
      ? r.type
      : null
    if (!kind) return null
    const alias = cleanRefAlias(label || resourceIdentityName(r) || r.name)
    if (kind === 'prop') {
      const preview = r.imageUrl || ''
      if (!preview) return null
      return {
        key: `resource:${r.id}`,
        source: 'resource',
        resourceId: r.id,
        kind: 'prop',
        previewUrl: preview,
        label: alias || r.name,
      }
    }
    // Build directly — do not go through effectiveShotRefVariant (it falls back to 非真人
    // when active.resources cache is missing imageUrl).
    const variant = preferredPositioningVariant(r) || 'original'
    const preview = variant === 'original'
      ? (r.imageUrl || r.stylizedImageUrl || '')
      : (r.stylizedImageUrl || r.imageUrl || '')
    if (!preview) return null
    const variantText = kind === 'scene'
      ? (variant === 'original' ? '原图' : '非真人')
      : (variant === 'original' ? '真人' : '非真人')
    return {
      key: `resource:${r.id}:${variant}`,
      source: 'resource',
      resourceId: r.id,
      kind,
      variant,
      previewUrl: preview,
      label: alias || `${resourceIdentityName(r) || r.name} · ${variantText}`,
    }
  }

  function sceneRefForPositioningFromShot(ref: ShotRef): SceneReference | null {
    const r = resourceForPositioningPick(ref.id)
    if (!r) return sceneRefFromShotRef(shotRefForPositioning(ref))
    return sceneRefForPositioningFromLibrary(r, ref.label || undefined)
  }

  /** Force every hung 站位参考 onto 真人/原图 when available (clears stale 非真人 drafts). */
  function remapPositioningRefsToPreferredVariants(): number {
    let changed = 0
    const next: SceneReference[] = []
    const used = new Set<string>()
    for (const ref of positioningRefs.value) {
      if (ref.source !== 'resource' || ref.resourceId == null || ref.kind === 'prop') {
        if (!used.has(ref.key)) {
          next.push(ref)
          used.add(ref.key)
        }
        continue
      }
      const r = resourceForPositioningPick(ref.resourceId)
      if (!r) {
        if (!used.has(ref.key)) {
          next.push(ref)
          used.add(ref.key)
        }
        continue
      }
      const want = preferredPositioningVariant(r)
      if (!want || want === ref.variant) {
        // Refresh preview in case resource URLs updated
        const refreshed = sceneRefForPositioningFromLibrary(r, ref.label)
        const item = refreshed || ref
        if (!used.has(item.key)) {
          if (item.key !== ref.key || item.previewUrl !== ref.previewUrl || item.variant !== ref.variant) changed++
          next.push({ ...item, label: ref.label || item.label })
          used.add(item.key)
        }
        continue
      }
      const remapped = sceneRefForPositioningFromLibrary(r, ref.label)
      if (!remapped) {
        if (!used.has(ref.key)) {
          next.push(ref)
          used.add(ref.key)
        }
        continue
      }
      changed++
      if (!used.has(remapped.key)) {
        next.push({ ...remapped, label: ref.label || remapped.label })
        used.add(remapped.key)
      }
    }
    positioningRefs.value = next.slice(0, maxPositioningRefs)
    return changed
  }

  function libraryResourceById(id: number): Resource | undefined {
    if (!id) return undefined
    const fromActive = resourceById(id)
    const fromParent = libraryParent.value?.id === id ? libraryParent.value : undefined
    const fromPage = libraryPageItems.value.find(r => r.id === id)
    const cands = [fromActive, fromParent, fromPage].filter(Boolean) as Resource[]
    return cands.find(r => !!r.imageUrl) || cands.find(r => !!r.stylizedImageUrl) || cands[0]
  }

  /** Resource img2img (含衍生图) 真人优先；视频分镜仍用 preferredShotRefVariant。 */
  function preferredIdentityVariant(r: Resource | null | undefined): 'stylized' | 'original' {
    if (!r) return 'original'
    if (r.type === 'prop') return 'original'
    if (r.imageUrl) return 'original'
    if (r.stylizedImageUrl) return 'stylized'
    return 'original'
  }

  function ensureDerivativeParentRef(parentId: number) {
    const parent = libraryResourceById(parentId)
    const variant = preferredIdentityVariant(parent)
    const kind: ShotRef['kind'] = (parent?.type === 'character' || parent?.type === 'scene' || parent?.type === 'prop')
      ? parent.type
      : 'character'
    const preview = variant === 'stylized'
      ? (parent?.stylizedImageUrl || parent?.imageUrl || '')
      : (parent?.imageUrl || parent?.stylizedImageUrl || '')
    const variantText = kind === 'scene'
      ? (variant === 'original' ? '原图' : '非真人')
      : (variant === 'original' ? '真人' : '非真人')
    const label = parent
      ? `${resourceDisplayName(parent)} · 底模 · ${variantText}`
      : `底模参考 · ${variantText}`
    const parentRef: SceneReference = {
      key: `resource:${parentId}:${variant}`,
      source: 'resource',
      resourceId: parentId,
      kind,
      variant,
      previewUrl: preview,
      label,
    }
    const rest = sceneReferences.value.filter(r => r.resourceId !== parentId)
    sceneReferences.value = [parentRef, ...rest].slice(0, maxSceneReferences)
  }

  /** Prefer 非真人 for characters; scenes stay on the original plate. */
  function preferredShotRefVariant(r: Resource): 'stylized' | 'original' | undefined {
    if (r.type === 'prop') return undefined
    if (r.type === 'character') {
      if (r.stylizedImageUrl) return 'stylized'
      if (r.imageUrl) return 'original'
      return 'stylized'
    }
    if (r.type === 'scene') {
      if (r.imageUrl) return 'original'
      if (r.stylizedImageUrl) return 'stylized'
      return 'original'
    }
    const recent = recentShotRefForResource(r.id)
    if (recent && recent.kind !== 'prop' && recent.variant) {
      if (recent.variant === 'stylized' && r.stylizedImageUrl) return 'stylized'
      if (recent.variant === 'original' && r.imageUrl) return 'original'
    }
    return preferredPositioningVariant(r)
  }

  function sceneRefFromLibraryResource(r: Resource, label?: string): SceneReference | null {
    if (isPositioningLikeResource(r)) return null
    const kind = (r.type === 'character' || r.type === 'scene' || r.type === 'prop' || r.type === 'other')
      ? r.type
      : null
    if (!kind) return null
    const alias = cleanRefAlias(label || resourceIdentityName(r) || r.name)
    const variant = preferredShotRefVariant(r)
    const shotRef: ShotRef = kind === 'prop'
      ? { kind: 'prop', id: r.id, label: alias || undefined }
      : { kind, id: r.id, variant: variant || 'stylized', label: alias || undefined }
    return sceneRefFromShotRef(shotRef)
  }

  function pickBestLibraryResource(candidates: Resource[]): Resource | null {
    const withImage = candidates.filter(r => !!(r.imageUrl || r.stylizedImageUrl))
    const pool = withImage.length ? withImage : candidates
    if (!pool.length) return null
    // Prefer recently used image for the same name group
    let bestRecent: Resource | null = null
    let bestRecentIdx = Number.POSITIVE_INFINITY
    for (const r of pool) {
      const idx = recentResourceIndex(r.id)
      if (idx >= 0 && idx < bestRecentIdx) {
        bestRecent = r
        bestRecentIdx = idx
      }
    }
    if (bestRecent) return bestRecent
    const primary = pool.find(r => !parseCandidateName(r.name) || r.isGroupPrimary)
    return primary || pool[0]
  }

  /** After analyze: merge shot.refs + library match (scored); replace stale scenes. */
  function autoPickPositioningRefs(shot: Shot, promptText: string): number {
    const { body } = splitPositioningPrompt(promptText || '')
    const haystack = `${shot.script || ''}\n${body || promptText || ''}`
    const mentions = extractShotMentionNames(haystack)
    const sceneHints = extractShotSceneHints(haystack)
    const libraryMatches = matchLibraryResourcesByText(haystack)

    const labelOf = (ref: SceneReference) => cleanRefAlias(ref.label || '')
    const scoreOf = (ref: SceneReference) =>
      scoreResourceNameAgainstText(labelOf(ref), haystack, mentions, sceneHints)

    // Keep current picks unless a scene/character is stale vs better library match
    const next = positioningRefs.value.filter((ref) => {
      const score = scoreOf(ref)
      if (score >= 300) return true
      if (!ref.kind) return true
      const rival = libraryMatches.find(m => m.type === ref.kind && m.score >= 400 && m.score > score + 100)
      if (rival) return false
      if (ref.kind === 'scene' && score < 250 && libraryMatches.some(m => m.type === 'scene' && m.score >= 400)) {
        return false
      }
      return true
    })
    const usedKeys = new Set(next.map(r => r.key))
    const usedResourceIds = new Set(
      next.filter(r => typeof r.resourceId === 'number').map(r => r.resourceId as number),
    )

    const pushRef = (ref: SceneReference | null): boolean => {
      if (!ref) return false
      if (next.length >= maxPositioningRefs) return false
      if (usedKeys.has(ref.key)) return false
      if (ref.resourceId != null && usedResourceIds.has(ref.resourceId)) return false
      if (ref.kind && ref.label && next.some(r => r.kind === ref.kind && namesOverlapEntity(r.label || '', ref.label || ''))) {
        return false
      }
      next.push(ref)
      usedKeys.add(ref.key)
      if (ref.resourceId != null) usedResourceIds.add(ref.resourceId)
      return true
    }

    // 1) Shot video refs — skip stale scenes that lose to a better library scene
    for (const ref of shot.refs || []) {
      const r = resourceById(ref.id)
      if (r && isPositioningLikeResource(r)) continue
      const labeled = withDefaultShotRefLabel(ref)
      const label = cleanRefAlias(labeled.label || '') || cleanRefAlias(r?.name || '')
      const score = scoreResourceNameAgainstText(label, haystack, mentions, sceneHints)
      if (
        labeled.kind === 'scene'
        && score < 250
        && libraryMatches.some(m => m.type === 'scene' && m.score >= 400)
      ) {
        continue
      }
      pushRef(sceneRefForPositioningFromShot(labeled))
      if (next.length >= maxPositioningRefs) break
    }

    // 2) Recently used refs that still match the text
    for (const ref of recentShotRefsMatchingText(haystack, mentions, sceneHints)) {
      pushRef(sceneRefForPositioningFromShot(ref))
      if (next.length >= maxPositioningRefs) break
    }

    // 3) Same scored library matching as video「优化文案」.
    for (const item of libraryMatches) {
      pushRef(sceneRefForPositioningFromLibrary(item.resource, item.base))
      if (next.length >= maxPositioningRefs) break
    }

    positioningRefs.value = next.slice(0, maxPositioningRefs)
    remapPositioningRefsToPreferredVariants()
    return positioningRefs.value.length
  }

  /** LLM disambiguation for positioning refs; falls back to local scoring. */
  async function autoPickPositioningRefsWithAI(shot: Shot, promptText: string): Promise<number> {
    const { body } = splitPositioningPrompt(promptText || '')
    const haystack = `${shot.script || ''}\n${body || promptText || ''}`
    if (!haystack.trim()) return autoPickPositioningRefs(shot, promptText)

    const poolMap = new Map<number, LibraryNameMatch>()
    for (const c of collectLibraryMatchCandidates(haystack)) {
      poolMap.set(c.resource.id, c)
    }
    for (const ref of shot.refs || []) {
      const r = resourceById(ref.id)
      if (!r || isPositioningLikeResource(r) || poolMap.has(r.id)) continue
      const base = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
      if (base.length < 2) continue
      poolMap.set(r.id, { resource: r, base, type: r.type, score: 60 })
    }
    for (const ref of positioningRefs.value) {
      if (ref.resourceId == null || poolMap.has(ref.resourceId)) continue
      const r = resourceById(ref.resourceId)
      if (!r || isPositioningLikeResource(r)) continue
      const base = cleanRefAlias(ref.label || '') || cleanRefAlias(resourceEditableName(r) || r.name)
      if (base.length < 2) continue
      poolMap.set(r.id, { resource: r, base, type: r.type, score: 60 })
    }
    const pool = [...poolMap.values()].slice(0, 40)
    if (!pool.length) return autoPickPositioningRefs(shot, promptText)

    try {
      const result = await api(`/shots/${shot.id}/match-refs`, {
        method: 'POST',
        body: JSON.stringify({
          script: haystack.length > 1800 ? `${haystack.slice(0, 1800)}…` : haystack,
          candidates: pool.map(c => ({ id: c.resource.id, type: c.type, name: c.base })),
        }),
      })
      const picks = Array.isArray(result?.refs) ? result.refs as { id?: number; label?: string }[] : []
      if (!picks.length) return autoPickPositioningRefs(shot, promptText)

      const mentions = extractShotMentionNames(haystack)
      const sceneHints = extractShotSceneHints(haystack)
      const next: SceneReference[] = []
      const usedKeys = new Set<string>()
      const usedResourceIds = new Set<number>()

      const pushRef = (ref: SceneReference | null): boolean => {
        if (!ref) return false
        if (next.length >= maxPositioningRefs) return false
        if (usedKeys.has(ref.key)) return false
        if (ref.resourceId != null && usedResourceIds.has(ref.resourceId)) return false
        if (ref.kind && ref.label && next.some(r => r.kind === ref.kind && namesOverlapEntity(r.label || '', ref.label || ''))) {
          return false
        }
        next.push(ref)
        usedKeys.add(ref.key)
        if (ref.resourceId != null) usedResourceIds.add(ref.resourceId)
        return true
      }

      // Keep manual uploads
      for (const ref of positioningRefs.value) {
        if (ref.source === 'upload') pushRef(ref)
      }

      for (const pick of picks) {
        const id = Number(pick.id)
        if (!Number.isFinite(id)) continue
        const item = pool.find(c => c.resource.id === id)
        const raw = item?.resource || resourceById(id)
        if (!raw || isPositioningLikeResource(raw)) continue
        const r = preferRecentResourceForName(raw)
        const base = cleanRefAlias(pick.label || '') || item?.base || cleanRefAlias(resourceEditableName(r) || r.name)
        const core = normalizeEntityName(base)
        const stemHit = mentions.some(m => {
          const nm = normalizeEntityName(m)
          return nm.length >= 2 && core.startsWith(nm) && COMPOUND_PLACE_SUFFIX_RE.test(core.slice(nm.length))
        })
        const fullInText = haystack.includes(base) || haystack.includes(core)
          || sceneHints.some(h => normalizeEntityName(h) === core)
        if (stemHit && !fullInText && r.type === 'scene') continue
        pushRef(sceneRefForPositioningFromLibrary(r, base))
        if (next.length >= maxPositioningRefs) break
      }

      for (const item of matchLibraryResourcesByText(haystack)) {
        if (next.length >= maxPositioningRefs) break
        if (item.score < 850) continue
        pushRef(sceneRefForPositioningFromLibrary(item.resource, item.base))
      }

      for (const ref of recentShotRefsMatchingText(haystack, mentions, sceneHints)) {
        if (next.length >= maxPositioningRefs) break
        pushRef(sceneRefForPositioningFromShot(ref))
      }

      positioningRefs.value = next.slice(0, maxPositioningRefs)
      remapPositioningRefsToPreferredVariants()
      return positioningRefs.value.length
    } catch {
      return autoPickPositioningRefs(shot, promptText)
    }
  }

  function positioningRefsPayload(): ResourceGenRef[] {
    return positioningRefs.value
      .filter(r => r.source === 'resource' && r.resourceId)
      .map(r => ({
        id: r.resourceId!,
        variant: r.variant || (r.kind === 'character' || r.kind === 'scene' ? 'original' : ''),
        kind: r.kind || '',
        label: r.label || '',
      }))
  }

  async function persistPositioningDraft(shot: Shot, prompt: string) {
    shot.positioningPrompt = prompt.trim()
    shot.positioningRefs = positioningRefsPayload()
    try {
      await saveShot(shot)
    } catch (e: any) {
      error.value = e.message
    }
  }

  async function analyzePositioningPrompt(shot: Shot) {
    if (!positioningModal.value || positioningModal.value.shotId !== shot.id) return
    positioningModal.value = { ...positioningModal.value, analyzing: true }
    error.value = ''
    try {
      await refreshProjectResourcesForPick().catch(() => {})
      const result = await api(`/shots/${shot.id}/analyze-positioning`, {
        method: 'POST',
        body: JSON.stringify({
          refLabels: positioningRefs.value.map((r, i) => `图${i + 1}：${r.label}`),
        }),
      })
      if (!positioningModal.value || positioningModal.value.shotId !== shot.id) return
      const raw = String(result.prompt || '')
      const { body } = splitPositioningPrompt(raw)
      await autoPickPositioningRefsWithAI(shot, body || raw)
      remapPositioningRefsToPreferredVariants()
      const prompt = joinPositioningPrompt(body || raw, positioningRefs.value)
      positioningModal.value = {
        ...positioningModal.value,
        prompt,
        analyzing: false,
      }
      await persistPositioningDraft(shot, prompt)
    } catch (e: any) {
      if (positioningModal.value?.shotId === shot.id) {
        positioningModal.value = { ...positioningModal.value, analyzing: false }
      }
      error.value = e.message
    }
  }

  async function openPositioningModal(shot: Shot) {
    if (!shot.script.trim()) {
      error.value = '请先填写当前分镜文案'
      return
    }
    void loadProviders()
    expandShot(shot.id)

    // Per-shot only: draft on this shot, or a 站位图 already linked on this shot.
    // Never reuse another shot's prompt/refs; never auto-fill from video refs.
    const savedPrompt = (shot.positioningPrompt || '').trim()
    const savedRefs = shot.positioningRefs || []
    const ownRes = findPositioningResourceOnShot(shot)

    positioningReplaceIndex.value = null
    positioningRefs.value = []
    let prompt = ''
    if (savedPrompt || savedRefs.length) {
      prompt = savedPrompt
      if (savedRefs.length) seedPositioningRefsFromGenRefs(savedRefs)
    } else if (ownRes) {
      prompt = (ownRes.genPrompt || ownRes.description || '').trim()
      if (ownRes.genRefs?.length) seedPositioningRefsFromGenRefs(ownRes.genRefs)
    }
    remapPositioningRefsToPreferredVariants()
    if (prompt) {
      const { body } = splitPositioningPrompt(prompt)
      prompt = joinPositioningPrompt(body || prompt, positioningRefs.value)
    }

    positioningModal.value = {
      shotId: shot.id,
      shotLabel: shot.label || `分镜 ${shot.sortOrder}`,
      prompt,
      initializing: true,
      analyzing: false,
      submitting: false,
      skeleton: findPositioningSkeletonOnShotId(shot.id),
      results: undefined,
    }

    // Opening the modal must never wait for a network save or for the paginated
    // resource library (large projects can require dozens of requests). Hydrate
    // in the background and only fill empty fields so user edits are preserved.
    void (async () => {
      await Promise.all([
        saveShot(shot).catch((e: any) => { error.value = e.message }),
        refreshProjectResourcesForPick().catch(() => {}),
      ])
      const modal = positioningModal.value
      if (!modal || modal.shotId !== shot.id) return

      if (!modal.prompt.trim() && positioningRefs.value.length === 0) {
        const refreshedOwnRes = findPositioningResourceOnShot(shot)
        if (refreshedOwnRes) {
          let refreshedPrompt = (refreshedOwnRes.genPrompt || refreshedOwnRes.description || '').trim()
          if (refreshedOwnRes.genRefs?.length) seedPositioningRefsFromGenRefs(refreshedOwnRes.genRefs)
          remapPositioningRefsToPreferredVariants()
          if (refreshedPrompt) {
            const { body } = splitPositioningPrompt(refreshedPrompt)
            refreshedPrompt = joinPositioningPrompt(body || refreshedPrompt, positioningRefs.value)
          }
          positioningModal.value = { ...modal, prompt: refreshedPrompt, initializing: false }
          return
        }
      }
      remapPositioningRefsToPreferredVariants()
      positioningModal.value = { ...modal, initializing: false }
    })()
  }

  function setPositioningPromptBody(body: string) {
    const modal = positioningModal.value
    if (!modal) return
    positioningModal.value = {
      ...modal,
      prompt: joinPositioningPrompt(body, positioningRefs.value),
    }
  }

  function positioningPromptBody(): string {
    if (!positioningModal.value) return ''
    return splitPositioningPrompt(positioningModal.value.prompt).body
  }

  async function closePositioningModal() {
    if (positioningModal.value?.submitting) return
    const modal = positioningModal.value
    const shot = modal ? activeEpisode.value?.shots.find(s => s.id === modal.shotId) : null
    if (shot && modal && (modal.prompt.trim() || positioningRefs.value.length)) {
      await persistPositioningDraft(shot, modal.prompt)
    }
    positioningModal.value = null
    positioningReplaceIndex.value = null
    if (refPickerTarget.value === 'positioning') {
      sceneRefPickerOpen.value = false
      refPickerTarget.value = 'resource'
		positioningPickingSkeleton.value = false
    }
  }

  async function reanalyzePositioningPrompt() {
    const modal = positioningModal.value
    if (!modal) return
    const shot = activeEpisode.value?.shots.find(s => s.id === modal.shotId)
    if (!shot) {
      error.value = '分镜不存在'
      return
    }
    await saveShot(shot)
    await analyzePositioningPrompt(shot)
  }

  function attachPositioningResultToShot(shotId: number, resources: Resource[]) {
    const shot = activeEpisode.value?.shots.find(s => s.id === shotId)
    if (!shot || !resources.length) return
    const primary = resources.find(r => r.isGroupPrimary) || resources[0]
    if (!primary?.id) return
    addShotRef(shot, { kind: 'scene', id: primary.id, variant: 'original', label: '站位图' })
    trimCrowdCharacterRefs(shot, shot.script)
    saveShot(shot)
  }

  function skeletonSceneRef(skeleton: { url: string; resourceId?: number }): SceneReference {
    if (skeleton.resourceId) {
      return {
        key: `resource:${skeleton.resourceId}`,
        source: 'resource',
        resourceId: skeleton.resourceId,
        kind: 'other',
        variant: 'original',
        previewUrl: skeleton.url,
        label: '火柴人站位骨架',
      }
    }
    return {
      key: 'upload:positioning-skeleton',
      source: 'upload',
      imageData: skeleton.url,
      kind: 'other',
      previewUrl: skeleton.url,
      label: '火柴人站位骨架',
    }
  }

  async function confirmPositioningSkeleton() {
    const modal = positioningModal.value
    if (!modal) return
    const prompt = (splitPositioningPrompt(modal.prompt).body || modal.prompt).trim()
    if (!prompt) {
      error.value = '请填写站位图提示词'
      return
    }
    const projectId = active.value?.id
    if (!projectId) return
    positioningModal.value = { ...modal, submitting: true, submittingStep: 'skeleton' }
    error.value = ''
    try {
      const shot = activeEpisode.value?.shots.find(s => s.id === modal.shotId)
      if (shot) await persistPositioningDraft(shot, modal.prompt)
      const sceneRef = pickPositioningSkeletonSceneRef(shot, prompt)
      const resourceRefs = sceneRef?.source === 'resource' && sceneRef.resourceId
        ? [{
            id: sceneRef.resourceId,
            variant: sceneRef.variant || 'original',
          }]
        : []
      const started = await api(`/shots/${modal.shotId}/generate-positioning`, {
        method: 'POST',
        body: JSON.stringify({
          name: modal.shotLabel,
          prompt,
          stage: 'skeleton',
          count: 1,
          resolution: '1k',
          quality: '1k',
          modelId: effectiveImageModelId.value || undefined,
          resourceRefs,
        }),
      })
      const jobId = started.jobId as number
      const shotId = modal.shotId
      upsertImageJobFromApi({
        id: jobId,
        projectId,
        type: 'positioning_skeleton',
        status: started.status || 'pending',
        progress: started.progress ?? 0,
        message: started.message || '正在生成火柴人站位骨架…',
        doneCount: 0,
        totalCount: 1,
        input: { name: `${modal.shotLabel} · 火柴人骨架`, description: prompt, count: 1, shotId },
      })
      focusedImageJobId.value = jobId
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'started' })
      await trackImageGenerationJob(
        projectId,
        jobId,
        (result) => {
          mergeNewResources(result.resources || [])
          applyPositioningSkeletonToModal(shotId, result)
        },
        '未生成火柴人骨架',
      )
    } catch (e: any) {
      error.value = e.message
    } finally {
      if (positioningModal.value?.shotId === modal.shotId) {
        positioningModal.value = {
          ...positioningModal.value,
          submitting: false,
          submittingStep: undefined,
        }
      }
    }
  }

  async function confirmPositioningGenerate() {
    const modal = positioningModal.value
    if (!modal) return
    if (modal.submitting) return
    if (!modal.skeleton?.url) {
      error.value = '请先生成火柴人骨架，确认站位无误后再生成正式站位图'
      return
    }
    const projectId = active.value?.id
    if (!projectId) return
    // Refreshing and remapping references can take several seconds. Enter the
    // loading state before that work so the first click always gets feedback.
    positioningModal.value = { ...modal, submitting: true, submittingStep: 'final' }
    error.value = ''
    try {
      await refreshProjectResourcesForPick().catch(() => {})
      remapPositioningRefsToPreferredVariants()
      const baseRefs = trimPositioningRefsForGenerate(positioningRefs.value)
      const { body } = splitPositioningPrompt(modal.prompt)
      const positionedPeople = Array.from((body || modal.prompt).matchAll(/\((?:左前|中前|右前|左中|中中|右中|左后|中后|右后)\)/g)).length
      const allGenRefs = [skeletonSceneRef(modal.skeleton), ...baseRefs.filter(r => r.resourceId !== modal.skeleton?.resourceId)]
        .slice(0, maxPositioningRefs)
      // Three or more people: character sheets commonly compete with the map and
      // make the model output only a subset. Faces are mosaicked anyway, so send
      // the approved skeleton plus one scene plate and keep clothing in text.
      const genRefs = positionedPeople >= 3
        ? [allGenRefs[0], allGenRefs.find((r, i) => i > 0 && r.kind === 'scene')].filter(Boolean) as SceneReference[]
        : allGenRefs
      const draftPrompt = joinPositioningPrompt(body || modal.prompt.trim(), positioningRefs.value).trim()
      const prompt = joinPositioningPrompt(body || modal.prompt.trim(), genRefs).trim()
      if (!prompt) throw new Error('请填写站位图提示词')
      if (positioningModal.value?.shotId === modal.shotId) {
        positioningModal.value = { ...positioningModal.value, prompt: draftPrompt }
      }
      const shot = activeEpisode.value?.shots.find(s => s.id === modal.shotId)
      if (shot) await persistPositioningDraft(shot, draftPrompt)
      const started = await api(`/shots/${modal.shotId}/generate-positioning`, {
        method: 'POST',
        body: JSON.stringify({
          name: modal.shotLabel,
          prompt,
          stage: 'final',
          count: 1,
          resolution: imageResolution.value,
          quality: imageResolution.value,
          modelId: effectiveImageModelId.value || undefined,
          imageDataList: genRefs.filter(r => r.source === 'upload').map(r => r.imageData).filter(Boolean),
          resourceRefs: genRefs
            .filter(r => r.source === 'resource' && r.resourceId)
            .map(r => ({
              id: r.resourceId,
              variant: r.variant || (r.kind === 'character' || r.kind === 'scene' ? 'original' : ''),
            })),
        }),
      })
      const jobId = started.jobId as number
      const shotId = modal.shotId
      const jobRefs = genRefs
        .filter(r => r.source === 'resource' && r.resourceId)
        .map(r => ({
          id: r.resourceId!,
          variant: r.variant || (r.kind === 'character' || r.kind === 'scene' ? 'original' : ''),
          kind: r.kind || '',
          label: r.label || '',
        }))
      positioningModal.value = null
      refPickerTarget.value = 'resource'
      upsertImageJobFromApi({
        id: jobId,
        projectId,
        type: 'positioning',
        status: started.status || 'pending',
        progress: started.progress ?? 0,
        message: started.message || '按已确认的骨架生成正式站位图…',
        doneCount: 0,
        totalCount: 1,
        input: {
          name: modal.shotLabel,
          description: prompt,
          count: 1,
          shotId,
          resourceRefs: (() => {
            const mapped = jobRefs.map(r => ({
              id: r.id,
              variant: r.variant || '',
              kind: r.kind || '',
              label: r.label || '',
              imageUrl: genRefs.find(p => p.resourceId === r.id)?.previewUrl || '',
            }))
            for (const p of genRefs.filter(r => r.source === 'upload')) {
              mapped.push({
                id: 0,
                variant: 'original',
                kind: 'other',
                label: p.label || '',
                imageUrl: p.previewUrl || '',
              })
            }
            return mapped
          })(),
        },
      })
      focusedImageJobId.value = jobId
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'started' })
      void trackImageGenerationJob(
        projectId,
        jobId,
        (result) => {
          mergeNewResources(result.resources || [])
          attachPositioningResultToShot(shotId, result.resources || [])
        },
        '未生成站位图',
      )
    } catch (e: any) {
      if (positioningModal.value) {
        positioningModal.value = { ...positioningModal.value, submitting: false, submittingStep: undefined }
      }
      error.value = e.message
    }
  }

  // ---------- 9帧图（连续画面网格） ----------

  function syncMotionGridPromptLegend() {
    const modal = motionGridModal.value
    if (!modal) return
    const { body } = splitPositioningPrompt(modal.prompt || '')
    motionGridModal.value = {
      ...modal,
      prompt: joinPositioningPrompt(body, motionGridRefs.value),
    }
  }

  function renameMotionGridRef(index: number, label: string) {
    if (index < 0 || index >= motionGridRefs.value.length) return
    const trimmed = label.trim()
    const current = motionGridRefs.value[index]
    const nextLabel = trimmed || current.label
    if (current.label === nextLabel) return
    const next = [...motionGridRefs.value]
    next[index] = { ...current, label: nextLabel }
    motionGridRefs.value = next
    syncMotionGridPromptLegend()
  }

  function updateMotionGridRefLabel(index: number, label: string) {
    if (index < 0 || index >= motionGridRefs.value.length) return
    const current = motionGridRefs.value[index]
    if (current.label === label) return
    const next = [...motionGridRefs.value]
    next[index] = { ...current, label }
    motionGridRefs.value = next
    syncMotionGridPromptLegend()
  }

  function removeMotionGridReference(key: string) {
    motionGridRefs.value = motionGridRefs.value.filter(r => r.key !== key)
    if (motionGridReplaceIndex.value != null && motionGridReplaceIndex.value >= motionGridRefs.value.length) {
      motionGridReplaceIndex.value = null
    }
    if (motionGridAnchor.value?.key === key) motionGridAnchor.value = null
    syncMotionGridPromptLegend()
  }

  function clearMotionGridReferences() {
    motionGridRefs.value = []
    motionGridReplaceIndex.value = null
    motionGridAnchor.value = null
    syncMotionGridPromptLegend()
  }

  function openMotionGridRefPicker() {
    motionGridReplaceIndex.value = null
    refPickerTarget.value = 'motionGrid'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  function openMotionGridReplacePicker(index: number) {
    if (index < 0 || index >= motionGridRefs.value.length) return
    motionGridReplaceIndex.value = index
    refPickerTarget.value = 'motionGrid'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }

  async function addMotionGridRefFromFile(file: File, labelPrefix = '上传') {
    if (!file.type.startsWith('image/')) {
      error.value = '请选择图片文件'
      return false
    }
    const imageData = await readFileAsDataURL(file)
    const label = file.name
      ? `${labelPrefix} · ${file.name}`
      : `${labelPrefix} · 粘贴图片`
    const ref: SceneReference = {
      key: `upload:${file.name || 'paste'}:${Date.now()}:${Math.random()}`,
      source: 'upload',
      imageData,
      previewUrl: imageData,
      label,
    }
    const prev = refPickerTarget.value
    refPickerTarget.value = 'motionGrid'
    if (motionGridReplaceIndex.value != null) {
      addToActiveRefList(ref)
    } else {
      if (motionGridRefs.value.length >= maxMotionGridRefs) {
        error.value = `最多添加 ${maxMotionGridRefs} 张参考图`
        refPickerTarget.value = prev
        return false
      }
      addToActiveRefList(ref)
    }
    refPickerTarget.value = prev
    return true
  }
  function onMotionGridRefFiles(e: Event) {
    const input = e.target as HTMLInputElement
    const files = input.files ? Array.from(input.files) : []
    input.value = ''
    if (!files.length) return
    void (async () => {
      let added = 0
      for (const file of files) {
        if (motionGridRefs.value.length >= maxMotionGridRefs) break
        if (await addMotionGridRefFromFile(file)) added++
      }
      if (added < files.length) error.value = `最多添加 ${maxMotionGridRefs} 张参考图`
    })()
  }
  async function onMotionGridRefPaste(e: ClipboardEvent) {
    if (!motionGridModal.value) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of items) {
      if (!item.type.startsWith('image/')) continue
      e.preventDefault()
      const file = item.getAsFile()
      if (!file) return
      await addMotionGridRefFromFile(file, '粘贴')
      return
    }
  }

  function findPrevShot(shot: Shot): Shot | null {
    const priors = (activeEpisode.value?.shots || []).filter(s => s.sortOrder < shot.sortOrder)
    if (!priors.length) return null
    return priors.sort((a, b) => b.sortOrder - a.sortOrder || b.id - a.id)[0]
  }

  /** Best-effort client-side lookup of the previous shot's outro frame (帧9). */
  function findPrevShotOutroCell(shot: Shot): Resource | null {
    const prev = findPrevShot(shot)
    if (!prev) return null
    const cells = (active.value?.resources || []).filter(r =>
      r.shotId === prev.id && r.genType === 'motion_grid_cell' && r.gridCell === 9 && r.imageUrl,
    )
    if (!cells.length) return null
    return cells.sort((a, b) => (b.gridId || 0) - (a.gridId || 0) || b.id - a.id)[0]
  }

  function findMotionGridResourceOnShot(shot: Shot): Resource | null {
    const resources = active.value?.resources || []
    const byId = new Map(resources.map(r => [r.id, r]))
    for (let i = shot.refs.length - 1; i >= 0; i--) {
      const r = byId.get(shot.refs[i].id)
      if (r && r.genType === 'motion_grid') return r
    }
    return null
  }

  function ensureMotionGridAnchorFirst() {
    const anchor = motionGridAnchor.value
    if (!anchor) return
    motionGridRefs.value = [anchor, ...motionGridRefs.value.filter(r => r.key !== anchor.key)]
  }

  /**
   * Reuse the positioning auto-pick machinery on the motion-grid list by temporarily
   * swapping the refs channel; the anchor frame is excluded and re-prepended after.
   */
  async function autoPickMotionGridRefsWithAI(shot: Shot, promptText: string): Promise<number> {
    const anchor = motionGridAnchor.value
    const savedPositioningRefs = positioningRefs.value
    positioningRefs.value = motionGridRefs.value.filter(r => r.key !== anchor?.key)
    try {
      await autoPickPositioningRefsWithAI(shot, promptText)
      motionGridRefs.value = positioningRefs.value
    } finally {
      positioningRefs.value = savedPositioningRefs
    }
    ensureMotionGridAnchorFirst()
    motionGridRefs.value = motionGridRefs.value.slice(0, maxMotionGridRefs)
    return motionGridRefs.value.length
  }

  function motionGridRefsPayload(): ResourceGenRef[] {
    return motionGridRefs.value
      .filter(r => r.source === 'resource' && r.resourceId)
      .map(r => ({
        id: r.resourceId!,
        variant: r.variant || '',
        kind: r.kind || '',
        label: r.label || '',
      }))
  }

  async function persistMotionGridDraft(shot: Shot, prompt: string) {
    shot.motionGridPrompt = prompt.trim()
    shot.motionGridRefs = motionGridRefsPayload()
    try {
      await saveShot(shot)
    } catch (e: any) {
      error.value = e.message
    }
  }

  async function analyzeMotionGridPrompt(shot: Shot) {
    if (!motionGridModal.value || motionGridModal.value.shotId !== shot.id) return
    motionGridModal.value = { ...motionGridModal.value, analyzing: true }
    error.value = ''
    try {
      await refreshProjectResourcesForPick().catch(() => {})
      const result = await api(`/shots/${shot.id}/analyze-motion-grid`, {
        method: 'POST',
        body: JSON.stringify({
          refLabels: motionGridRefs.value.map((r, i) => `图${i + 1}：${r.label}`),
        }),
      })
      if (!motionGridModal.value || motionGridModal.value.shotId !== shot.id) return
      // Auto-anchor: the previous shot's outro frame (帧9) becomes 图1.
      const anchorRaw = result.anchorRef as ResourceGenRef | undefined
      if (anchorRaw?.id && anchorRaw?.imageUrl) {
        motionGridAnchor.value = {
          key: `resource:${anchorRaw.id}:original`,
          source: 'resource',
          resourceId: anchorRaw.id,
          kind: 'other',
          variant: 'original',
          previewUrl: anchorRaw.imageUrl,
          label: anchorRaw.label || '上一镜收势帧',
        }
        ensureMotionGridAnchorFirst()
      }
      const raw = String(result.prompt || '')
      const { body } = splitPositioningPrompt(raw)
      await autoPickMotionGridRefsWithAI(shot, body || raw)
      const prompt = joinPositioningPrompt(body || raw, motionGridRefs.value)
      motionGridModal.value = {
        ...motionGridModal.value,
        prompt,
        analyzing: false,
      }
      await persistMotionGridDraft(shot, prompt)
    } catch (e: any) {
      if (motionGridModal.value?.shotId === shot.id) {
        motionGridModal.value = { ...motionGridModal.value, analyzing: false }
      }
      error.value = e.message
    }
  }

  async function openMotionGridModal(shot: Shot) {
    if (!shot.script.trim()) {
      error.value = '请先填写当前分镜文案'
      return
    }
    void loadProviders()
    expandShot(shot.id)
    await saveShot(shot)

    // Per-shot only: draft on this shot, or a 9帧图 already linked on this shot.
    const savedPrompt = (shot.motionGridPrompt || '').trim()
    const savedRefs = shot.motionGridRefs || []
    const ownRes = findMotionGridResourceOnShot(shot)

    motionGridReplaceIndex.value = null
    motionGridRefs.value = []
    motionGridAnchor.value = null
    let prompt = ''
    if (savedPrompt || savedRefs.length) {
      prompt = savedPrompt
      if (savedRefs.length) {
        motionGridRefs.value = buildSceneRefsFromGenRefs(savedRefs).slice(0, maxMotionGridRefs)
        const anchorIdx = motionGridRefs.value.findIndex(r => r.label === '上一镜收势帧')
        if (anchorIdx >= 0) motionGridAnchor.value = motionGridRefs.value[anchorIdx]
      }
    } else if (ownRes) {
      prompt = (ownRes.genPrompt || ownRes.description || '').trim()
      if (ownRes.genRefs?.length) {
        motionGridRefs.value = buildSceneRefsFromGenRefs(ownRes.genRefs).slice(0, maxMotionGridRefs)
        const anchorIdx = motionGridRefs.value.findIndex(r => r.label === '上一镜收势帧')
        if (anchorIdx >= 0) motionGridAnchor.value = motionGridRefs.value[anchorIdx]
      }
    }
    // Best-effort client-side anchor preview; the analyze call returns the authoritative one.
    if (!motionGridAnchor.value) {
      const anchorCell = findPrevShotOutroCell(shot)
      if (anchorCell) {
        motionGridAnchor.value = {
          key: `resource:${anchorCell.id}:original`,
          source: 'resource',
          resourceId: anchorCell.id,
          kind: 'other',
          variant: 'original',
          previewUrl: anchorCell.imageUrl,
          label: '上一镜收势帧',
        }
      }
    }
    if (motionGridAnchor.value) {
      ensureMotionGridAnchorFirst()
      motionGridRefs.value = motionGridRefs.value.slice(0, maxMotionGridRefs)
    }
    if (prompt) {
      const { body } = splitPositioningPrompt(prompt)
      prompt = joinPositioningPrompt(body || prompt, motionGridRefs.value)
    }

    motionGridModal.value = {
      shotId: shot.id,
      shotLabel: shot.label || `分镜 ${shot.sortOrder}`,
      prompt,
      analyzing: false,
      submitting: false,
      results: undefined,
    }
  }

  function setMotionGridPromptBody(body: string) {
    const modal = motionGridModal.value
    if (!modal) return
    motionGridModal.value = {
      ...modal,
      prompt: joinPositioningPrompt(body, motionGridRefs.value),
    }
  }

  function motionGridPromptBody(): string {
    if (!motionGridModal.value) return ''
    return splitPositioningPrompt(motionGridModal.value.prompt).body
  }

  async function closeMotionGridModal() {
    if (motionGridModal.value?.submitting) return
    const modal = motionGridModal.value
    const shot = modal ? activeEpisode.value?.shots.find(s => s.id === modal.shotId) : null
    if (shot && modal && (modal.prompt.trim() || motionGridRefs.value.length)) {
      await persistMotionGridDraft(shot, modal.prompt)
    }
    motionGridModal.value = null
    motionGridReplaceIndex.value = null
    motionGridAnchor.value = null
    if (refPickerTarget.value === 'motionGrid') {
      sceneRefPickerOpen.value = false
      refPickerTarget.value = 'resource'
    }
  }

  async function reanalyzeMotionGridPrompt() {
    const modal = motionGridModal.value
    if (!modal) return
    const shot = activeEpisode.value?.shots.find(s => s.id === modal.shotId)
    if (!shot) {
      error.value = '分镜不存在'
      return
    }
    await saveShot(shot)
    await analyzeMotionGridPrompt(shot)
  }

  function attachMotionGridResultToShot(shotId: number, resources: Resource[]) {
    const shot = activeEpisode.value?.shots.find(s => s.id === shotId)
    if (!shot || !resources.length) return
    const grids = resources.filter(r => r.genType === 'motion_grid')
    if (!grids.length) return
    const primary = grids.find(r => r.isGroupPrimary) || grids[0]
    if (!primary?.id) return
    addShotRef(shot, { kind: 'other', id: primary.id, variant: 'original', label: '9帧连续画面' })
  }

  async function confirmMotionGridGenerate() {
    const modal = motionGridModal.value
    if (!modal) return
    const { body } = splitPositioningPrompt(modal.prompt)
    const prompt = joinPositioningPrompt(body || modal.prompt.trim(), motionGridRefs.value).trim()
    if (!prompt) {
      error.value = '请填写9帧图提示词'
      return
    }
    if (!motionGridRefs.value.length) {
      error.value = '请至少选择一张参考图'
      return
    }
    if (motionGridRefs.value.length > maxMotionGridRefs) {
      error.value = `9帧图参考图最多 ${maxMotionGridRefs} 张，请先删减后再生成`
      return
    }
    const projectId = active.value?.id
    if (!projectId) return
    motionGridModal.value = { ...modal, prompt, submitting: true }
    error.value = ''
    try {
      const shot = activeEpisode.value?.shots.find(s => s.id === modal.shotId)
      if (shot) await persistMotionGridDraft(shot, prompt)
      const started = await api(`/shots/${modal.shotId}/generate-motion-grid`, {
        method: 'POST',
        body: JSON.stringify({
          name: modal.shotLabel,
          prompt,
          count: 1,
          resolution: imageResolution.value,
          quality: imageResolution.value,
          modelId: effectiveImageModelId.value || undefined,
          imageDataList: motionGridRefs.value.filter(r => r.source === 'upload').map(r => r.imageData).filter(Boolean),
          resourceRefs: motionGridRefs.value
            .filter(r => r.source === 'resource' && r.resourceId)
            .map(r => ({ id: r.resourceId, variant: r.variant || '' })),
        }),
      })
      const jobId = started.jobId as number
      const shotId = modal.shotId
      const jobRefs = motionGridRefsPayload()
      motionGridModal.value = null
      refPickerTarget.value = 'resource'
      upsertImageJobFromApi({
        id: jobId,
        projectId,
        type: 'motion_grid',
        status: started.status || 'pending',
        progress: started.progress ?? 0,
        message: started.message || '任务已提交，等待开始…',
        doneCount: 0,
        totalCount: 1,
        input: {
          name: modal.shotLabel,
          description: prompt,
          count: 1,
          shotId,
          resourceRefs: (() => {
            const mapped = jobRefs.map(r => ({
              id: r.id,
              variant: r.variant || '',
              kind: r.kind || '',
              label: r.label || '',
              imageUrl: motionGridRefs.value.find(p => p.resourceId === r.id)?.previewUrl || '',
            }))
            for (const p of motionGridRefs.value.filter(r => r.source === 'upload')) {
              mapped.push({
                id: 0,
                variant: 'original',
                kind: 'other',
                label: p.label || '',
                imageUrl: p.previewUrl || '',
              })
            }
            return mapped
          })(),
        },
      })
      focusedImageJobId.value = jobId
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'started' })
      void trackImageGenerationJob(
        projectId,
        jobId,
        (result) => {
          mergeNewResources(result.resources || [])
          attachMotionGridResultToShot(shotId, result.resources || [])
        },
        '未生成9帧图',
      )
    } catch (e: any) {
      if (motionGridModal.value) {
        motionGridModal.value = { ...motionGridModal.value, submitting: false }
      }
      error.value = e.message
    }
  }

  function openMotionGridJob(job: ImageGenJobView) {
    motionGridReplaceIndex.value = null
    motionGridAnchor.value = null
    const rawPrompt = (job.description || job.prompt || '').trim()
    const refs = job.resourceRefs || []
    if (refs.length > 0) {
      const keptUploads = motionGridRefs.value.filter(r => r.source === 'upload')
      motionGridRefs.value = buildSceneRefsFromGenRefs(refs).slice(0, maxMotionGridRefs)
      if (keptUploads.length) {
        const next = [...motionGridRefs.value]
        for (const u of keptUploads) {
          if (!next.some(r => r.key === u.key || (u.previewUrl && r.previewUrl === u.previewUrl))) {
            next.push(u)
          }
        }
        motionGridRefs.value = next.slice(0, maxMotionGridRefs)
      }
      const anchorIdx = motionGridRefs.value.findIndex(r => r.label === '上一镜收势帧')
      if (anchorIdx >= 0) motionGridAnchor.value = motionGridRefs.value[anchorIdx]
    }
    const { body } = splitPositioningPrompt(rawPrompt)
    const prompt = rawPrompt
      ? joinPositioningPrompt(body || rawPrompt, motionGridRefs.value)
      : ''
    let shotLabel = job.name || '9帧图'
    const shotId = job.shotId
    if (shotId) {
      const shot = activeEpisode.value?.shots.find(s => s.id === shotId)
        || active.value?.episodes.flatMap(e => e.shots).find(s => s.id === shotId)
      if (shot) {
        shotLabel = shot.label || `分镜 ${shot.sortOrder}`
        expandShot(shot.id)
      }
    }
    const gridResults = (job.resources || [])
      .filter(r => r.imageUrl && r.genType === 'motion_grid')
      .map((r, i) => ({ url: r.imageUrl, resourceId: r.id, label: r.name || `候选 ${i + 1}` }))
    const results = job.status === 'completed'
      ? (
          gridResults.length
            ? gridResults
            : (job.images || []).map((img, i) => ({
              url: img.url,
              resourceId: img.resourceId,
              label: `候选 ${i + 1}`,
            }))
        )
      : undefined
    if (job.status === 'completed') {
      mergeNewResources(job.resources || [])
    }
    studioTab.value = 'episodes'
    showAddForm.value = false
    motionGridModal.value = {
      shotId: shotId || 0,
      shotLabel,
      prompt,
      analyzing: false,
      submitting: false,
      results,
    }
  }

  // ---------- 场景9宫格（机位矩阵） ----------

  /** Mirrors services.BuildSceneGridPrompt; keep both copies in sync. */
  function buildSceneGridPromptTemplate(name: string, description: string, style: string): string {
    let subject = (name || '').trim()
    const desc = (description || '').trim()
    if (desc) subject = subject ? `${subject}：${desc}` : desc
    if (!subject) subject = '（待补充场景名称与描述）'
    let prompt = `输出一张 3×3 九宫格拼图：3行3列共9格，细窄深色分隔线，整体画面 16:9，空镜无人。
【空间主体】
${subject}
【九格机位必须一眼能分开】参考图/图1只提供这个房间的材质、家具、酒食和光线，禁止九格都复刻参考图的取景。只有格2允许接近参考图构图。
格1 第一行左 正面全景：平视、站得远。必须同时入画整张长桌/床/柜台、围合座位、门口。比参考图更远。
格2 第一行中 正面近景：平视推近主活动区桌面。这是唯一允许接近参考图构图的一格。
格3 第一行右 侧面全景：平视贴侧墙。桌子变成纵深一条线，尽头是另一面墙，不要正对门拍成格1。
格4 第二行左 侧面近景：平视侧向中近景，拍座位侧面和侧墙。
格5 第二行中 背面全景：平视反打，站到格1对面拍回来。格1里远处的门在这一格要靠近镜头或落到画面边缘。
格6 第二行右 背面近景：反打中近景，靠近背面的门或墙。
格7 第三行左 俯视全景：真实摄影机在天花板高度垂直往下拍的写实空镜。必须看见地板材质与家具立体顶面，桌子是俯视矩形，瓶口朝镜头。禁止平视。禁止白底黑线 CAD、建筑平面图、线稿示意图。
格8 第三行中 俯视近景：真实摄影机更近的写实俯拍，只拍桌面/地面局部，盘子是圆的。禁止 CAD 平面图、禁止线稿。
格9 第三行右 斜向高位总览：约45度斜上方写实空镜，同时看见桌面和一面墙。禁止平面图。
硬性：第三行（格7格8格9）必须是写实摄影俯视/高位机位，镜头高度明显高于前两行；若第三行出现白底黑线平面图、CAD、线稿布局图，或仍是平视桌面，就算失败。九格家具材质相同，但机位、远近、朝向必须不同。
画面内不要文字、标注、水印或logo。严禁出现人物、人影或剪影。`
    const s = (style || '').trim()
    if (s) prompt += `\n【整体画面质感】\n${s}`
    return prompt
  }

  function removeSceneGridReference(key: string) {
    sceneGridRefs.value = sceneGridRefs.value.filter(r => r.key !== key)
    if (sceneGridReplaceIndex.value != null && sceneGridReplaceIndex.value >= sceneGridRefs.value.length) {
      sceneGridReplaceIndex.value = null
    }
  }
  function clearSceneGridReferences() {
    sceneGridRefs.value = []
    sceneGridReplaceIndex.value = null
  }
  function openSceneGridRefPicker() {
    sceneGridPickingOverhead.value = false
    sceneGridReplaceIndex.value = null
    refPickerTarget.value = 'sceneGrid'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  function openSceneGridReplacePicker(index: number) {
    sceneGridPickingOverhead.value = false
    if (index < 0 || index >= sceneGridRefs.value.length) return
    sceneGridReplaceIndex.value = index
    refPickerTarget.value = 'sceneGrid'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  function openSceneGridOverheadPicker() {
    sceneGridReplaceIndex.value = null
    sceneGridPickingOverhead.value = true
    refPickerTarget.value = 'sceneGrid'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  async function addSceneGridRefFromFile(file: File, labelPrefix = '上传') {
    if (!file.type.startsWith('image/')) {
      error.value = '请选择图片文件'
      return false
    }
    const imageData = await readFileAsDataURL(file)
    const label = file.name
      ? `${labelPrefix} · ${file.name}`
      : `${labelPrefix} · 粘贴图片`
    const ref: SceneReference = {
      key: `upload:${file.name || 'paste'}:${Date.now()}:${Math.random()}`,
      source: 'upload',
      imageData,
      previewUrl: imageData,
      label,
    }
    const prev = refPickerTarget.value
    refPickerTarget.value = 'sceneGrid'
    if (sceneGridReplaceIndex.value != null) {
      addToActiveRefList(ref)
    } else {
      if (sceneGridRefs.value.length >= maxSceneGridRefs) {
        error.value = `最多添加 ${maxSceneGridRefs} 张参考图`
        refPickerTarget.value = prev
        return false
      }
      addToActiveRefList(ref)
    }
    refPickerTarget.value = prev
    return true
  }
  function onSceneGridRefFiles(e: Event) {
    const input = e.target as HTMLInputElement
    const files = input.files ? Array.from(input.files) : []
    input.value = ''
    if (!files.length) return
    void (async () => {
      let added = 0
      for (const file of files) {
        if (sceneGridRefs.value.length >= maxSceneGridRefs) break
        if (await addSceneGridRefFromFile(file)) added++
      }
      if (added < files.length) error.value = `最多添加 ${maxSceneGridRefs} 张参考图`
    })()
  }
  async function onSceneGridRefPaste(e: ClipboardEvent) {
    if (!sceneGridModal.value) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of items) {
      if (!item.type.startsWith('image/')) continue
      e.preventDefault()
      const file = item.getAsFile()
      if (!file) return
      await addSceneGridRefFromFile(file, '粘贴')
      return
    }
  }

  function openSceneGridModal(item?: Resource) {
    void loadProviders()
    sceneGridReplaceIndex.value = null
    sceneGridRefs.value = []
    const ident = sceneGridIdentityFromResource(item)
    const name = ident.name
    const prompt = refillSceneGridPromptFromIdentity(name, ident.description)
    if (item?.id && (item.imageUrl || item.stylizedImageUrl)) {
      const variant = item.imageUrl ? 'original' : 'stylized'
      const previewUrl = variant === 'original' ? item.imageUrl : item.stylizedImageUrl
      sceneGridRefs.value = [{
        key: `resource:${item.id}:${variant}`,
        source: 'resource',
        resourceId: item.id,
        kind: 'scene',
        variant,
        previewUrl,
        label: cleanRefAlias(resourceEditableName(item) || item.name || '') || '场景原图',
      }]
    }
    const persisted = loadSceneGridPersistedState(item, name)
    sceneGridModal.value = {
      resourceId: item?.id,
      name,
      prompt,
      submitting: false,
      results: undefined,
      overheadShapeLegend: persisted.legend,
      overheadSketch: persisted.overheadSketch,
      // Default candidate count for the "俯视布局线稿" step.
      // User can override in the modal UI.
      overheadSketchCandidateCount: 1,
    }
    if (!persisted.legend.trim()) void analyzeSceneGridShapeLegend()
    // Recover floor plan from in-flight / completed jobs even if library cache missed it.
    syncSceneGridOverheadFromJobs()
  }

  function closeSceneGridModal() {
    if (sceneGridModal.value?.submitting) return
    const modal = sceneGridModal.value
    if (modal && (modal.overheadShapeLegend || '').trim()) {
      void persistSceneGridShapeLegend(
        sceneLibraryRootId(modal.resourceId) || modal.resourceId,
        modal.overheadShapeLegend,
      )
    }
    sceneGridModal.value = null
    sceneGridReplaceIndex.value = null
    sceneGridPickingOverhead.value = false
    if (refPickerTarget.value === 'sceneGrid') {
      sceneRefPickerOpen.value = false
      refPickerTarget.value = 'resource'
    }
  }

  function refillSceneGridPrompt() {
    const modal = sceneGridModal.value
    if (!modal) return
    const name = sceneNameWithoutGridSuffix(modal.name || '')
    let desc = extractSceneGridSubject(modal.prompt || '')
    if (looksLikeSceneGridPrompt(desc)) desc = ''
    if (name && desc.startsWith(`${name}：`)) desc = desc.slice(name.length + 1).trim()
    if (!desc && modal.resourceId) {
      const item = resourceById(modal.resourceId)
      desc = sceneGridIdentityFromResource(item).description
    }
    sceneGridModal.value = {
      ...modal,
      name: name || modal.name,
      prompt: refillSceneGridPromptFromIdentity(name || modal.name, desc),
      overheadShapeLegend: modal.overheadShapeLegend || '',
    }
    if (!(modal.overheadShapeLegend || '').trim()) void analyzeSceneGridShapeLegend()
  }

  function sceneGridLegendSourceDesc(modal: NonNullable<SceneGridModalState>) {
    const name = sceneNameWithoutGridSuffix(modal.name || '')
    let desc = extractSceneGridSubject(modal.prompt || '')
    if (looksLikeSceneGridPrompt(desc)) desc = ''
    if (name && desc.startsWith(`${name}：`)) desc = desc.slice(name.length + 1).trim()
    if (!desc && modal.resourceId) {
      const item = resourceById(modal.resourceId)
      desc = sceneGridIdentityFromResource(item).description
    }
    return { name: name || modal.name || '', description: desc }
  }

  function fallbackSceneGridShapeLegend() {
    return `图2为人工确认二维建筑平面布局图，图片本身无任何文字。\n请按以下形状语义解读图2：\n1. 外框粗黑线 = 房间墙体边界\n2. 墙体缺口 = 门洞开口\n3. 门洞旁扇形弧线 = 门扇开启方向\n4. 留白窄带区域 = 人行通道`
  }

  function extractShapeLegendFromPrompt(text: string) {
    const raw = (text || '').trim()
    if (!raw) return ''
    const block = raw.match(/【图形语义对照[^】]*】\s*([\s\S]*?)(?=\n【|\n画面不包含|$)/)
    if (block?.[1]?.trim()) return block[1].trim()
    const idx = raw.indexOf('图2为人工确认二维建筑平面布局图')
    if (idx < 0) return ''
    const slice = raw.slice(idx)
    const end = slice.search(/\n【(?!图形语义)/)
    return (end > 0 ? slice.slice(0, end) : slice).trim()
  }

  function findSceneGridOverheadResource(sceneId?: number, sceneName?: string) {
    const rootId = sceneLibraryRootId(sceneId)
    const base = sceneNameWithoutGridSuffix(sceneName || '')
    const pool = active.value?.resources ?? []
    let best: Resource | undefined
    for (const r of pool) {
      if (r.type !== 'scene') continue
      if (!/(二维建筑平面布局图|俯视布局线稿)/.test(r.name || '')) continue
      const rBase = sceneNameWithoutGridSuffix(
        (r.name || '').replace(/\s*·\s*(二维建筑平面布局图|俯视布局线稿)\s*$/u, ''),
      )
      const parentMatch = !!rootId && r.parentId === rootId
      const nameMatch = !!base && rBase === base
      if (!parentMatch && !nameMatch) continue
      if (!r.imageUrl && !r.stylizedImageUrl) continue
      if (!best || String(r.createdAt || '') > String(best.createdAt || '')) best = r
    }
    return best
  }

  function loadSceneGridPersistedState(item?: Resource, sceneName?: string) {
    const rootId = sceneLibraryRootId(item?.id)
    const root = rootId ? resourceById(rootId) : item
    const overhead = findSceneGridOverheadResource(item?.id, sceneName || root?.name)
    const legend = (root?.sceneGridShapeLegend || '').trim()
      || loadSceneGridLegendLocal(rootId || item?.id)
      || extractShapeLegendFromPrompt(overhead?.genPrompt || overhead?.description || '')
    const overheadSketch = overhead?.imageUrl || overhead?.stylizedImageUrl
      ? {
        url: overhead.imageUrl || overhead.stylizedImageUrl,
        resourceId: overhead.id,
      }
      : undefined
    return { rootId, legend, overheadSketch }
  }

  async function persistSceneGridShapeLegend(resourceId?: number, legend?: string) {
    const text = (legend || '').trim()
    if (!resourceId || !text) return
    saveSceneGridLegendLocal(resourceId, text)
    const item = resourceById(resourceId)
    const modalName = sceneNameWithoutGridSuffix(sceneGridModal.value?.name || '')
    try {
      const updated = await api(`/resources/${resourceId}`, {
        method: 'PUT',
        body: JSON.stringify({
          name: (item ? resourceEditableName(item) : '') || item?.name || modalName || '场景',
          description: item?.description || '',
          remark: item?.remark || '',
          sceneGridShapeLegend: text,
        }),
      })
      upsertResourceInCaches(updated)
    } catch {
      // non-blocking: legend still lives in modal + localStorage
    }
  }

  async function analyzeSceneGridShapeLegend(force = false) {
    const modal = sceneGridModal.value
    const projectId = active.value?.id
    if (!modal || !projectId) return
    if (modal.overheadShapeLegendAnalyzing) return
    if (!force && (modal.overheadShapeLegend || '').trim()) return
    const { name, description } = sceneGridLegendSourceDesc(modal)
    if (!name.trim() && !description.trim()) {
      sceneGridModal.value = {
        ...modal,
        overheadShapeLegend: fallbackSceneGridShapeLegend(),
        overheadShapeLegendAnalyzing: false,
      }
      return
    }
    sceneGridModal.value = { ...modal, overheadShapeLegendAnalyzing: true }
    error.value = ''
    try {
      const controller = new AbortController()
      const timer = window.setTimeout(() => controller.abort(), 90000)
      const result = await api(`/projects/${projectId}/resources/analyze-scene-grid-legend`, {
        method: 'POST',
        signal: controller.signal,
        body: JSON.stringify({
          name,
          description,
          scenePrompt: modal.prompt || '',
        }),
      })
      window.clearTimeout(timer)
      if (!sceneGridModal.value) return
      const legend = String(result.legend || '').trim()
      const nextLegend = legend || fallbackSceneGridShapeLegend()
      sceneGridModal.value = {
        ...sceneGridModal.value,
        overheadShapeLegend: nextLegend,
        overheadShapeLegendAnalyzing: false,
      }
      const rid = sceneLibraryRootId(sceneGridModal.value.resourceId) || sceneGridModal.value.resourceId
      saveSceneGridLegendLocal(rid, nextLegend)
      void persistSceneGridShapeLegend(rid, nextLegend)
    } catch (e: any) {
      if (sceneGridModal.value) {
        sceneGridModal.value = {
          ...sceneGridModal.value,
          overheadShapeLegend: sceneGridModal.value.overheadShapeLegend || fallbackSceneGridShapeLegend(),
          overheadShapeLegendAnalyzing: false,
        }
      }
      const msg = e?.name === 'AbortError'
        ? '分析图形语义超时，已填入通用模板，可手动编辑或重试'
        : (e.message || '分析图形语义失败')
      error.value = msg
    }
  }

  async function reanalyzeSceneGridShapeLegend() {
    const modal = sceneGridModal.value
    if (!modal || modal.overheadShapeLegendAnalyzing) return
    sceneGridModal.value = { ...modal, overheadShapeLegend: '' }
    await analyzeSceneGridShapeLegend(true)
  }

  function sceneGridOverheadPrompt(modal: NonNullable<SceneGridModalState>) {
    const rawDesc = extractSceneGridSubject(modal.prompt || '')
    // Overhead sketch must be a CAD-like 2D plan.
    // Strip "scene atmosphere / lighting" wording that nudges the model toward
    // perspective interior illustrations.
    const desc = rawDesc
      ? rawDesc
        .replace(/内景|白日|狭小空间/g, '')
        .replace(/光线[^，。]*[，。]?/g, '')
        .replace(/明暗[^，。]*[，。]?/g, '')
        .replace(/氛围[^，。]*[，。]?/g, '')
        .replace(/压抑紧张/g, '')
        .replace(/破旧/g, '')
        .replace(/堆满/g, '堆放')
        .replace(/\s+/g, ' ')
        .replace(/[，。]{2,}/g, '。')
        .trim()
      : ''
    const descBlock = desc
      ? `\n场景语义（只识别空间用途与物件名称，禁止写成实景氛围）：\n${desc}\n`
      : ''
    const legend = (modal.overheadShapeLegend || fallbackSceneGridShapeLegend()).trim()
    const legendBlock = legend ? `\n【图形语义对照】\n${legend}\n` : ''
    return `【任务】生成一张 CAD 正交俯视二维建筑平面线稿，不是场景实景图，也不要根据任何照片去「画空镜」。
场景名：${sceneNameWithoutGridSuffix(modal.name)}。
画幅 16:9。纯白背景，黑色细实线，像建筑制图 / floor plan。

【输出必须是】白底黑线平面图、正交俯视、无透视。
【输出绝不能是】写实照片、夜景石门、电影空镜、室内透视图、3D 渲染、概念插画、UE5、胶片调色。

【投影】严格正上方垂直俯视，无透视、无消失点、无斜俯视、不画天花板与墙立面。
【物体画法】只画顶面轮廓符号（墙厚、门洞缺口、门扇开启弧线、桌椅顶面、通道留白）；不画高度体积、材质纹理、光影阴影、上色。
按下方图形语义与场景用途安排墙门家具的平面方位；不要发明实景光影。
${descBlock}${legendBlock}
画面不要任何文字、标签、数字、尺寸标注、水印 logo。线条干净，只表达平面方位。`
  }

  async function generateSceneGridOverhead() {
    const modal = sceneGridModal.value
    const projectId = active.value?.id
    if (!modal || !projectId || modal.overheadSubmitting) return
    if (!sceneGridRefs.value.length) {
      error.value = '请先保留或添加一张场景参考图'
      return
    }
    if (modal.overheadShapeLegendAnalyzing) {
      error.value = '正在分析图形语义，请稍候…'
      return
    }
    if (!(modal.overheadShapeLegend || '').trim()) {
      await analyzeSceneGridShapeLegend(true)
    }
    const readyModal = sceneGridModal.value
    if (!readyModal || readyModal.overheadShapeLegendAnalyzing) return
    void persistSceneGridShapeLegend(
      sceneLibraryRootId(readyModal.resourceId) || readyModal.resourceId,
      readyModal.overheadShapeLegend,
    )
    sceneGridModal.value = { ...readyModal, overheadSubmitting: true }
    error.value = ''
    let jobId = 0
    try {
      const prompt = sceneGridOverheadPrompt(readyModal)
      const count = Math.max(1, Math.min(6, Number(readyModal.overheadSketchCandidateCount || 1)))
      // Line-art floor plans: keep 1K. Do NOT pass photoreal scene refs — Seedream
      // otherwise copies night/stone empty plates and ignores CAD instructions.
      const overheadRes = '1k'
      const started = await api(`/projects/${projectId}/resources/generate-scene`, {
        method: 'POST',
        body: JSON.stringify({
          name: `${sceneNameWithoutGridSuffix(readyModal.name)} · 二维建筑平面布局图`,
          description: prompt,
          count,
          resolution: overheadRes,
          quality: overheadRes,
          modelId: effectiveImageModelId.value || undefined,
          candidateOnly: false,
          rawPrompt: true,
          imageDataList: [],
          resourceRefs: [],
          parentId: sceneLibraryRootId(readyModal.resourceId) || readyModal.resourceId || undefined,
        }),
      })
      jobId = started.jobId as number
      upsertImageJobFromApi({
        id: jobId, projectId, type: 'scene', status: started.status || 'pending',
        progress: started.progress ?? 0, message: started.message || '正在生成二维建筑平面布局图…',
        doneCount: 0, totalCount: count,
        input: {
          name: `${readyModal.name} · 二维建筑平面布局图`,
          description: prompt,
          count,
          parentId: sceneLibraryRootId(readyModal.resourceId) || readyModal.resourceId || undefined,
          resourceRefs: [],
        },
      })
      const result = await pollImageGenerationJob(projectId, jobId)
      const resources = (result.resources || []) as Resource[]
      type OverheadCandidate = { url: string; resourceId?: number; imageData?: string }
      let candidates: OverheadCandidate[] = resources.length
        ? resources.map(r => ({
          url: r.imageUrl || r.stylizedImageUrl || '',
          resourceId: r.id,
          imageData: undefined,
        }))
        : []

      // Fallback: some job responses may only include "images" (url + resourceId).
      if ((!candidates || candidates.length === 0) && Array.isArray((result as any)?.images)) {
        candidates = (result as any).images
          .filter((x: any) => typeof x?.url === 'string' && x.url.trim() !== '')
          .map((x: any) => ({
            url: x.url as string,
            resourceId: typeof x?.resourceId === 'number' ? x.resourceId : undefined,
            imageData: undefined as string | undefined,
          }))
      }

      if (!candidates?.length) throw new Error('未生成二维建筑平面布局图候选')

      // If backend returns resources but urls are empty, surface a clearer error.
      if (!candidates.some(c => !!c.url)) {
        throw new Error('候选二维平面布局图已生成但 URL 为空（可能是COS/本地文件未就绪）')
      }

      const first = candidates[0]
      if (!first?.url) throw new Error('未生成二维建筑平面布局图候选')
      mergeNewResources(result.resources || [])
      upsertImageJobFromApi(result)
      if (sceneGridModal.value) {
        sceneGridModal.value = {
          ...sceneGridModal.value,
          overheadSubmitting: false,
          overheadSketchCandidates: candidates,
          overheadSketch: first,
        }
      } else {
        // Modal was closed mid-job; keep job panel result so reopening / clicking can recover.
        applySceneGridOverheadFromJob(result)
      }
    } catch (e: any) {
      if (sceneGridModal.value) sceneGridModal.value = { ...sceneGridModal.value, overheadSubmitting: false }
      // Still try to recover from job panel if poll partially succeeded.
      const job = imageGenJobs.value.find(j => j.id === jobId)
      if (job) applySceneGridOverheadFromJob(job)
      error.value = e.message
    }
  }

  async function onSceneGridOverheadFile(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file || !sceneGridModal.value) return
    if (!file.type.startsWith('image/')) { error.value = '请选择图片文件'; return }
    const imageData = await readFileAsDataURL(file)
    sceneGridModal.value = {
      ...sceneGridModal.value,
      overheadSketch: { url: imageData, imageData },
      overheadSketchCandidates: undefined,
    }
  }

  function clearSceneGridOverhead() {
    if (!sceneGridModal.value) return
    sceneGridModal.value = { ...sceneGridModal.value, overheadSketch: undefined, overheadSketchCandidates: undefined }
  }

  async function confirmSceneGridGenerate() {
    const modal = sceneGridModal.value
    if (!modal) return
    let prompt = modal.prompt.trim()
    if (!prompt) {
      error.value = '请填写9宫格提示词'
      return
    }
    if (!modal.name.trim()) {
      error.value = '请填写场景名称'
      return
    }
    if (!modal.overheadSketch?.url) {
      error.value = '请先生成或上传二维建筑平面布局图，确认空间方位后再生成9宫格'
      return
    }
    if (looksLikeStaleSceneGridPrompt(prompt)) {
      prompt = refillSceneGridPromptFromIdentity(
        sceneNameWithoutGridSuffix(modal.name),
        extractSceneGridSubject(prompt),
      )
      sceneGridModal.value = { ...modal, prompt }
    }
    const projectId = active.value?.id
    if (!projectId) return
    const overheadFigure = sceneGridRefs.value.length + 1
    if (!prompt.includes('【二维平面布局图约束】')) {
      prompt = `${prompt}\n【二维平面布局图约束】图${overheadFigure}是已经人工确认的二维建筑平面布局图（白底黑线 CAD），仅用于锁定门、墙、桌子、沙发、座椅和通道的平面位置与朝向，禁止把图${overheadFigure}的线稿样式复制进九宫格任何一格。九格全部必须是写实摄影空镜：格1–6 平视/侧视/反打；格7–8 真实摄影机从天花板往下拍的写实俯视（可见地板材质与家具立体顶面，禁止平面图）；格9 斜向高位写实。场景材质、灯光和装修以图1场景原图为准。`
    }
    const shapeLegend = (modal.overheadShapeLegend || fallbackSceneGridShapeLegend()).trim()
    if (shapeLegend && !prompt.includes('【二维平面布局图形状语义对照】')) {
      prompt = `${prompt}\n【二维平面布局图形状语义对照】\n${shapeLegend}\n九个机位必须严格继承图${overheadFigure}中墙体、门洞、门扇开启方向、主要家具/堆放物与通道的平面位置、朝向和相对大小；但成片禁止出现 CAD / 白底黑线 / 示意图，只能画成写实摄影空镜。`
    }
    void persistSceneGridShapeLegend(
      sceneLibraryRootId(modal.resourceId) || modal.resourceId,
      shapeLegend,
    )
    sceneGridModal.value = { ...(sceneGridModal.value || modal), prompt, submitting: true }
    error.value = ''
    try {
      const started = await api(`/projects/${projectId}/resources/generate-scene-grid`, {
        method: 'POST',
        body: JSON.stringify({
          name: modal.name.trim(),
          description: prompt,
          count: 1,
          resolution: imageResolution.value,
          quality: imageResolution.value,
          modelId: effectiveImageModelId.value || undefined,
          imageDataList: [
            ...sceneGridRefs.value.filter(r => r.source === 'upload').map(r => r.imageData).filter(Boolean),
            modal.overheadSketch.imageData,
          ].filter(Boolean),
          resourceRefs: sceneGridRefs.value
            .filter(r => r.source === 'resource' && r.resourceId)
            .map(r => ({ id: r.resourceId, variant: r.variant || '' }))
            .concat(modal.overheadSketch.resourceId ? [{ id: modal.overheadSketch.resourceId, variant: 'original' }] : []),
          parentId: sceneLibraryRootId(modal.resourceId) || modal.resourceId || undefined,
        }),
      })
      const jobId = started.jobId as number
      sceneGridModal.value = null
      refPickerTarget.value = 'resource'
      upsertImageJobFromApi({
        id: jobId,
        projectId,
        type: 'scene_grid',
        status: started.status || 'pending',
        progress: started.progress ?? 0,
        message: started.message || '任务已提交，等待开始…',
        doneCount: 0,
        totalCount: 1,
        input: {
          name: modal.name.trim(),
          description: prompt,
          count: 1,
          resourceRefs: sceneGridRefs.value.map(r => ({
            id: r.resourceId || 0,
            variant: r.variant || (r.source === 'upload' ? 'original' : ''),
            kind: r.kind || (r.source === 'upload' ? 'other' : ''),
            label: r.label || '',
            imageUrl: r.previewUrl || '',
          })).concat([{
            id: modal.overheadSketch.resourceId || 0,
            variant: modal.overheadSketch.resourceId ? 'original' : 'original',
            kind: 'scene',
            label: '已确认俯视布局线稿',
            imageUrl: modal.overheadSketch.url,
          }]),
        },
      })
      focusedImageJobId.value = jobId
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'started' })
      void trackImageGenerationJob(
        projectId,
        jobId,
        (result) => {
          mergeNewResources(result.resources || [])
        },
        '未生成9宫格',
      )
    } catch (e: any) {
      if (sceneGridModal.value) {
        sceneGridModal.value = { ...sceneGridModal.value, submitting: false }
      }
      error.value = e.message
    }
  }

  function openSceneGridJob(job: ImageGenJobView) {
    sceneGridReplaceIndex.value = null
    const rawPrompt = (job.description || job.prompt || '').trim()
    const refs = job.resourceRefs || []
    const builtRefs = refs.length > 0 ? buildSceneRefsFromGenRefs(refs) : []
    const overheadRef = builtRefs.find(r => /俯视布局线稿/.test(r.label || ''))
    if (builtRefs.length > 0) {
      sceneGridRefs.value = builtRefs.filter(r => r !== overheadRef).slice(0, maxSceneGridRefs)
    }
    const gridResults = (job.resources || [])
      .filter(r => r.imageUrl && r.genType === 'scene_grid')
      .map((r, i) => ({ url: r.imageUrl, resourceId: r.id, label: r.name || `候选 ${i + 1}` }))
    const results = job.status === 'completed'
      ? (
          gridResults.length
            ? gridResults
            : (job.images || []).map((img, i) => ({
              url: img.url,
              resourceId: img.resourceId,
              label: `候选 ${i + 1}`,
            }))
        )
      : undefined
    if (job.status === 'completed') {
      mergeNewResources(job.resources || [])
    }
    studioTab.value = 'resources'
    resourceLibraryTab.value = 'library'
    const name = sceneNameWithoutGridSuffix(job.name || '') || '场景9宫格'
    let desc = extractSceneGridSubject(rawPrompt)
    if (looksLikeSceneGridPrompt(desc)) desc = ''
    if (name && desc.startsWith(`${name}：`)) desc = desc.slice(name.length + 1).trim()
    const prompt = looksLikeStaleSceneGridPrompt(rawPrompt) || !rawPrompt
      ? refillSceneGridPromptFromIdentity(name, desc)
      : rawPrompt
    const sceneResourceId = sceneGridRefs.value.find(r => r.kind === 'scene' && r.resourceId)?.resourceId
    const persisted = loadSceneGridPersistedState(sceneResourceId ? resourceById(sceneResourceId) : undefined, name)
    const legend = persisted.legend || extractShapeLegendFromPrompt(rawPrompt)
    sceneGridModal.value = {
      resourceId: sceneResourceId,
      name,
      prompt,
      submitting: false,
      overheadShapeLegend: legend,
      overheadSketch: overheadRef
        ? { url: overheadRef.previewUrl, resourceId: overheadRef.resourceId, imageData: overheadRef.imageData }
        : persisted.overheadSketch,
      results,
    }
    if (!legend.trim()) void analyzeSceneGridShapeLegend()
  }

  function looksLikeSceneReverseSkeletonPrompt(text: string) {
    return /这张线稿就是反打|必须对调前后景|禁止俯视平面图|空间关系线稿|轴线\+正反打|按图1描房间|必须画人|人物必须按图1|线稿硬锁|极简线稿|机位A\s*原镜头|A 正打/.test(text || '')
  }
  function looksLikeSceneReversePhotorealPrompt(text: string) {
    return /【反打优先】|【骨架优先】|把图1的火柴人换成|【参考图】图1是空间线稿|图1是反打镜头|根据图1空间线稿的机位B|生成图2房间的反打空镜|生成图2的反打镜头/.test(text || '')
  }
  function looksLikeStaleSceneReversePrompt(text: string) {
    const t = text || ''
    if (!t.trim()) return false
    if (!(looksLikeSceneReverseSkeletonPrompt(t) || looksLikeSceneReversePhotorealPrompt(t))) return false
    if (looksLikeSceneReverseSkeletonPrompt(t) && !/这张线稿就是反打/.test(t)) return true
    if (looksLikeSceneReverseSkeletonPrompt(t) && /俯视/.test(t) && /机位A/.test(t)) return true
    if (looksLikeSceneReverseSkeletonPrompt(t) && !/俯视格/.test(t)) return true
    if (looksLikeSceneReverseSkeletonPrompt(t) && !/必须四角换位/.test(t)) return true
    if (looksLikeSceneReverseSkeletonPrompt(t) && !/不要照片人物/.test(t) && !/不要定妆照/.test(t)) return true
    if (looksLikeSceneReversePhotorealPrompt(t) && /空镜无人/.test(t)) return true
    if (looksLikeSceneReversePhotorealPrompt(t) && /参考图：图\d为/.test(t) && !/反打镜头线稿|反打空间线稿/.test(t)) return true
    if (looksLikeSceneReversePhotorealPrompt(t) && !/骨架优先/.test(t) && !looksLikeSceneReverseSkeletonPrompt(t)) return true
    if (looksLikeSceneReversePhotorealPrompt(t) && !/俯视格/.test(t) && !looksLikeSceneReverseSkeletonPrompt(t)) return true
    if (looksLikeSceneReversePhotorealPrompt(t) && !/反打一侧/.test(t) && !looksLikeSceneReverseSkeletonPrompt(t)) return true
    if (looksLikeSceneReversePhotorealPrompt(t) && !/禁止换人/.test(t) && !looksLikeSceneReverseSkeletonPrompt(t)) return true
    if (looksLikeSceneReversePhotorealPrompt(t) && !/【反打图固定要求/.test(t) && !looksLikeSceneReverseSkeletonPrompt(t)) return true
    if (looksLikeSceneReversePhotorealPrompt(t) && !/姓名只认图1/.test(t) && !looksLikeSceneReverseSkeletonPrompt(t)) return true
    return false
  }
  function extractSceneReverseSubject(text: string) {
    const m = (text || '').match(/【空间】\s*([\s\S]+)$/)
    return (m?.[1] || '').trim()
  }
  function stripSceneNamePrefix(text: string, name: string) {
    const n = (name || '').trim()
    const t = (text || '').trim()
    if (n && t.startsWith(`${n}：`)) return t.slice(n.length + 1).trim()
    return t
  }
  function sceneReverseSubjectFromPrompt(prompt: string, name: string) {
    const raw = extractSceneReverseSubject(prompt) || (prompt || '').trim()
    if (looksLikeSceneReverseSkeletonPrompt(raw) || looksLikeSceneReversePhotorealPrompt(raw)) return ''
    return stripSceneNamePrefix(raw, name)
  }
  function stripImageRefLegend(text: string) {
    return (text || '').replace(/参考图：图\d为[\s\S]*?。(?:\s*按图号引用上方参考图，不要弄混。)?/g, (m) => (
      m.includes('反打空间线稿') || m.includes('反打镜头线稿') ? m : ''
    )).replace(/\n{3,}/g, '\n\n').trim()
  }
  function sceneReverseCleanSubject(name: string, description: string, cameraToken: string) {
    let subject = (name || '').trim()
    const d = stripImageRefLegend((description || '').trim())
    if (!d || d.includes(cameraToken) || d.includes('参考图：') || d.includes('【') || d.includes('图1') || d.includes('图2')) {
      return subject || '当前场景'
    }
    if ([...d].length > 80) return subject || '当前场景'
    if (subject) return `${subject}：${d}`
    return d
  }
  function buildSceneReverseSkeletonPrompt(name: string, description: string) {
    const subject = sceneReverseCleanSubject(name, description, '这张线稿就是反打')
    return `【这张线稿就是反打镜头】输出必须是反打机位「正在拍摄到的画面」：平视分镜线稿，16:9，白底黑线火柴人。禁止俯视平面图，禁止鸟瞰，禁止在一张图里画两个机位、轴线箭头、A正打/B反打图标。
【线稿硬锁 · 最高优先级】禁止照片、禁止真实皮肤五官、禁止服装花纹、禁止灯光材质。画成实拍或俯视示意图视为失败。
图1是原镜头（正打）。先看图1谁在近处、谁在远处、谁正对镜头、门在哪，然后把摄影机挪到桌子对面那一端（图1里远处、正对镜头的人背后），回头看向原镜头。
若还有图2俯视格或9宫格：只用来确认桌子、沙发、门在房间哪一端，线稿仍必须是平视反打画面。禁止画成俯视，禁止画成九宫格拼图。
【必须四角换位】摄影机水平旋转180度后，图1近处、背对镜头的人，线稿里必须变成远处、正对镜头；图1远处、正对镜头的人，必须变成近处、背影或过肩。屏幕方位也必须交叉：图1「近左→远右、近右→远左、远左→近右、远右→近左」。这是四角换位，不是照抄图1，也不是只做水平镜像。
【必须画人】图1有几个人就画几个火柴人，坐着画坐姿，站着/举杯画立姿。逐人跟踪身份，按上述四角规则移位；能辨认的人用中文名标注，不得把姓名留在图1的原位置。
图1里靠近镜头的门/门框，线稿里应画在远处背景，且屏幕左右也随180度反打变换。
房间用简单线框：桌椅卡座、门、窗。不要照片人物、不要定妆照。
【骨架硬约束】输出前逐人核对：图1远处正脸者现在必须在前景背影/过肩；图1近处背影者现在必须在对面正脸；四人场景必须四个角全部换位。任何人仍留在图1的前后/左右原位，都视为失败。
【空间】${subject}`
  }
  function buildSceneReversePrompt(name: string, description: string) {
    const subject = sceneReverseCleanSubject(name, description, '把图1的火柴人换成')
    return `【骨架优先 · 最高优先级】图1是反打镜头的火柴人线稿，这就是成片要拍到的画面。生成结果必须像把图1的火柴人换成真人：机位、透视、谁在前景/后景、谁正脸/背影/过肩、门在近还是远，全部按图1，不要按图2构图。
【参考图】图1=反打线稿（唯一的构图、人物位置、姓名依据），图2=原镜头/人物（只锁房间、五官服装，禁止复制图2里的姓名文字和马赛克位置），俯视格只锁平面布局（禁止抄俯视），反打一侧空镜只锁对面看到的房间（门、沙发、墙），禁止按空镜里的无人构图摆人。其余=角色定妆。
禁止再拍成图2那个机位。如果成片里仍是图2那种「门口往里拍、近处背影、远处正脸」，视为失败。
按图1每个火柴人旁的姓名，把它替换成对应人物定妆参考中的真人；若图2的旧姓名位置与图1冲突，绝对以图1为准。禁止换人、加人、少人。房间材质优先按反打一侧空镜，空镜没有的细节再按图2。
【反打图固定要求 · 姓名只认图1】所有人物的面部必须打满马赛克，马赛克彻底完全遮住人脸，五官完全不可见。人物身份、姓名以及姓名所在的人物位置，只能读取图1反打线稿；图2中已有的姓名文字、马赛克及其位置全部忽略，禁止照抄。每个人只出现一次姓名，姓名紧贴该人物头顶或肩旁；禁止同一姓名出现两次，禁止一个人旁边出现两个姓名，禁止把远处人物姓名放到近处人物身上。输出前逐人核对：人物数=马赛克数=姓名数，且每个姓名唯一并与图1对应。姓名做醒目、清晰、大号的中文「示意图悬浮标注」，不要做成衣服上的实体名牌、贴纸、号码布或缝在服装上的文字；除这组人物姓名外，不要其他文字、水印、logo 或 UI 边框。
遵守轴线：不要左右翻转图1。
【空间】${subject}`
  }
  function sceneReverseIdentityFromResource(item?: Resource): { name: string; description: string } {
    const ident = sceneGridIdentityFromResource(item)
    let desc = ident.description
    if (looksLikeSceneReverseSkeletonPrompt(desc) || looksLikeSceneReversePhotorealPrompt(desc)) {
      desc = sceneReverseSubjectFromPrompt(desc, ident.name)
    }
    return { name: ident.name, description: desc }
  }
  function sceneRefFromLibraryItem(item: Resource, fallbackLabel = '场景原图'): SceneReference | null {
    if (!item?.id) return null
    const variant: 'original' | 'stylized' | undefined = item.imageUrl
      ? 'original'
      : (item.stylizedImageUrl ? 'stylized' : undefined)
    if (!variant) return null
    const previewUrl = variant === 'original' ? item.imageUrl : item.stylizedImageUrl
    if (!previewUrl) return null
    const kind = item.type === 'character' || item.type === 'scene' || item.type === 'prop' ? item.type : 'other'
    return {
      key: kind === 'prop' ? `resource:${item.id}` : `resource:${item.id}:${variant}`,
      source: 'resource',
      resourceId: item.id,
      kind,
      variant: kind === 'prop' ? undefined : variant,
      previewUrl,
      label: cleanRefAlias(resourceEditableName(item) || item.name || '') || fallbackLabel,
    }
  }
  function originalSceneRefForReverse(modal: NonNullable<SceneReverseModalState>): SceneReference | null {
    if (modal.resourceId) {
      const fromList = sceneReverseRefs.value.find(r => r.resourceId === modal.resourceId)
      if (fromList) return fromList
      const item = resourceById(modal.resourceId)
      if (item) return sceneRefFromLibraryItem(item)
    }
    return sceneReverseRefs.value.find(r => r.kind === 'scene') || sceneReverseRefs.value[0] || null
  }
  function sceneReverseParentId(modal: NonNullable<SceneReverseModalState>): number | undefined {
    const orig = originalSceneRefForReverse(modal)
    return sceneLibraryRootId(orig?.resourceId || modal.resourceId)
  }
  function isSceneOverheadResource(r?: Resource | null): boolean {
    if (!r) return false
    if (r.genType === 'scene_grid') return true
    if (r.genType !== 'scene_grid_cell') return false
    const cell = r.gridCell || 0
    if (cell === 7 || cell === 8 || cell === 9) return true
    const angle = sceneGridAngleOf(r)
    return angle === '俯视全景' || angle === '俯视近景' || angle === '斜向高位总览'
  }
  function sceneLibraryRootId(startId?: number): number | undefined {
    if (!startId) return undefined
    let cur = resourceById(startId)
    if (!cur) return startId
    const seen = new Set<number>()
    while (cur) {
      if (seen.has(cur.id)) break
      seen.add(cur.id)
      if (cur.genType === 'scene_grid_cell' && cur.gridId) {
        const grid = resourceById(cur.gridId)
        if (grid) {
          cur = grid
          continue
        }
      }
      if (cur.parentId) {
        const parent = resourceById(cur.parentId)
        if (parent) {
          cur = parent
          continue
        }
      }
      break
    }
    const g = cur.genType || ''
    if (
      cur.type === 'scene'
      && g !== 'scene_reverse'
      && g !== 'scene_reverse_skeleton'
      && g !== 'scene_grid'
      && g !== 'scene_grid_cell'
    ) return cur.id
    return startId
  }
  function scenePlateStem(name: string): string {
    let n = sceneNameWithoutGridSuffix(name || '')
    const suffixes = ['站位图', '反打骨架', '反打']
    let changed = true
    while (changed) {
      changed = false
      for (const suf of suffixes) {
        if (n.endsWith(suf)) {
          n = n.slice(0, -suf.length).replace(/[ ·-]+$/u, '').trim()
          changed = true
        }
      }
    }
    return n
  }
  function sceneGridMatchesPlate(gridName: string, plateName: string): boolean {
    const gridBase = sceneNameWithoutGridSuffix(gridName)
    const plate = sceneNameWithoutGridSuffix(plateName)
    if (!gridBase || !plate) return false
    if (gridBase === plate) return true
    const gridStem = scenePlateStem(gridBase)
    const plateStem = scenePlateStem(plate)
    if (gridStem && gridStem === plateStem) return true
    const contains = (haystack: string, needle: string) => [...needle].length >= 2 && haystack.includes(needle)
    return contains(gridBase, plateStem) || contains(plate, gridStem) || contains(gridStem, plateStem) || contains(plateStem, gridStem)
  }
  function relatedSceneIdsForOverhead(item?: Resource): number[] {
    const ids = new Set<number>()
    const add = (id?: number) => {
      const root = sceneLibraryRootId(id)
      if (root) ids.add(root)
    }
    add(item?.id)
    for (const ref of item?.genRefs || []) add(ref.id)
    return [...ids]
  }
  function allSceneGrids(): Resource[] {
    return (active.value?.resources || []).filter(r =>
      r.genType === 'scene_grid' && !r.deletedAt && !!(r.imageUrl || r.stylizedImageUrl),
    )
  }
  function gridsForReverseScene(item?: Resource): Resource[] {
    const relatedIds = relatedSceneIdsForOverhead(item)
    const plates = relatedIds
      .map(id => resourceById(id)?.name || '')
      .concat(item?.name || '')
      .filter(Boolean)
    const grids = allSceneGrids().filter((r) => {
      if (relatedIds.includes(r.parentId || 0) || relatedIds.includes(r.id)) return true
      if ((r.genRefs || []).some(g => relatedIds.includes(sceneLibraryRootId(g.id) || 0))) return true
      return plates.some(plate => sceneGridMatchesPlate(r.name, plate))
    })
    if (grids.length) return grids
    const all = allSceneGrids()
    return all.length === 1 ? all : []
  }
  function oppositeSceneGridCell(cell: number): number {
    if (cell === 1) return 5
    if (cell === 2) return 6
    if (cell === 3 || cell === 4) return 5
    if (cell === 5) return 1
    if (cell === 6) return 2
    return 5
  }
  function inferOriginalGridCell(item?: Resource | null): number {
    if (!item) return 1
    if (item.genType === 'scene_grid_cell' && (item.gridCell || 0) >= 1 && (item.gridCell || 0) <= 6) {
      return item.gridCell || 1
    }
    const angle = sceneGridAngleOf(item)
    const idx = SCENE_GRID_ANGLES.indexOf(angle as typeof SCENE_GRID_ANGLES[number])
    if (idx >= 0 && idx <= 5) return idx + 1
    const n = item.name || ''
    if (/背面/.test(n)) return 5
    if (/侧面/.test(n)) return 3
    return 1
  }
  function pickGridCell(cells: Resource[], wanted: number[]): Resource | undefined {
    for (const cell of wanted) {
      const hit = cells
        .filter(c => (c.gridCell || 0) === cell || sceneGridAngleOf(c) === SCENE_GRID_ANGLES[cell - 1])
        .sort((a, b) => b.id - a.id)[0]
      if (hit) return hit
    }
    return undefined
  }
  function labeledGridCellRef(cell: Resource, suffix: string): SceneReference | null {
    const ref = sceneRefFromLibraryItem(cell, sceneGridCellRefLabel(cell) || '场景格')
    if (!ref) return null
    const angle = sceneGridAngleOf(cell) || sceneGridCellRefLabel(cell)
    ref.label = `${angle}${suffix}`
    return ref
  }
  function applySceneReverseGridRefs() {
    const modal = sceneReverseModal.value
    if (!modal?.gridId) return
    const original = originalSceneRefForReverse(modal)
    const keep = sceneReverseRefs.value.filter((r) => {
      if (original && (r.resourceId === original.resourceId || r.key === original.key)) return true
      if (r.source === 'upload') return true
      const res = r.resourceId ? resourceById(r.resourceId) : null
      if (!res) return true
      if (res.genType === 'scene_grid' || res.genType === 'scene_grid_cell') return false
      return true
    })
    const others = keep.filter(r => !original || (r.resourceId !== original.resourceId && r.key !== original.key))
    const cells = gridCellsFor(modal.gridId)
    const gridRefs: SceneReference[] = []
    const overhead = pickGridCell(cells, [7, 9])
    if (overhead) {
      const ref = labeledGridCellRef(overhead, '（自动）')
      if (ref) gridRefs.push(ref)
    } else {
      const grid = resourceById(modal.gridId)
      if (grid) {
        const ref = sceneRefFromLibraryItem(grid, '场景9宫格（用底排俯视格）')
        if (ref) {
          ref.label = `${cleanRefAlias(resourceEditableName(grid) || grid.name || '')} · 俯视全景（自动）`
          gridRefs.push(ref)
        }
      }
    }
    if (modal.skeleton) {
      const origItem = original?.resourceId
        ? resourceById(original.resourceId)
        : resourceById(modal.resourceId || 0)
      const want = oppositeSceneGridCell(inferOriginalGridCell(origItem))
      const opposite = pickGridCell(cells, [want, want === 5 ? 6 : 5, want === 1 ? 2 : 1])
      if (opposite && opposite.id !== overhead?.id) {
        const ref = labeledGridCellRef(opposite, '（反打一侧·自动）')
        if (ref && !gridRefs.some(g => g.resourceId === ref.resourceId)) gridRefs.push(ref)
      }
    }
    const next = [
      ...(original ? [original] : []),
      ...gridRefs,
      ...others,
    ].slice(0, maxSceneReverseRefs)
    sceneReverseRefs.value = next
  }
  function gridIdFromSceneRefs(refs: { id?: number }[]): number | undefined {
    for (const ref of refs) {
      if (!ref.id) continue
      const r = resourceById(ref.id)
      if (r?.genType === 'scene_grid') return r.id
      if (r?.genType === 'scene_grid_cell' && r.gridId) return r.gridId
    }
  }
  async function selectSceneReverseGrid(grid: Resource) {
    if (!sceneReverseModal.value || grid.genType !== 'scene_grid') return
    const existing = await loadGridCells(grid.id)
    if (!existing.length) {
      await splitGridResource(grid)
    }
    if (!sceneReverseModal.value) return
    sceneReverseModal.value = { ...sceneReverseModal.value, gridId: grid.id }
    applySceneReverseGridRefs()
  }
  const sceneReverseGridChoices = computed(() => {
    const item = sceneReverseModal.value?.resourceId
      ? resourceById(sceneReverseModal.value.resourceId)
      : undefined
    const related = new Set(gridsForReverseScene(item).map(g => g.id))
    return allSceneGrids().slice().sort((a, b) => {
      const ar = related.has(a.id) ? 1 : 0
      const br = related.has(b.id) ? 1 : 0
      if (ar !== br) return br - ar
      return b.id - a.id
    })
  })
  function seedSceneReverseOverheadRefs(item?: Resource) {
    const modal = sceneReverseModal.value
    if (modal?.gridId) {
      applySceneReverseGridRefs()
      return
    }
    const grid = gridsForReverseScene(item).sort((a, b) => b.id - a.id)[0]
    if (grid) {
      void selectSceneReverseGrid(grid)
      return
    }
  }
  function sceneReverseOverheadFromRefs(original: SceneReference | null): SceneReference[] {
    if (!original) return []
    return sceneReverseRefs.value.filter((r) => {
      if (!r.resourceId || r.resourceId === original.resourceId || r.key === original.key) return false
      return isSceneOverheadResource(resourceById(r.resourceId))
    }).slice(0, 1)
  }
  function reverseSkeletonRef(skeleton: { url: string; resourceId?: number }): SceneReference {
    if (skeleton.resourceId) {
      return {
        key: `resource:${skeleton.resourceId}:original`,
        source: 'resource',
        resourceId: skeleton.resourceId,
        kind: 'other',
        variant: 'original',
        previewUrl: skeleton.url,
        label: '反打线稿',
      }
    }
    return {
      key: 'upload:scene-reverse-skeleton',
      source: 'upload',
      imageData: skeleton.url,
      kind: 'other',
      previewUrl: skeleton.url,
      label: '反打线稿',
    }
  }
  function sceneReverseCharacterIdentityKey(item: Resource): string {
    return item.parentId ? `parent:${item.parentId}` : `resource:${item.id}`
  }
  function collectSceneReverseCharacterRefs(modal: NonNullable<SceneReverseModalState>): SceneReference[] {
    const original = originalSceneRefForReverse(modal)
    const originalItem = original?.resourceId ? resourceById(original.resourceId) : undefined
    if (!originalItem) return []

    // Generated scene/positioning images retain the character resources that created them.
    // Walk the parent chain as well because a scene derivative may only keep the scene plate ref.
    const directIds: number[] = []
    const seenResources = new Set<number>()
    let cursor: Resource | undefined = originalItem
    while (cursor && !seenResources.has(cursor.id)) {
      seenResources.add(cursor.id)
      for (const ref of cursor.genRefs || []) {
        const resource = ref.id ? resourceById(ref.id) : undefined
        if (resource?.type === 'character') directIds.push(resource.id)
      }
      cursor = cursor.parentId ? resourceById(cursor.parentId) : undefined
    }
    if (originalItem.shotId) {
      const shot = active.value?.episodes.flatMap(episode => episode.shots || []).find(item => item.id === originalItem.shotId)
      for (const ref of shot?.refs || []) {
        if (ref.kind === 'character') directIds.push(ref.id)
      }
    }

    // Older resources may not have structured genRefs. In that case, match character identity
    // names mentioned by the source prompt/description instead of blindly adding every character.
    const sourceText = [
      originalItem.name,
      originalItem.description,
      originalItem.genPrompt,
      ...(originalItem.genRefs || []).map(ref => ref.label || ''),
    ].filter(Boolean).join('\n')
    const namedIds: number[] = []
    if (sourceText) {
      for (const resource of active.value?.resources || []) {
        if (resource.type !== 'character' || !(resource.imageUrl || resource.stylizedImageUrl)) continue
        const identity = cleanRefAlias(resourceIdentityName(resource) || resourceEditableName(resource) || resource.name || '')
        const stem = entityStem(identity) || normalizeEntityName(identity)
        if (stem.length >= 2 && sourceText.includes(stem)) namedIds.push(resource.id)
      }
    }

    const refs: SceneReference[] = []
    const identities = new Set<string>()
    for (const id of [...directIds, ...namedIds]) {
      const item = resourceById(id)
      if (!item || item.type !== 'character') continue
      const identity = sceneReverseCharacterIdentityKey(item)
      if (identities.has(identity)) continue
      const ref = sceneRefFromLibraryItem(item, resourceDisplayName(item) || '人物参考')
      if (!ref) continue
      identities.add(identity)
      ref.label = `${cleanRefAlias(resourceDisplayName(item) || item.name || '')} · 人物定妆（自动）`
      refs.push(ref)
    }
    return refs
  }
  async function autoAppendSceneReverseCharacterRefs() {
    const before = sceneReverseModal.value
    if (!before?.skeleton) return
    await refreshProjectResourcesForPick().catch(() => {})
    const modal = sceneReverseModal.value
    if (!modal?.skeleton || modal.skeleton.url !== before.skeleton.url) return
    const candidates = collectSceneReverseCharacterRefs(modal)
    if (!candidates.length) return
    const existingIds = new Set(sceneReverseRefs.value.map(ref => ref.resourceId).filter(Boolean))
    const existingIdentities = new Set(sceneReverseRefs.value.flatMap((ref) => {
      const item = ref.resourceId ? resourceById(ref.resourceId) : undefined
      return item?.type === 'character' ? [sceneReverseCharacterIdentityKey(item)] : []
    }))
    const added: SceneReference[] = []
    for (const ref of candidates) {
      if (sceneReverseRefs.value.length + added.length >= maxSceneReverseRefs) break
      const item = ref.resourceId ? resourceById(ref.resourceId) : undefined
      const identity = item ? sceneReverseCharacterIdentityKey(item) : ''
      if (existingIds.has(ref.resourceId) || (identity && existingIdentities.has(identity))) continue
      if (ref.resourceId) existingIds.add(ref.resourceId)
      if (identity) existingIdentities.add(identity)
      added.push(ref)
    }
    if (!added.length) return
    sceneReverseRefs.value = [...sceneReverseRefs.value, ...added].slice(0, maxSceneReverseRefs)
    ElNotification({
      title: '已自动补充人物参考',
      message: `已从资源库选择 ${added.length} 张人物定妆图，用于生成反打图`,
      type: 'success',
      position: 'bottom-right',
      duration: 3000,
    })
  }
  function promptForSceneReverseStage(modal: NonNullable<SceneReverseModalState>, stage: 'skeleton' | 'final') {
    const name = sceneNameWithoutGridSuffix(modal.name)
    const raw = (modal.prompt || '').trim()
    const subject = sceneReverseSubjectFromPrompt(raw, name)
    if (stage === 'skeleton') {
      if (looksLikeSceneReverseSkeletonPrompt(raw) && !looksLikeStaleSceneReversePrompt(raw)) return raw
      return buildSceneReverseSkeletonPrompt(name, subject)
    }
    if (looksLikeSceneReversePhotorealPrompt(raw) && !looksLikeSceneReverseSkeletonPrompt(raw) && !looksLikeStaleSceneReversePrompt(raw)) return raw
    return buildSceneReversePrompt(name, subject)
  }
  function applySceneReverseSkeletonToModal(result: any) {
    const img = result?.images?.[0]
    const res = (result?.resources || []).find((r: Resource) => r.genType === 'scene_reverse_skeleton' && r.imageUrl)
      || (result?.resources || []).find((r: Resource) => r.imageUrl)
    const url = img?.url || res?.imageUrl
    const resourceId = img?.resourceId || res?.id
    if (!url) return
    const modal = sceneReverseModal.value
    if (!modal) return
    let prompt = modal.prompt
    if (looksLikeSceneReverseSkeletonPrompt(prompt)) {
      prompt = buildSceneReversePrompt(
        sceneNameWithoutGridSuffix(modal.name),
        sceneReverseSubjectFromPrompt(prompt, modal.name),
      )
    }
    sceneReverseModal.value = {
      ...modal,
      submitting: false,
      submittingStep: undefined,
      skeleton: { url, resourceId },
      prompt,
    }
    applySceneReverseGridRefs()
    void autoAppendSceneReverseCharacterRefs()
  }
  function snapshotSceneReverseRefsForJob(refs: SceneReference[]): ResourceGenRef[] {
    return refs.map((r, i) => ({
      id: r.resourceId || 0,
      variant: r.variant || (r.source === 'upload' ? 'original' : ''),
      kind: r.kind || (r.source === 'upload' ? 'other' : ''),
      label: r.label || `参考 ${i + 1}`,
      imageUrl: r.previewUrl || '',
    }))
  }
  function seedSceneReverseRefsFromJob(refs: ResourceGenRef[]) {
    const built = buildSceneRefsFromGenRefs(refs).filter((r) => {
      if (/线稿|骨架/.test(r.label || '')) return false
      if (r.resourceId) {
        const res = resourceById(r.resourceId)
        if (res?.genType === 'scene_reverse_skeleton') return false
      }
      return true
    })
    sceneReverseRefs.value = built.slice(0, maxSceneReverseRefs)
  }
  function findSceneReverseSkeleton(job: ImageGenJobView, name: string): { url: string; resourceId?: number } | undefined {
    for (const ref of job.resourceRefs || []) {
      if (ref.id) {
        const r = resourceById(ref.id)
        if (r?.genType === 'scene_reverse_skeleton' && r.imageUrl) {
          return { url: r.imageUrl, resourceId: r.id }
        }
      }
      if (/线稿|骨架/.test(ref.label || '') && ref.imageUrl) {
        return { url: ref.imageUrl, resourceId: ref.id || undefined }
      }
    }
    const base = sceneNameWithoutGridSuffix(name)
    const hits = (active.value?.resources || [])
      .filter(r => r.genType === 'scene_reverse_skeleton' && r.imageUrl && sceneNameWithoutGridSuffix(r.name) === base)
      .sort((a, b) => b.id - a.id)
    const r = hits[0]
    if (r?.imageUrl) return { url: r.imageUrl, resourceId: r.id }
  }
  function findSceneReverseSourceId(job: ImageGenJobView): number | undefined {
    for (const ref of job.resourceRefs || []) {
      if (!ref.id) continue
      const r = resourceById(ref.id)
      if (!r) continue
      if (
        r.type === 'scene'
        && r.genType !== 'scene_reverse'
        && r.genType !== 'scene_reverse_skeleton'
        && r.genType !== 'scene_grid'
        && r.genType !== 'scene_grid_cell'
      ) {
        return r.id
      }
    }
  }
  function sceneReverseJobImages(job: ImageGenJobView, genType: string) {
    const fromRes = (job.resources || [])
      .filter(r => r.imageUrl && r.genType === genType)
      .map((r, i) => ({ url: r.imageUrl, resourceId: r.id, label: r.name || `候选 ${i + 1}` }))
    if (fromRes.length) return fromRes
    return (job.images || []).map((img, i) => ({
      url: img.url,
      resourceId: img.resourceId,
      label: `候选 ${i + 1}`,
    }))
  }

  function removeSceneReverseReference(key: string) {
    sceneReverseRefs.value = sceneReverseRefs.value.filter(r => r.key !== key)
    if (sceneReverseReplaceIndex.value != null && sceneReverseReplaceIndex.value >= sceneReverseRefs.value.length) {
      sceneReverseReplaceIndex.value = null
    }
  }
  function clearSceneReverseReferences() {
    sceneReverseRefs.value = []
    sceneReverseReplaceIndex.value = null
  }
  function openSceneReverseRefPicker() {
    sceneReversePickingSkeleton.value = false
    sceneReverseReplaceIndex.value = null
    refPickerTarget.value = 'sceneReverse'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  function openSceneReverseSkeletonPicker() {
    sceneReverseReplaceIndex.value = null
    sceneReversePickingSkeleton.value = true
    refPickerTarget.value = 'sceneReverse'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  async function onSceneReverseSkeletonFile(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file || !sceneReverseModal.value) return
    if (!file.type.startsWith('image/')) {
      error.value = '请选择图片文件作为反打线稿'
      return
    }
    const imageData = await readFileAsDataURL(file)
    replaceSceneReverseSkeletonWithImage(imageData)
  }
  function replaceSceneReverseSkeletonWithImage(imageData: string) {
    const modal = sceneReverseModal.value
    if (!modal) return
    sceneReverseModal.value = {
      ...modal,
      skeleton: { url: imageData },
      prompt: buildSceneReversePrompt(
        sceneNameWithoutGridSuffix(modal.name),
        sceneReverseSubjectFromPrompt(modal.prompt, modal.name),
      ),
    }
    error.value = ''
    ElNotification({
      title: '已替换反打线稿',
      message: '手动修改的线稿将作为图1生成下一张反打图',
      type: 'success',
      position: 'bottom-right',
      duration: 3000,
    })
  }
  async function onSceneReverseSkeletonPaste(e: ClipboardEvent) {
    if (!sceneReverseModal.value?.skeleton) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of items) {
      if (!item.type.startsWith('image/')) continue
      const file = item.getAsFile()
      if (!file) return
      e.preventDefault()
      e.stopPropagation()
      const imageData = await readFileAsDataURL(file)
      replaceSceneReverseSkeletonWithImage(imageData)
      return
    }
  }
  function openSceneReverseReplacePicker(index: number) {
    if (index < 0 || index >= sceneReverseRefs.value.length) return
    sceneReverseReplaceIndex.value = index
    refPickerTarget.value = 'sceneReverse'
    sceneRefPickerOpen.value = true
    void refreshProjectResources()
  }
  async function addSceneReverseRefFromFile(file: File, labelPrefix = '上传') {
    if (!file.type.startsWith('image/')) {
      error.value = '请选择图片文件'
      return false
    }
    const imageData = await readFileAsDataURL(file)
    const label = file.name
      ? `${labelPrefix} · ${file.name}`
      : `${labelPrefix} · 粘贴图片`
    const ref: SceneReference = {
      key: `upload:${file.name || 'paste'}:${Date.now()}:${Math.random()}`,
      source: 'upload',
      imageData,
      previewUrl: imageData,
      label,
    }
    const prev = refPickerTarget.value
    refPickerTarget.value = 'sceneReverse'
    if (sceneReverseReplaceIndex.value != null) {
      addToActiveRefList(ref)
    } else {
      if (sceneReverseRefs.value.length >= maxSceneReverseRefs) {
        error.value = `最多添加 ${maxSceneReverseRefs} 张参考图`
        refPickerTarget.value = prev
        return false
      }
      addToActiveRefList(ref)
    }
    refPickerTarget.value = prev
    return true
  }
  function onSceneReverseRefFiles(e: Event) {
    const input = e.target as HTMLInputElement
    const files = input.files ? Array.from(input.files) : []
    input.value = ''
    if (!files.length) return
    void (async () => {
      let added = 0
      for (const file of files) {
        if (sceneReverseRefs.value.length >= maxSceneReverseRefs) break
        if (await addSceneReverseRefFromFile(file)) added++
      }
      if (added < files.length) error.value = `最多添加 ${maxSceneReverseRefs} 张参考图`
    })()
  }
  async function onSceneReverseRefPaste(e: ClipboardEvent) {
    if (!sceneReverseModal.value) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of items) {
      if (!item.type.startsWith('image/')) continue
      e.preventDefault()
      const file = item.getAsFile()
      if (!file) return
      await addSceneReverseRefFromFile(file, '粘贴')
      return
    }
  }

  function openSceneReverseModal(item?: Resource) {
    void loadProviders()
    sceneReverseReplaceIndex.value = null
    sceneReverseRefs.value = []
    const ident = sceneReverseIdentityFromResource(item)
    const name = ident.name || '场景'
    if (item?.id && (item.imageUrl || item.stylizedImageUrl)) {
      const ref = sceneRefFromLibraryItem(item)
      if (ref) sceneReverseRefs.value = [ref]
    }
    sceneReverseModal.value = {
      resourceId: item?.id,
      gridId: undefined,
      name,
      prompt: buildSceneReverseSkeletonPrompt(name, ident.description),
      submitting: false,
      skeleton: undefined,
      results: undefined,
    }
    seedSceneReverseOverheadRefs(item)
    void refreshProjectResourcesForPick().then(() => {
      if (!sceneReverseModal.value) return
      seedSceneReverseOverheadRefs(item)
    }).catch(() => {})
  }
  function closeSceneReverseModal() {
    if (sceneReverseModal.value?.submitting) return
    sceneReverseModal.value = null
    sceneReverseReplaceIndex.value = null
    sceneReversePickingSkeleton.value = false
    if (refPickerTarget.value === 'sceneReverse') {
      sceneRefPickerOpen.value = false
      refPickerTarget.value = 'resource'
    }
  }
  function refillSceneReversePrompt() {
    const modal = sceneReverseModal.value
    if (!modal) return
    const name = sceneNameWithoutGridSuffix(modal.name || '')
    let desc = sceneReverseSubjectFromPrompt(modal.prompt || '', name)
    if (!desc && modal.resourceId) {
      desc = sceneReverseIdentityFromResource(resourceById(modal.resourceId)).description
    }
    sceneReverseModal.value = {
      ...modal,
      name: name || modal.name,
      prompt: modal.skeleton
        ? buildSceneReversePrompt(name || modal.name, desc)
        : buildSceneReverseSkeletonPrompt(name || modal.name, desc),
    }
  }
  async function confirmSceneReverseSkeleton() {
    const modal = sceneReverseModal.value
    if (!modal) return
    const prompt = promptForSceneReverseStage(modal, 'skeleton').trim()
    if (!prompt) {
      error.value = '请填写空间线稿提示词'
      return
    }
    if (!modal.name.trim()) {
      error.value = '请填写场景名称'
      return
    }
    const original = originalSceneRefForReverse(modal)
    if (!original || (original.source === 'resource' && !original.resourceId && !original.imageData)) {
      error.value = '请先选择场景原图作为参考'
      return
    }
    const overhead = sceneReverseOverheadFromRefs(original)
    const skeletonRefs = [original, ...overhead]
    const projectId = active.value?.id
    if (!projectId) return
    sceneReverseModal.value = { ...modal, prompt, submitting: true, submittingStep: 'skeleton' }
    error.value = ''
    try {
      const started = await api(`/projects/${projectId}/resources/generate-scene-reverse`, {
        method: 'POST',
        body: JSON.stringify({
          name: modal.name.trim(),
          description: prompt,
          stage: 'skeleton',
          count: 1,
          resolution: '1k',
          quality: '1k',
          modelId: effectiveImageModelId.value || undefined,
          parentId: sceneReverseParentId(modal),
          imageDataList: skeletonRefs.filter(r => r.source === 'upload').map(r => r.imageData).filter(Boolean),
          resourceRefs: skeletonRefs
            .filter(r => r.source === 'resource' && r.resourceId)
            .map(r => ({ id: r.resourceId, variant: r.variant || 'original' })),
        }),
      })
      const jobId = started.jobId as number
      upsertImageJobFromApi({
        id: jobId,
        projectId,
        type: 'scene_reverse_skeleton',
        status: started.status || 'pending',
        progress: started.progress ?? 0,
        message: started.message || '正在生成反打线稿（平视，对调前后景）…',
        doneCount: 0,
        totalCount: 1,
        input: {
          name: `${sceneNameWithoutGridSuffix(modal.name)} · 反打骨架`,
          description: prompt,
          count: 1,
          resourceRefs: snapshotSceneReverseRefsForJob(skeletonRefs),
        },
      })
      focusedImageJobId.value = jobId
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'started' })
      sceneReverseModal.value = null
      refPickerTarget.value = 'resource'
      void trackImageGenerationJob(
        projectId,
        jobId,
        (result) => {
          mergeNewResources(result.resources || [])
        },
        '未生成反打线稿',
      )
    } catch (e: any) {
      error.value = e.message
    } finally {
      if (sceneReverseModal.value) {
        sceneReverseModal.value = {
          ...sceneReverseModal.value,
          submitting: false,
          submittingStep: undefined,
        }
      }
    }
  }
  async function confirmSceneReverseGenerate() {
    const modal = sceneReverseModal.value
    if (!modal) return
    if (!modal.skeleton?.url) {
      error.value = '请先生成空间线稿，确认机位对打后再生成正式反打图'
      return
    }
    const prompt = promptForSceneReverseStage(modal, 'final').trim()
    if (!prompt) {
      error.value = '请填写反打图提示词'
      return
    }
    if (!modal.name.trim()) {
      error.value = '请填写场景名称'
      return
    }
    const skeleton = reverseSkeletonRef(modal.skeleton)
    const original = originalSceneRefForReverse(modal)
    if (!original) {
      error.value = '请先选择场景原图作为参考'
      return
    }
    const extras = sceneReverseRefs.value.filter(r =>
      r.resourceId !== skeleton.resourceId
      && r.key !== skeleton.key
      && r.resourceId !== original.resourceId
      && r.key !== original.key,
    )
    const genRefs = [skeleton, original, ...extras].slice(0, maxSceneReverseRefs)
    const projectId = active.value?.id
    if (!projectId) return
    sceneReverseModal.value = { ...modal, prompt, submitting: true, submittingStep: 'final' }
    error.value = ''
    try {
      const started = await api(`/projects/${projectId}/resources/generate-scene-reverse`, {
        method: 'POST',
        body: JSON.stringify({
          name: modal.name.trim(),
          description: prompt,
          stage: 'final',
          count: 1,
          resolution: imageResolution.value,
          quality: imageResolution.value,
          modelId: effectiveImageModelId.value || undefined,
          parentId: sceneReverseParentId(modal),
          imageDataList: genRefs.filter(r => r.source === 'upload').map(r => r.imageData).filter(Boolean),
          resourceRefs: genRefs
            .filter(r => r.source === 'resource' && r.resourceId)
            .map(r => ({ id: r.resourceId, variant: r.variant || '' })),
        }),
      })
      const jobId = started.jobId as number
      sceneReverseModal.value = null
      refPickerTarget.value = 'resource'
      upsertImageJobFromApi({
        id: jobId,
        projectId,
        type: 'scene_reverse',
        status: started.status || 'pending',
        progress: started.progress ?? 0,
        message: started.message || '按已确认的线稿生成反打图…',
        doneCount: 0,
        totalCount: 1,
        input: {
          name: `${sceneNameWithoutGridSuffix(modal.name)} · 反打`,
          description: prompt,
          count: 1,
          resourceRefs: snapshotSceneReverseRefsForJob(genRefs),
        },
      })
      focusedImageJobId.value = jobId
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'started' })
      void trackImageGenerationJob(
        projectId,
        jobId,
        (result) => {
          mergeNewResources(result.resources || [])
        },
        '未生成反打图',
      )
    } catch (e: any) {
      if (sceneReverseModal.value) {
        sceneReverseModal.value = { ...sceneReverseModal.value, submitting: false, submittingStep: undefined }
      }
      error.value = e.message
    }
  }
  function openSceneReverseJob(job: ImageGenJobView) {
    sceneReverseReplaceIndex.value = null
    const refs = job.resourceRefs || []
    if (refs.length > 0) seedSceneReverseRefsFromJob(refs)
    const name = sceneNameWithoutGridSuffix(job.name || '') || '场景'
    const rawPrompt = (job.description || job.prompt || '').trim()
    const isSkeletonJob = job.type === 'scene_reverse_skeleton'
    const completedImages = job.status === 'completed'
      ? sceneReverseJobImages(job, isSkeletonJob ? 'scene_reverse_skeleton' : 'scene_reverse')
      : undefined
    if (job.status === 'completed') {
      mergeNewResources(job.resources || [])
    }
    studioTab.value = 'resources'
    resourceLibraryTab.value = 'library'
    const skeleton = isSkeletonJob && completedImages?.[0]
      ? { url: completedImages[0].url, resourceId: completedImages[0].resourceId }
      : (isSkeletonJob ? undefined : findSceneReverseSkeleton(job, name))
    let prompt = rawPrompt
    if (!prompt) {
      prompt = isSkeletonJob
        ? buildSceneReverseSkeletonPrompt(name, '')
        : buildSceneReversePrompt(name, '')
    } else if (isSkeletonJob && (looksLikeStaleSceneReversePrompt(prompt) || (looksLikeSceneReversePhotorealPrompt(prompt) && !looksLikeSceneReverseSkeletonPrompt(prompt)))) {
      prompt = buildSceneReverseSkeletonPrompt(name, sceneReverseSubjectFromPrompt(rawPrompt, name))
    } else if (!isSkeletonJob && (looksLikeStaleSceneReversePrompt(prompt) || looksLikeSceneReverseSkeletonPrompt(prompt))) {
      prompt = buildSceneReversePrompt(name, sceneReverseSubjectFromPrompt(prompt, name))
    }
    sceneReverseModal.value = {
      resourceId: findSceneReverseSourceId(job),
      gridId: gridIdFromSceneRefs(refs),
      name,
      prompt,
      submitting: false,
      skeleton,
      lastSentPrompt: job.prompt,
      results: isSkeletonJob ? undefined : completedImages,
    }
    if (sceneReverseModal.value.gridId) applySceneReverseGridRefs()
    else seedSceneReverseOverheadRefs(resourceById(sceneReverseModal.value.resourceId || 0))
  }

  function buildScenePanoramaPrompt(name: string, description = '') {
    const subject = (name || '当前场景').trim() || '当前场景'
    const desc = (description || subject).trim() || subject
    return `把该场景已有的「九宫格机位」展开成一张精确 2:1 的 360° 等距柱状全景图（equirectangular panorama）。
不是普通广角，不是再画一张九宫格，不是俯视平面图，不是多面板拼贴。

【场景】${subject}
【空间描述】${desc}

【核心做法：参照九宫格展开】
- 九宫格已经锁定同一房间的材质、家具与机位关系。全景必须像「把格1~格6沿水平 yaw 摊开」，不是重新发明一个地点。
- 格1 正面全景 → 全景图 x≈25% 正面中心（前半球视觉圣经）。
- 格5 背面全景 → 全景图 x≈75% 背面中心（水平 yaw 180° 反打）。
- 格3/格4 侧面 → 填 x≈50% 附近的左右接合带。
- 格7 俯视全景 → 只锁地面拓扑（桌/床/门/通道方位），禁止把整张全景画成俯视。
- 图1 正面底板补风格与近处细节；有反打空镜时只补后半球。

【等距柱状硬约束】
- 左右边缘无缝相接；地平线水平；禁止鱼眼、立方体贴图拼贴、九宫格分隔线。
- 空镜无人，无文字水印 logo。`
  }

  function applyScenePanoramaGridRefs() {
    const modal = scenePanoramaModal.value
    if (!modal?.gridId) return
    const grid = resourceById(modal.gridId)
    if (!grid) return
    const cells = gridCellsFor(modal.gridId)
    const plate = modal.resourceId ? resourceById(modal.resourceId) : undefined
    const refs: SceneReference[] = []
    const push = (r: Resource | undefined | null, label: string) => {
      if (!r || !(r.imageUrl || r.stylizedImageUrl)) return
      if (refs.some(x => x.resourceId === r.id)) return
      const labeled = (r.genType === 'scene_grid_cell' ? labeledGridCellRef(r, '') : null) || sceneRefFromLibraryItem(r)
      if (!labeled) return
      labeled.label = label
      refs.push(labeled)
    }
    push(plate, `${(plate ? resourceEditableName(plate) : '') || plate?.name || '场景'} · 正面底板`)
    push(pickGridCell(cells, [1, 2]), '正面全景格（格1）')
    push(pickGridCell(cells, [5, 6]), '背面全景格（格5）')
    push(pickGridCell(cells, [7, 8, 9]), '俯视全景格（格7）')
    push(pickGridCell(cells, [3, 4]), '侧面全景格（格3）')
    if (plate?.id && active.value) {
      const reverse = active.value.resources.find(r =>
        r.parentId === plate.id && r.genType === 'scene_reverse' && !r.deletedAt && !!(r.imageUrl || r.stylizedImageUrl),
      )
      push(reverse, '背面 / 反打空镜')
      const cad = active.value.resources.find(r =>
        r.parentId === plate.id
        && !r.deletedAt
        && /二维建筑平面布局图|俯视布局线稿/.test(r.name || '')
        && !!(r.imageUrl || r.stylizedImageUrl),
      )
      push(cad, '二维建筑平面布局图')
    }
    // Keep whole grid as last resort spatial bible if cells missing.
    if (refs.length < 3) push(grid, '场景9宫格整图')
    scenePanoramaRefs.value = refs.slice(0, maxScenePanoramaRefs)
  }

  async function selectScenePanoramaGrid(grid: Resource) {
    if (!scenePanoramaModal.value || grid.genType !== 'scene_grid') return
    const existing = await loadGridCells(grid.id)
    if (!existing.length) {
      await splitGridResource(grid)
    }
    if (!scenePanoramaModal.value) return
    scenePanoramaModal.value = { ...scenePanoramaModal.value, gridId: grid.id }
    applyScenePanoramaGridRefs()
  }

  const scenePanoramaGridChoices = computed(() => {
    const item = scenePanoramaModal.value?.resourceId
      ? resourceById(scenePanoramaModal.value.resourceId)
      : undefined
    const related = new Set(gridsForReverseScene(item).map(g => g.id))
    return allSceneGrids().slice().sort((a, b) => {
      const ar = related.has(a.id) ? 1 : 0
      const br = related.has(b.id) ? 1 : 0
      if (ar !== br) return br - ar
      return b.id - a.id
    })
  })

  function seedScenePanoramaRefs(item?: Resource | null) {
    const modal = scenePanoramaModal.value
    if (modal?.gridId) {
      applyScenePanoramaGridRefs()
      return
    }
    const grid = gridsForReverseScene(item || undefined).sort((a, b) => b.id - a.id)[0]
    if (grid) {
      void selectScenePanoramaGrid(grid)
      return
    }
    const refs: SceneReference[] = []
    const push = (r: Resource | undefined | null, label: string) => {
      if (!r || !(r.imageUrl || r.stylizedImageUrl)) return
      if (refs.some(x => x.resourceId === r.id)) return
      const ref = sceneRefFromLibraryItem(r)
      if (!ref) return
      ref.label = label
      refs.push(ref)
    }
    if (item) push(item, `${resourceEditableName(item) || item.name} · 正面底板`)
    scenePanoramaRefs.value = refs.slice(0, maxScenePanoramaRefs)
  }

  function openScenePanoramaModal(item?: Resource) {
    void loadProviders()
    const name = sceneNameWithoutGridSuffix(item?.name || '') || '场景'
    const desc = (item?.description || item?.genPrompt || '').trim()
    scenePanoramaModal.value = {
      resourceId: item?.id,
      gridId: undefined,
      name,
      prompt: buildScenePanoramaPrompt(name, desc),
      submitting: false,
      results: undefined,
    }
    seedScenePanoramaRefs(item)
    void refreshProjectResourcesForPick().then(() => {
      if (!scenePanoramaModal.value) return
      seedScenePanoramaRefs(item)
    }).catch(() => {})
  }

  function closeScenePanoramaModal() {
    if (scenePanoramaModal.value?.submitting) return
    scenePanoramaModal.value = null
  }

  function refillScenePanoramaPrompt() {
    const modal = scenePanoramaModal.value
    if (!modal) return
    const name = sceneNameWithoutGridSuffix(modal.name || '') || '场景'
    let desc = ''
    if (modal.resourceId) {
      const r = resourceById(modal.resourceId)
      desc = (r?.description || r?.genPrompt || '').trim()
    }
    scenePanoramaModal.value = { ...modal, name, prompt: buildScenePanoramaPrompt(name, desc) }
  }

  function removeScenePanoramaReference(key: string) {
    scenePanoramaRefs.value = scenePanoramaRefs.value.filter(r => sceneReferenceKey(r) !== key)
  }

  function clearScenePanoramaReferences() {
    scenePanoramaRefs.value = []
  }

  async function onScenePanoramaRefFiles(e: Event) {
    const input = e.target as HTMLInputElement
    const files = input.files ? Array.from(input.files) : []
    input.value = ''
    for (const file of files) {
      if (scenePanoramaRefs.value.length >= maxScenePanoramaRefs) break
      if (!file.type.startsWith('image/')) continue
      const dataUrl = await readFileAsDataURL(file)
      scenePanoramaRefs.value = [...scenePanoramaRefs.value, {
        key: `upload:pano:${Date.now()}:${file.name}`,
        source: 'upload' as const,
        imageData: dataUrl,
        previewUrl: dataUrl,
        label: file.name.replace(/\.[^.]+$/, '') || '上传参考',
      }].slice(0, maxScenePanoramaRefs)
    }
  }

  async function confirmScenePanoramaGenerate() {
    const modal = scenePanoramaModal.value
    if (!modal) return
    const prompt = (modal.prompt || '').trim()
    if (!prompt) {
      error.value = '请填写全景提示词'
      return
    }
    if (!modal.name.trim()) {
      error.value = '请填写场景名称'
      return
    }
    if (!scenePanoramaRefs.value.length) {
      error.value = '请至少提供场景正面底板或九宫格机位'
      return
    }
    if (!modal.gridId && !scenePanoramaRefs.value.some(r => (r.label || '').includes('全景格'))) {
      // Soft hint only — still allow master-only for rooms without a grid yet.
      ElMessage.warning('尚未选中 9 宫格：建议先生成并切分九宫格再展开全景，空间会更稳')
    }
    const projectId = active.value?.id
    if (!projectId) return
    scenePanoramaModal.value = { ...modal, submitting: true }
    error.value = ''
    try {
      const started = await api(`/projects/${projectId}/resources/generate-scene-panorama`, {
        method: 'POST',
        body: JSON.stringify({
          name: modal.name.trim(),
          description: prompt,
          count: 1,
          resolution: imageResolution.value,
          quality: imageResolution.value,
          modelId: effectiveImageModelId.value || undefined,
          parentId: modal.resourceId || undefined,
          imageDataList: scenePanoramaRefs.value.filter(r => r.source === 'upload').map(r => r.imageData).filter(Boolean),
          resourceRefs: scenePanoramaRefs.value
            .filter(r => r.source === 'resource' && r.resourceId)
            .map(r => ({ id: r.resourceId, variant: r.variant || 'original' })),
        }),
      })
      const jobId = started.jobId as number
      upsertImageJobFromApi({
        id: jobId,
        projectId,
        type: 'scene_panorama',
        status: started.status || 'pending',
        progress: started.progress ?? 0,
        message: started.message || '正在生成场景全景…',
        doneCount: 0,
        totalCount: 1,
        input: {
          name: `${modal.name.trim()} · 全景图`,
          description: prompt,
          count: 1,
          targetResourceId: undefined,
          resourceRefs: scenePanoramaRefs.value.map(r => ({
            id: r.resourceId || 0,
            variant: r.variant || '',
            kind: r.kind || 'scene',
            label: r.label || '',
            imageUrl: r.previewUrl || '',
          })),
        },
      })
      focusedImageJobId.value = jobId
      notifyStudioSync({ type: 'image-job', projectId, jobId, status: 'started' })
      void trackImageGenerationJob(
        projectId,
        jobId,
        (result) => {
          mergeNewResources(result.resources || [])
          const images = (result.images || []).map((img: any, i: number) => ({
            url: img.url,
            resourceId: img.resourceId,
            label: `全景 ${i + 1}`,
          }))
          if (scenePanoramaModal.value) {
            scenePanoramaModal.value = {
              ...scenePanoramaModal.value,
              submitting: false,
              results: images.length ? images : scenePanoramaModal.value.results,
            }
          }
        },
        '未生成全景图',
      )
      if (scenePanoramaModal.value) {
        scenePanoramaModal.value = { ...scenePanoramaModal.value, submitting: false }
      }
      ElMessage.success('全景任务已提交，可在右下角查看进度')
    } catch (e: any) {
      if (scenePanoramaModal.value) {
        scenePanoramaModal.value = { ...scenePanoramaModal.value, submitting: false }
      }
      error.value = e.message
    }
  }

  function openScenePanoramaJob(job: ImageGenJobView) {
    const refs = job.resourceRefs || []
    if (refs.length) {
      const seeded: SceneReference[] = []
      for (const ref of refs) {
        if (!ref.id) {
          if (!ref.imageUrl) continue
          seeded.push({
            key: `upload:pano-job:${seeded.length}`,
            source: 'upload',
            imageData: ref.imageUrl.startsWith('data:') ? ref.imageUrl : undefined,
            previewUrl: ref.imageUrl,
            label: ref.label || `参考 ${seeded.length + 1}`,
          })
          continue
        }
        const r = resourceById(ref.id)
        const fromLib = r ? sceneRefFromLibraryItem(r) : null
        if (fromLib) {
          if (ref.label) fromLib.label = ref.label
          seeded.push(fromLib)
        }
      }
      scenePanoramaRefs.value = seeded.slice(0, maxScenePanoramaRefs)
    }
    const name = sceneNameWithoutGridSuffix(job.name || '') || '场景'
    const completed = job.status === 'completed'
      ? (job.images || []).map((img, i) => ({
          url: img.url,
          resourceId: img.resourceId,
          label: `全景 ${i + 1}`,
        }))
      : undefined
    if (job.status === 'completed') mergeNewResources(job.resources || [])
    studioTab.value = 'resources'
    resourceLibraryTab.value = 'library'
    scenePanoramaModal.value = {
      resourceId: typeof job.targetResourceId === 'number' ? job.targetResourceId : undefined,
      name,
      prompt: (job.description || job.prompt || buildScenePanoramaPrompt(name, '')).trim(),
      submitting: job.status === 'pending' || job.status === 'running',
      results: completed,
    }
  }

  function openStylizeModal(item: Resource) {
    if (item.type !== 'character' && item.type !== 'scene' && item.type !== 'other' && item.type !== 'prop') return
    if (isStylizingResource(item.id)) return
    stylizeModal.value = {
      resourceId: item.id,
      resourceName: resourceDisplayName(item),
      type: item.type,
      prompt: defaultStylizePrompt(item.type),
    }
  }
  function closeStylizeModal() { stylizeModal.value = null }
  async function confirmStylize() {
    const modal = stylizeModal.value
    if (!modal) return
    const model = effectiveImageModel()
    if (isNanoBananaImageModel(model)) {
      try {
        await ElMessageBox.confirm(
          `当前选的是「${model?.name || 'Nano Banana'}」。用 Nano Banana 生成非真人图容易跑偏，不像定妆手绘，建议改选 Seedream 后再生成。`,
          '模型提示',
          {
            type: 'warning',
            confirmButtonText: '仍用 Nano Banana',
            cancelButtonText: '去换模型',
            distinguishCancelAndClose: true,
          },
        )
      } catch {
        return
      }
    }
    const item = active.value?.resources.find(r => r.id === modal.resourceId)
    const prompt = modal.prompt.trim()
    closeStylizeModal()
    if (!item) return
    void stylizeResource(item, prompt)
  }
  function openEditResourceModal(item: Resource) {
    editResourceModal.value = {
      resourceId: item.id,
      type: item.type,
      name: resourceEditableName(item),
      description: item.description || '',
      genPrompt: item.genPrompt || '',
      voicePrompt: item.voicePrompt || '',
      remark: item.remark || '',
    }
  }
  function canRegenerateResource(item: Resource) {
    if (item.type === 'video') return false
    if (item.source !== 'ai') return false
    if (item.genType === 'scene_grid_cell') return true
    return !!(item.genPrompt?.trim() || item.description?.trim() || (item.genRefs && item.genRefs.length))
  }
  function seedSceneReferencesFromGenRefs(refs: Resource['genRefs']) {
    if (!refs?.length) {
      sceneReferences.value = []
      return
    }
    const seeded: SceneReference[] = []
    for (const ref of refs) {
      // In-memory job snapshots may include uploads as id:0 + data/preview URL.
      if (!ref.id) {
        if (!ref.imageUrl) continue
        const key = `upload:job:${seeded.length}:${ref.label || ''}`
        if (seeded.some(r => r.previewUrl === ref.imageUrl)) continue
        seeded.push({
          key,
          source: 'upload',
          imageData: ref.imageUrl.startsWith('data:') ? ref.imageUrl : undefined,
          previewUrl: ref.imageUrl,
          label: ref.label || `上传参考 ${seeded.length + 1}`,
        })
        continue
      }
      const kind = (ref.kind || 'other') as ShotRef['kind']
      const variant = (ref.variant === 'original' || ref.variant === 'stylized')
        ? ref.variant
        : (kind === 'prop' ? undefined : 'stylized')
      const shotRef: ShotRef = kind === 'prop'
        ? { kind: 'prop', id: ref.id }
        : { kind: kind === 'character' || kind === 'scene' || kind === 'other' ? kind : 'other', id: ref.id, variant: variant || 'stylized' }
      const fromShot = sceneRefFromShotRef(shotRef)
      if (fromShot) {
        if (!seeded.some(r => r.key === fromShot.key)) seeded.push(fromShot)
        continue
      }
      if (!ref.imageUrl) continue
      const key = variant ? `resource:${ref.id}:${variant}` : `resource:${ref.id}`
      if (seeded.some(r => r.key === key)) continue
      seeded.push({
        key,
        source: 'resource',
        resourceId: ref.id,
        kind: shotRef.kind,
        variant,
        previewUrl: ref.imageUrl,
        label: ref.label || `资源 ${ref.id}`,
      })
    }
    sceneReferences.value = seeded.slice(0, maxSceneReferences)
  }
  function openRegenerateResource(item: Resource) {
    if (!canRegenerateResource(item)) return
    if (item.genType === 'scene_grid_cell') {
      // This is a fresh adjustment session. Existing jobs keep running in the task panel,
      // but must not lend their progress or references to the newly opened cell.
      focusedImageJobId.value = null
      studioTab.value = 'resources'
      resourceLibraryTab.value = 'library'
      switchResourceType('scene')
      const grid = item.gridId ? resourceById(item.gridId) : undefined
      const angle = sceneGridAngleOf(item) || `格${item.gridCell || ''}`
      const sceneName = shortGridSceneBase(grid?.name || item.name) || '当前场景'
      sceneReferences.value = []
      const currentRef = sceneRefFromLibraryItem(item, `${angle} · 当前格`)
      if (currentRef) {
        currentRef.label = `${angle}（待调整，只参考材质和内容）`
        sceneReferences.value.push(currentRef)
      }
      const cells = item.gridId ? gridCellsFor(item.gridId) : []
      const overhead = pickGridCell(cells.filter(cell => cell.id !== item.id), [7, 9, 8])
      let overheadFigure = 0
      if (overhead) {
        const overheadRef = sceneRefFromLibraryItem(overhead, '俯视全景')
        if (overheadRef) {
          overheadRef.label = `${sceneGridAngleOf(overhead) || '俯视全景'}（空间布局基准）`
          sceneReferences.value.push(overheadRef)
          overheadFigure = sceneReferences.value.length
        }
      }
      let gridFigure = 0
      if (grid) {
        const ref = sceneRefFromLibraryItem(grid, `${sceneName} · 9宫格整图`)
        if (ref) {
          ref.label = `${sceneName} · 9宫格整图（房间一致性）`
          sceneReferences.value.push(ref)
          gridFigure = sceneReferences.value.length
        }
      }
      regenerateResourceId.value = item.id
      resourceForm.value.name = item.name
      resourceForm.value.description = ''
      const refDuties = [
        '图1是当前待调整格：只参考房间材质、灯光、家具与摆设内容，不锁定它的错误机位和透视。',
        overheadFigure
          ? `图${overheadFigure}是俯视/高位空间布局基准：严格锁定门、墙、桌子、沙发、通道的平面位置和朝向，但最终画面仍必须是“${angle}”，除非目标机位本身是俯视，否则禁止输出俯视图。`
          : '',
        gridFigure
          ? `图${gridFigure}是原9宫格整图：只用于确认同一房间的色调、材质和结构一致性，禁止输出九宫格。`
          : '',
      ].filter(Boolean).join('\n')
      baseGenPrompt.value = `【单格机位调整】只生成一张16:9的独立场景空镜，不要九宫格，不要拼图。
【目标机位】${angle}。必须准确生成这个机位的摄影方向、景别、相机高度与透视，禁止沿用图1中可能错误的视角。
【参考图分工】${refDuties}
保持同一房间，禁止移动门窗、互换沙发与桌子、改变桌子朝向或凭空增减家具。画面无人、无文字、无水印、无logo。场景：${sceneName}`
      promptRevision.value = ''
      showAddForm.value = true
      error.value = ''
      return
    }
    if (item.genType === 'positioning') {
      seedPositioningRefsFromGenRefs(item.genRefs)
      studioTab.value = 'episodes'
      showAddForm.value = false
      positioningModal.value = {
        shotId: 0,
        shotLabel: resourceEditableName(item).replace(/\s*·\s*站位图$/, '') || '站位图',
        prompt: (item.genPrompt || item.description || '').trim(),
        analyzing: false,
        submitting: false,
      }
      error.value = ''
      return
    }
    if (item.genType === 'scene_grid') {
      sceneGridReplaceIndex.value = null
      const builtRefs = buildSceneRefsFromGenRefs(item.genRefs || [])
      const overheadRef = builtRefs.find(r => /(俯视布局线稿|二维建筑平面布局图)/.test(r.label || ''))
      sceneGridRefs.value = builtRefs.filter(r => r !== overheadRef).slice(0, maxSceneGridRefs)
      studioTab.value = 'resources'
      resourceLibraryTab.value = 'library'
      const sourceId = item.parentId || (item.genRefs || []).map(r => r.id).find((id) => {
          const r = resourceById(id)
          return !!r && r.type === 'scene' && r.genType !== 'scene_grid' && r.genType !== 'scene_grid_cell'
        })
      const gridName = resourceEditableName(item).replace(/\s*·\s*9宫格$/, '') || '场景9宫格'
      const persisted = loadSceneGridPersistedState(sourceId ? resourceById(sourceId) : undefined, gridName)
      sceneGridModal.value = {
        resourceId: sourceId,
        name: gridName,
        prompt: (item.genPrompt || item.description || '').trim(),
        submitting: false,
        overheadShapeLegend: persisted.legend || extractShapeLegendFromPrompt(item.genPrompt || item.description || ''),
        overheadSketch: overheadRef
          ? { url: overheadRef.previewUrl, resourceId: overheadRef.resourceId, imageData: overheadRef.imageData }
          : persisted.overheadSketch,
        results: item.imageUrl ? [{ url: item.imageUrl, resourceId: item.id, label: item.name }] : undefined,
      }
      if (!(sceneGridModal.value.overheadShapeLegend || '').trim()) void analyzeSceneGridShapeLegend()
      error.value = ''
      return
    }
    if (item.genType === 'scene_reverse' || item.genType === 'scene_reverse_skeleton') {
      sceneReverseReplaceIndex.value = null
      seedSceneReverseRefsFromJob(item.genRefs || [])
      studioTab.value = 'resources'
      resourceLibraryTab.value = 'library'
      const name = sceneNameWithoutGridSuffix(resourceEditableName(item) || item.name || '') || '场景'
      const rawPrompt = (item.genPrompt || item.description || '').trim()
      const isSkeleton = item.genType === 'scene_reverse_skeleton'
      const skeleton = isSkeleton && item.imageUrl
        ? { url: item.imageUrl, resourceId: item.id }
        : findSceneReverseSkeleton({ resourceRefs: item.genRefs || [], name: item.name } as ImageGenJobView, name)
      let prompt = rawPrompt
      if (!prompt) {
        prompt = isSkeleton ? buildSceneReverseSkeletonPrompt(name, '') : buildSceneReversePrompt(name, '')
      } else if (isSkeleton && (looksLikeStaleSceneReversePrompt(prompt) || (looksLikeSceneReversePhotorealPrompt(prompt) && !looksLikeSceneReverseSkeletonPrompt(prompt)))) {
        prompt = buildSceneReverseSkeletonPrompt(name, sceneReverseSubjectFromPrompt(rawPrompt, name))
      } else if (!isSkeleton && (looksLikeStaleSceneReversePrompt(prompt) || looksLikeSceneReverseSkeletonPrompt(prompt))) {
        prompt = buildSceneReversePrompt(name, sceneReverseSubjectFromPrompt(prompt, name))
      }
      sceneReverseModal.value = {
        resourceId: (item.genRefs || []).map(r => r.id).find((id) => {
          const r = resourceById(id)
          return !!r && r.type === 'scene'
            && r.genType !== 'scene_reverse'
            && r.genType !== 'scene_reverse_skeleton'
            && r.genType !== 'scene_grid'
            && r.genType !== 'scene_grid_cell'
        }) || item.parentId || undefined,
        gridId: gridIdFromSceneRefs(item.genRefs || []),
        name,
        prompt,
        submitting: false,
        skeleton,
        results: !isSkeleton && item.imageUrl ? [{ url: item.imageUrl, resourceId: item.id, label: item.name }] : undefined,
      }
      if (sceneReverseModal.value.gridId) applySceneReverseGridRefs()
      else seedSceneReverseOverheadRefs(resourceById(sceneReverseModal.value.resourceId || 0))
      error.value = ''
      return
    }
    if (item.genType === 'motion_grid' && item.shotId) {
      const shot = activeEpisode.value?.shots.find(s => s.id === item.shotId)
        || active.value?.episodes.flatMap(e => e.shots).find(s => s.id === item.shotId)
      if (shot) {
        void openMotionGridModal(shot)
        return
      }
    }
    studioTab.value = 'resources'
    resourceLibraryTab.value = 'library'
    const genType = item.genType || item.type
    const formType: 'character' | 'scene' | 'prop' =
      genType === 'character' || genType === 'prop' ? genType : 'scene'
    switchResourceType(formType)
    regenerateResourceId.value = item.id
    resourceForm.value.name = resourceEditableName(item).replace(/\s*·\s*站位图$/, '') || resourceEditableName(item)
    resourceForm.value.description = (item.description || '').trim()
    baseGenPrompt.value = unwrapStoredGenPrompt((item.genPrompt || item.description || '').trim())
    promptRevision.value = ''
    seedSceneReferencesFromGenRefs(item.genRefs)
    if (item.parentId) {
      sceneReferences.value = sceneReferences.value.filter(r => r.resourceId !== item.id)
      ensureDerivativeParentRef(item.parentId)
    } else {
      const selfPreview = item.imageUrl || item.stylizedImageUrl || ''
      if (selfPreview) {
        const variant: 'stylized' | 'original' = item.imageUrl ? 'original' : 'stylized'
        const selfKey = `resource:${item.id}:${variant}`
        if (!sceneReferences.value.some(r => r.key === selfKey)) {
          const kind = (item.type === 'character' || item.type === 'scene' || item.type === 'prop' || item.type === 'other')
            ? item.type
            : 'character'
          sceneReferences.value = [{
            key: selfKey,
            source: 'resource' as const,
            resourceId: item.id,
            kind,
            variant,
            previewUrl: variant === 'original' ? (item.imageUrl || item.stylizedImageUrl || '') : (item.stylizedImageUrl || item.imageUrl || ''),
            label: `${resourceDisplayName(item)} · 当前图`,
          }, ...sceneReferences.value].slice(0, maxSceneReferences)
        }
      }
    }
    characterCandidates.value = []
    selectedCandidate.value = ''
    candidateCount.value = 1
    const existingJob = imageGenJobs.value.find(j => j.targetResourceId === item.id)
    focusedImageJobId.value = existingJob?.id ?? null
    showAddForm.value = true
    error.value = ''
  }
  function closeEditResourceModal() { editResourceModal.value = null }
  async function confirmEditResource() {
    const modal = editResourceModal.value
    if (!modal) return
    if (!modal.name.trim()) { error.value = '请填写资源名称'; return }
    const item = active.value?.resources.find(r => r.id === modal.resourceId)
      || resourceTrash.value.find(r => r.id === modal.resourceId)
    if (!item) { closeEditResourceModal(); return }
    updatingResource.value = item.id
    error.value = ''
    try {
      const updated = await api(`/resources/${item.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          name: buildResourceName(item, modal.name),
          description: modal.description.trim(),
          genPrompt: modal.genPrompt?.trim() ?? '',
          voicePrompt: modal.voicePrompt?.trim() ?? '',
          remark: modal.remark.trim(),
        }),
      })
      upsertResourceInCaches(updated)
      closeEditResourceModal()
    } catch (e: any) { error.value = e.message }
    finally { updatingResource.value = null }
  }
  function sanitizeExportFilename(name: string) {
    return name
      .replace(/[\\/:*?"<>|]/g, '_')
      .replace(/\s+/g, ' ')
      .trim()
      .replace(/^\.+/, '') || '视频'
  }
  function videoExportStem(item: Resource) {
    const base = (item.name || '视频').trim() || '视频'
    const remark = (item.remark || '').trim()
    return sanitizeExportFilename(remark ? `${base}+${remark}` : base)
  }
  function videoExportExt(item: Resource) {
    const url = item.videoUrl || ''
    const match = url.match(/\.([a-zA-Z0-9]+)(?:\?|#|$)/)
    const ext = match?.[1]?.toLowerCase()
    if (ext && ['mp4', 'webm', 'mov', 'm4v'].includes(ext)) return `.${ext}`
    return '.mp4'
  }
  function videoExportFilename(item: Resource) {
    return `${videoExportStem(item)}${videoExportExt(item)}`
  }
  async function downloadVideoBlob(item: Resource) {
    if (!item.id) throw new Error('视频文件不可用')
    // Prefer browser → COS (or public CDN) so export does not hairpin through the API.
    const direct = (item.videoUrl || '').trim()
    if (direct && /^https?:\/\//i.test(direct)) {
      try {
        const res = await fetch(direct, { mode: 'cors', credentials: 'omit' })
        if (res.ok) {
          const blob = await res.blob()
          if (blob.size > 0) return blob
        }
      } catch {
        /* CORS / network — fall back to same-origin proxy */
      }
    }
    const res = await fetch(`/api/resources/${item.id}/download`)
    if (!res.ok) {
      let msg = `下载失败（HTTP ${res.status}）`
      try {
        const data = await res.json()
        if (data?.error) msg = data.error
      } catch { /* ignore */ }
      throw new Error(msg)
    }
    return res.blob()
  }

  async function runPool<T>(
    items: T[],
    concurrency: number,
    worker: (item: T, index: number) => Promise<void>,
  ) {
    let next = 0
    const runners = Array.from({ length: Math.min(Math.max(1, concurrency), items.length) }, async () => {
      while (true) {
        const i = next++
        if (i >= items.length) return
        await worker(items[i], i)
      }
    })
    await Promise.all(runners)
  }
  async function triggerBlobDownload(blob: Blob, filename: string) {
    return saveBlobDownload(blob, filename)
  }
  async function downloadShotVideoBlob(shotId: number) {
    const res = await fetch(`/api/shots/${shotId}/download`)
    if (!res.ok) {
      let msg = `下载失败（HTTP ${res.status}）`
      try {
        const data = await res.json()
        if (data?.error) msg = data.error
      } catch { /* ignore */ }
      throw new Error(msg)
    }
    return res.blob()
  }
  async function exportShotVideo(shot: Shot) {
    if (!shot.videoUrl) {
      error.value = '分镜视频不可用'
      return
    }
    error.value = ''
    try {
      const blob = await downloadShotVideoBlob(shot.id)
      const linked = shotActiveVideoResource(shot)
      const filename = linked
        ? videoExportFilename(linked)
        : sanitizeExportFilename(shot.label?.trim() || `分镜${shot.id}`) + '.mp4'
      await triggerBlobDownload(blob, filename)
    } catch (e: any) {
      error.value = e.message || '导出视频失败'
    }
  }
  type VideoExportProgress = {
    phase: 'list' | 'download' | 'pack' | 'save' | 'done'
    done: number
    total: number
    current?: string
    message?: string
  }

  type VideoExportResult = {
    count: number
    filename: string
    destination: 'dir' | 'default'
    dirName: string | null
    skipped?: number
  }

  async function exportVideoResource(item: Resource): Promise<VideoExportResult | null> {
    if (item.type !== 'video') return null
    error.value = ''
    try {
      const blob = await downloadVideoBlob(item)
      const filename = videoExportFilename(item)
      const destination = await triggerBlobDownload(blob, filename)
      const dirName = destination === 'dir' ? await getDownloadDirName() : null
      return { count: 1, filename, destination, dirName }
    } catch (e: any) {
      error.value = e.message || '导出视频失败'
      return null
    }
  }

  /** Keep in-memory zip only for tiny batches; larger exports write files one-by-one or batch. */
  const VIDEO_EXPORT_ZIP_MAX = 12
  /** Per client zip batch — small enough to avoid OOM, large enough to cut download chatter. */
  const VIDEO_EXPORT_BATCH_SIZE = 8
  const VIDEO_EXPORT_BATCH_CONCURRENCY = 5
  const VIDEO_EXPORT_FOLDER_CONCURRENCY = 6

  function uniqueVideoExportFilename(item: Resource, used: Map<string, number>) {
    let filename = videoExportFilename(item)
    const n = (used.get(filename) || 0) + 1
    used.set(filename, n)
    if (n > 1) {
      const ext = videoExportExt(item)
      filename = `${videoExportStem(item)}_${n}${ext}`
    }
    return filename
  }

  function exportOomHint(raw: string) {
    const msg = String(raw || '')
    if (/array buffer allocation failed|out of memory|oom/i.test(msg)) {
      return '导出内存不足：请分批导出，或在 HTTPS / Chrome 下选择下载目录后重试'
    }
    return msg || '批量导出失败'
  }

  function chunkArray<T>(items: T[], size: number): T[][] {
    const out: T[][] = []
    for (let i = 0; i < items.length; i += size) out.push(items.slice(i, i + size))
    return out
  }

  function sleep(ms: number) {
    return new Promise<void>(resolve => window.setTimeout(resolve, ms))
  }

  /**
   * Fast path on plain HTTP: browser pulls videos directly from COS in small
   * batches, packs each batch with STORE (no recompress), and downloads the zip.
   * Much faster than hairpinning everything through the app server.
   */
  async function exportVideoResourcesBatchedZip(
    videos: Resource[],
    onProgress?: (p: VideoExportProgress) => void,
  ): Promise<VideoExportResult | null> {
    const { default: JSZip } = await import('jszip')
    const used = new Map<string, number>()
    const failed: string[] = []
    let packed = 0
    let finished = 0
    const batches = chunkArray(videos, VIDEO_EXPORT_BATCH_SIZE)
    const zipNames: string[] = []
    let destination: 'dir' | 'default' = 'default'

    onProgress?.({
      phase: 'download',
      done: 0,
      total: videos.length,
      message: `直连加速下载（每批 ${VIDEO_EXPORT_BATCH_SIZE} 个，共 ${batches.length} 批）…`,
    })

    for (let bi = 0; bi < batches.length; bi++) {
      const batch = batches[bi]
      const zip = new JSZip()
      let batchPacked = 0
      await runPool(batch, Math.min(VIDEO_EXPORT_BATCH_CONCURRENCY, batch.length), async (item) => {
        const label = videoExportFilename(item)
        try {
          const blob = await downloadVideoBlob(item)
          const filename = uniqueVideoExportFilename(item, used)
          // STORE: videos are already compressed; skipping DEFLATE saves a lot of CPU/time.
          zip.file(filename, blob, { compression: 'STORE' })
          batchPacked += 1
          packed += 1
        } catch (e: any) {
          failed.push(label)
          console.warn('export skip', item.id, e?.message || e)
        } finally {
          finished += 1
          onProgress?.({
            phase: 'download',
            done: finished,
            total: videos.length,
            current: label,
            message: `下载 ${finished}/${videos.length}（第 ${bi + 1}/${batches.length} 批）`,
          })
        }
      })
      if (!batchPacked) continue

      onProgress?.({
        phase: 'pack',
        done: bi,
        total: batches.length,
        message: `打包第 ${bi + 1}/${batches.length} 批…`,
      })
      const zipBlob = await zip.generateAsync({
        type: 'blob',
        compression: 'STORE',
      })
      const zipName = batches.length === 1
        ? `分镜视频导出_${batchPacked}个.zip`
        : `分镜视频导出_${batchPacked}个_第${bi + 1}批共${batches.length}批.zip`
      onProgress?.({
        phase: 'save',
        done: finished,
        total: videos.length,
        current: zipName,
        message: `保存 ${zipName}…`,
      })
      destination = await triggerBlobDownload(zipBlob, zipName)
      zipNames.push(zipName)
      // Give the browser a beat between multi-download prompts / disk writes.
      if (bi < batches.length - 1) await sleep(350)
    }

    if (!packed) {
      error.value = failed.length
        ? `全部 ${failed.length} 个视频无法下载（文件可能已丢失）`
        : '没有可导出的视频'
      return null
    }
    const filename = zipNames.length === 1
      ? zipNames[0]
      : `${zipNames.length} 个分批 zip`
    const dirName = destination === 'dir' ? await getDownloadDirName() : null
    onProgress?.({
      phase: 'done',
      done: packed,
      total: videos.length,
      current: filename,
      message: failed.length ? `导出完成（成功 ${packed}，跳过 ${failed.length}）` : '导出完成',
    })
    return { count: packed, filename, destination, dirName, skipped: failed.length }
  }

  async function exportVideoResourcesViaServer(
    opts: { ids?: number[]; q?: string; expectedCount?: number },
    onProgress?: (p: VideoExportProgress) => void,
  ): Promise<VideoExportResult | null> {
    if (!active.value) {
      error.value = '请先选择项目'
      return null
    }
    const projectId = active.value.id
    const qs = new URLSearchParams()
    if (opts.ids?.length) {
      qs.set('ids', opts.ids.join(','))
    } else if (opts.q) {
      qs.set('q', opts.q)
    }
    onProgress?.({
      phase: 'list',
      done: 0,
      total: opts.expectedCount || 0,
      message: '检查可导出视频…',
    })
    const metaQs = new URLSearchParams(qs)
    metaQs.set('meta', '1')
    const meta = await api(`/projects/${projectId}/resources/export-videos?${metaQs.toString()}`)
    const count = typeof meta?.count === 'number' ? meta.count : 0
    if (!count) {
      error.value = '没有可导出的视频'
      return null
    }
    const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, (ch) => (ch === 'T' ? '_' : ch === ':' ? '' : '-'))
    const zipName = `videos_export_${count}_${stamp}.zip`
    onProgress?.({
      phase: 'pack',
      done: 0,
      total: count,
      current: zipName,
      message: `服务器打包中（约 ${count} 个），请在浏览器下载栏查看进度…`,
    })
    // Native download streams to disk — do NOT res.blob() (that would OOM again).
    const a = document.createElement('a')
    a.href = `/api/projects/${projectId}/resources/export-videos?${qs.toString()}`
    a.rel = 'noopener'
    document.body.appendChild(a)
    a.click()
    a.remove()
    onProgress?.({
      phase: 'done',
      done: count,
      total: count,
      current: zipName,
      message: '已开始下载，请查看浏览器下载栏',
    })
    return {
      count,
      filename: zipName,
      destination: 'default',
      dirName: null,
    }
  }

  async function exportVideoResourcesToFolder(
    videos: Resource[],
    onProgress?: (p: VideoExportProgress) => void,
    preselectedRoot?: FileSystemDirectoryHandle | null,
  ): Promise<VideoExportResult | null> {
    const root = preselectedRoot || await resolveDownloadDirectory({ promptIfMissing: true })
    if (!root) {
      // Plain HTTP: direct COS batches (faster than server hairpin zip).
      return exportVideoResourcesBatchedZip(videos, onProgress)
    }
    const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, (ch) => (ch === 'T' ? '_' : ch === ':' ? '' : '-'))
    const folderName = `分镜视频导出_${videos.length}个_${stamp}`
    const target = await ensureSubdirectory(root, folderName)
    const used = new Map<string, number>()
    const failed: string[] = []
    let packed = 0
    let finished = 0
    const concurrency = Math.min(VIDEO_EXPORT_FOLDER_CONCURRENCY, videos.length)
    onProgress?.({
      phase: 'download',
      done: 0,
      total: videos.length,
      current: folderName,
      message: `直连 COS 并行保存到「${folderName}」（${concurrency} 路）…`,
    })
    await runPool(videos, concurrency, async (item) => {
      const label = videoExportFilename(item)
      try {
        const blob = await downloadVideoBlob(item)
        const filename = uniqueVideoExportFilename(item, used)
        await writeBlobToDirectory(target, blob, filename)
        packed += 1
      } catch (e: any) {
        failed.push(label)
        console.warn('export skip', item.id, e?.message || e)
      } finally {
        finished += 1
        onProgress?.({
          phase: 'download',
          done: finished,
          total: videos.length,
          current: label,
          message: `保存视频 ${finished}/${videos.length}`,
        })
      }
    })
    if (!packed) {
      error.value = failed.length
        ? `全部 ${failed.length} 个视频无法下载（文件可能已丢失）`
        : '没有可导出的视频'
      return null
    }
    const dirName = root.name || (await getDownloadDirName())
    onProgress?.({
      phase: 'done',
      done: packed,
      total: videos.length,
      current: folderName,
      message: failed.length ? `导出完成（成功 ${packed}，跳过 ${failed.length}）` : '导出完成',
    })
    return {
      count: packed,
      filename: folderName,
      destination: 'dir',
      dirName,
      skipped: failed.length,
    }
  }

  async function exportVideoResourcesAsZip(
    videos: Resource[],
    onProgress?: (p: VideoExportProgress) => void,
  ): Promise<VideoExportResult | null> {
    const { default: JSZip } = await import('jszip')
    const zip = new JSZip()
    const used = new Map<string, number>()
    const failed: string[] = []
    let finished = 0
    const concurrency = Math.min(4, videos.length)
    onProgress?.({
      phase: 'download',
      done: 0,
      total: videos.length,
      message: `并行下载中（${concurrency} 路）…`,
    })
    await runPool(videos, concurrency, async (item) => {
      const label = videoExportFilename(item)
      try {
        const blob = await downloadVideoBlob(item)
        const filename = uniqueVideoExportFilename(item, used)
        zip.file(filename, blob, { compression: 'STORE' })
      } catch (e: any) {
        failed.push(label)
        console.warn('export skip', item.id, e?.message || e)
      } finally {
        finished += 1
        onProgress?.({
          phase: 'download',
          done: finished,
          total: videos.length,
          current: label,
          message: `下载视频 ${finished}/${videos.length}`,
        })
      }
    })
    const packed = Object.keys(zip.files).length
    if (!packed) {
      error.value = failed.length
        ? `全部 ${failed.length} 个视频无法下载（文件可能已丢失）`
        : '没有可导出的视频'
      return null
    }
    onProgress?.({
      phase: 'pack',
      done: 0,
      total: 100,
      message: failed.length
        ? `正在打包 zip…（已跳过 ${failed.length} 个失败项）`
        : '正在打包 zip…',
    })
    const zipBlob = await zip.generateAsync(
      {
        type: 'blob',
        compression: 'STORE',
      },
      (meta: { percent?: number }) => {
        const pct = Math.max(0, Math.min(100, Math.round(meta?.percent || 0)))
        onProgress?.({
          phase: 'pack',
          done: pct,
          total: 100,
          message: `正在打包 zip… ${pct}%`,
        })
      },
    )
    const zipName = failed.length
      ? `分镜视频导出_${packed}个_跳过${failed.length}.zip`
      : `分镜视频导出_${packed}个.zip`
    onProgress?.({
      phase: 'save',
      done: packed,
      total: videos.length,
      current: zipName,
      message: '写入本地…',
    })
    const destination = await triggerBlobDownload(zipBlob, zipName)
    const dirName = destination === 'dir' ? await getDownloadDirName() : null
    onProgress?.({
      phase: 'done',
      done: packed,
      total: videos.length,
      current: zipName,
      message: failed.length ? `导出完成（成功 ${packed}，跳过 ${failed.length}）` : '导出完成',
    })
    return { count: packed, filename: zipName, destination, dirName, skipped: failed.length }
  }

  async function exportVideoResources(
    items: Resource[],
    onProgress?: (p: VideoExportProgress) => void,
    options?: { downloadRoot?: FileSystemDirectoryHandle | null },
  ): Promise<VideoExportResult | null> {
    const videos = items.filter(r => r.type === 'video' && (r.videoUrl || r.id))
    if (!videos.length) {
      error.value = '请先选择要导出的视频'
      return null
    }
    error.value = ''
    try {
      if (videos.length === 1) {
        onProgress?.({
          phase: 'download',
          done: 0,
          total: 1,
          current: videoExportFilename(videos[0]),
          message: '下载视频…',
        })
        const one = await exportVideoResource(videos[0])
        if (one) {
          onProgress?.({ phase: 'done', done: 1, total: 1, current: one.filename, message: '导出完成' })
        }
        return one
      }
      // Prefer a folder path for large batches (or when a root was already chosen).
      if (videos.length > VIDEO_EXPORT_ZIP_MAX || options?.downloadRoot) {
        return await exportVideoResourcesToFolder(videos, onProgress, options?.downloadRoot)
      }
      const existingDir = await resolveDownloadDirectory({ promptIfMissing: false })
      if (existingDir) {
        return await exportVideoResourcesToFolder(videos, onProgress, existingDir)
      }
      // Small multi-select without a folder: one client zip is fine.
      return await exportVideoResourcesAsZip(videos, onProgress)
    } catch (e: any) {
      error.value = exportOomHint(e?.message || e)
      return null
    }
  }

  /** Fetch every library video matching the current search (all pages). */
  async function fetchAllLibraryVideos(onProgress?: (p: VideoExportProgress) => void): Promise<Resource[]> {
    if (!active.value) return []
    const projectId = active.value.id
    const q = resourceQuery.value.trim()
    const pageSize = 48
    const all: Resource[] = []
    let page = 1
    let total = Infinity
    onProgress?.({ phase: 'list', done: 0, total: 0, message: '拉取视频列表…' })
    while (all.length < total) {
      const qs = new URLSearchParams({
        page: String(page),
        pageSize: String(pageSize),
        type: 'video',
        enrich: '0',
      })
      if (q) qs.set('q', q)
      const data = await api(`/projects/${projectId}/resources?${qs.toString()}`)
      if (active.value?.id !== projectId) return all
      const items = Array.isArray(data?.items)
        ? data.items as Resource[]
        : (Array.isArray(data) ? data as Resource[] : [])
      total = typeof data?.total === 'number' ? data.total : (all.length + items.length)
      for (const item of items) {
        if (item.type === 'video') all.push(item)
      }
      onProgress?.({
        phase: 'list',
        done: all.length,
        total: Number.isFinite(total) ? total : all.length,
        message: `拉取列表 ${all.length}/${Number.isFinite(total) ? total : '?'}`,
      })
      if (!items.length || items.length < pageSize) break
      page += 1
      if (page > 200) break
    }
    return all
  }

  async function exportAllLibraryVideos(onProgress?: (p: VideoExportProgress) => void): Promise<VideoExportResult | null> {
    error.value = ''
    onProgress?.({ phase: 'list', done: 0, total: 0, message: '准备导出…' })
    // Fastest: write files in parallel from COS when directory picker works (HTTPS).
    const downloadRoot = isDownloadDirSupported()
      ? await resolveDownloadDirectory({ promptIfMissing: true })
      : null
    const videos = await fetchAllLibraryVideos(onProgress)
    if (!videos.length) {
      error.value = '没有可导出的视频'
      return null
    }
    if (downloadRoot) {
      return exportVideoResources(videos, onProgress, { downloadRoot })
    }
    try {
      // Next-fastest on HTTP: browser → COS in small batches (no server hairpin).
      return await exportVideoResourcesBatchedZip(videos, onProgress)
    } catch (e: any) {
      console.warn('batched export failed, falling back to server zip', e)
      try {
        return await exportVideoResourcesViaServer(
          {
            q: resourceQuery.value.trim() || undefined,
            expectedCount: videos.length,
          },
          onProgress,
        )
      } catch (e2: any) {
        error.value = exportOomHint(e2?.message || e?.message || e2)
        return null
      }
    }
  }
  const splittingGridIds = ref<Set<number>>(new Set())
  async function splitGridResource(item: Resource) {
    if (item.genType !== 'scene_grid' && item.genType !== 'motion_grid') return
    if (splittingGridIds.value.has(item.id)) return
    const next = new Set(splittingGridIds.value)
    next.add(item.id)
    splittingGridIds.value = next
    error.value = ''
    try {
      const result = await api(`/resources/${item.id}/split-grid`, { method: 'POST' })
      const cells = Array.isArray(result?.cells) ? result.cells as Resource[] : []
      if (cells.length) mergeGridCells(cells)
    } catch (e: any) {
      error.value = e.message || '切分9宫格失败'
    } finally {
      const done = new Set(splittingGridIds.value)
      done.delete(item.id)
      splittingGridIds.value = done
    }
  }

  const splittingPanoramaIds = ref<Set<number>>(new Set())
  async function splitPanoramaResource(item: Resource) {
    if (item.genType !== 'scene_panorama') return
    if (splittingPanoramaIds.value.has(item.id)) return
    const next = new Set(splittingPanoramaIds.value)
    next.add(item.id)
    splittingPanoramaIds.value = next
    error.value = ''
    try {
      const result = await api(`/resources/${item.id}/split-panorama`, { method: 'POST' })
      const views = Array.isArray(result?.views) ? result.views as Resource[] : []
      if (views.length) mergeGridCells(views)
    } catch (e: any) {
      error.value = e.message || '切出全景机位失败'
    } finally {
      const done = new Set(splittingPanoramaIds.value)
      done.delete(item.id)
      splittingPanoramaIds.value = done
    }
  }

  async function deleteResource(item: Resource) {
    const label = resourceTypeLabel(item.type)
    if (!confirm(`确定从资源管理中心删除${label}「${item.name}」？此操作不可恢复。`)) return
    await api(`/resources/${item.id}`, { method: 'DELETE' })
    await refreshProjectResources()
  }
  async function saveProvider(provider: Provider) {
    try {
      const key = providerKeys.value[provider.id]
      const payload = { name: provider.name, baseUrl: provider.baseUrl, ...(key ? { apiKey: key } : {}) }
      const p = await api(`/settings/providers/${provider.id}`, { method: 'PUT', body: JSON.stringify(payload) })
      providers.value = providers.value.map(x => x.id === p.id ? p : x)
      providerKeys.value[provider.id] = ''
    } catch (e: any) { error.value = e.message }
  }
  async function toggleKey(provider: Provider) {
    if (shownKeys.value[provider.id]) { shownKeys.value[provider.id] = false; return }
    try {
      const result = await api(`/settings/providers/${provider.id}/api-key`)
      providerKeys.value[provider.id] = result.apiKey
      shownKeys.value[provider.id] = true
    } catch (e: any) { error.value = e.message }
  }
  function openAddModel(provider: Provider, capability: 'text' | 'image' | 'video') {
    const preset = provider.slug === 'doubao-web-api' && capability === 'video' ? DOUBAO_WEB_VIDEO_PRESETS[0] : null
    modelForm.value = {
      providerId: provider.id,
      providerSlug: provider.slug,
      capability,
      name: preset?.name ?? '',
      modelId: preset?.modelId ?? '',
    }
  }
  function openEditModel(provider: Provider, model: AIModel) {
    modelForm.value = {
      providerId: provider.id,
      providerSlug: provider.slug,
      capability: model.capability,
      editingId: model.id,
      name: model.name,
      modelId: model.modelId,
    }
  }
  function closeModelForm() { modelForm.value = null }
  function onModelPresetChange(modelId: string) {
    if (!modelForm.value) return
    modelForm.value.modelId = modelId
    const preset = DOUBAO_WEB_VIDEO_PRESETS.find(p => p.modelId === modelId)
    if (preset && (!modelForm.value.name || DOUBAO_WEB_VIDEO_PRESETS.some(p => p.name === modelForm.value!.name))) {
      modelForm.value.name = preset.name
    }
  }
  async function saveModelForm() {
    const form = modelForm.value
    if (!form) return
    const name = form.name.trim()
    const modelId = form.modelId.trim()
    if (!name || !modelId) {
      error.value = '请填写模型名称与模型 ID'
      return
    }
    modelFormSaving.value = true
    error.value = ''
    try {
      if (form.editingId) {
        const updated = await api(`/settings/models/${form.editingId}`, {
          method: 'PUT',
          body: JSON.stringify({ name, modelId }),
        })
        const provider = providers.value.find(p => p.id === updated.providerId)
        if (provider) provider.models = provider.models.map(m => m.id === updated.id ? updated : m)
      } else {
        const model = await api(`/settings/providers/${form.providerId}/models`, {
          method: 'POST',
          body: JSON.stringify({ name, modelId, capability: form.capability }),
        })
        const provider = providers.value.find(p => p.id === form.providerId)
        if (provider) provider.models.push(model)
      }
      closeModelForm()
    } catch (e: any) {
      error.value = e.message
    } finally {
      modelFormSaving.value = false
    }
  }
  async function toggleModel(model: AIModel, field: 'enabled' | 'isDefault') {
    try {
      const updated = await api(`/settings/models/${model.id}`, { method: 'PUT', body: JSON.stringify({ [field]: !model[field] }) })
      providers.value = providers.value.map(p => {
        if (p.id !== updated.providerId) {
          if (field === 'isDefault' && updated.isDefault) {
            return {
              ...p,
              models: p.models.map(m =>
                m.capability === updated.capability && m.isDefault ? { ...m, isDefault: false } : m,
              ),
            }
          }
          return p
        }
        return {
          ...p,
          models: p.models.map(m => {
            if (m.id === updated.id) return updated
            if (field === 'isDefault' && updated.isDefault && m.capability === updated.capability) {
              return { ...m, isDefault: false }
            }
            return m
          }),
        }
      })
    } catch (e: any) { error.value = e.message }
  }
  async function setDefaultModel(capability: 'text' | 'image' | 'video', modelId: number | null) {
    if (!modelId) return
    const model = providers.value.flatMap(p => p.models).find(m => m.id === modelId && m.capability === capability)
    if (!model || model.isDefault) return
    await toggleModel(model, 'isDefault')
  }
  function settingsModelLabel(m: AIModel) {
    return `${providerName(m.providerId)} · ${m.name}`
  }
  async function testProvider(provider: Provider) {
    testing.value = provider.id; error.value = ''
    try { await api(`/settings/providers/${provider.id}/test`, { method: 'POST' }); alert('连接成功') }
    catch (e: any) { error.value = e.message } finally { testing.value = null }
  }
  function openSettings(tab: 'providers' | 'download' | 'trash' = 'providers') {
    settingsTab.value = tab
    view.value = 'settings'
    active.value = null
    activeEpisode.value = null
    projectMenuOpen.value = false
    loadProviders()
    if (tab === 'trash') loadTrash()
    navigateTo(settingsPath(tab))
  }

  function openTts() {
    view.value = 'tts'
    active.value = null
    activeEpisode.value = null
    projectMenuOpen.value = false
    navigateTo('/tts')
  }

  async function applyRoute() {
    if (applyingRoute) return
    const name = route.name
    if (name === 'settings' || name === 'settings-download' || name === 'settings-trash') {
      const tab = name === 'settings-trash' ? 'trash' : name === 'settings-download' ? 'download' : 'providers'
      settingsTab.value = tab
      view.value = 'settings'
      active.value = null
      activeEpisode.value = null
      projectMenuOpen.value = false
      loadProviders()
      if (tab === 'trash') loadTrash()
      return
    }
    if (name === 'tts') {
      view.value = 'tts'
      active.value = null
      activeEpisode.value = null
      projectMenuOpen.value = false
      return
    }
    if (name === 'home') {
      view.value = 'studio'
      active.value = null
      activeEpisode.value = null
      projectMenuOpen.value = false
      return
    }
    if (name === 'project' || name === 'project-episode' || name === 'project-resources') {
      const id = Number(route.params.id)
      if (!Number.isFinite(id) || id <= 0) {
        await goHome()
        return
      }
      const tab = name === 'project-resources' ? 'resources' as const : name === 'project-episode' ? 'episodes' as const : 'scripts' as const
      const episodeNumber = name === 'project-episode'
        ? Number(route.params.episodeNumber)
        : undefined
      const validEpisode = episodeNumber != null && Number.isFinite(episodeNumber) && episodeNumber > 0
        ? episodeNumber
        : undefined
      try {
        applyingRoute = true
        try {
          // Same hydrated project: only sync tab/episode. Avoid full re-fetch on tab switches.
          if (active.value?.id === id && view.value === 'studio' && projectHydrated.value) {
            studioTab.value = tab
            if (validEpisode != null && activeEpisode.value?.number !== validEpisode) {
              selectEpisodeByNumber(validEpisode)
            } else if (!activeEpisode.value) {
              selectEpisodeByNumber(validEpisode)
            }
          } else {
            if (!active.value || active.value.id !== id) {
              active.value = projectShell(id)
              activeEpisode.value = null
              projectHydrated.value = false
            }
            await hydrateProject(id, { tab, episodeNumber: validEpisode })
          }
        } finally {
          applyingRoute = false
        }
        if (tab === 'episodes' && validEpisode != null && activeEpisode.value?.number !== validEpisode) {
          await navigateTo(studioPath({ tab, episodeNumber: validEpisode }))
        }
      } catch {
        error.value = '项目不存在或无法打开'
        await goHome()
      }
    }
  }
  function statusLabel(status: string) {
    return ({ draft: '待生成', generating: '生成中', done: '已完成', error: '失败' } as Record<string, string>)[status] || status
  }
  function shotUiStatus(shot: Shot) {
    return generating.value === shot.id ? 'generating' : shot.status
  }
  function shotUiStatusLabel(shot: Shot, videoTag = false) {
    if (generating.value === shot.id) return '生成中'
    if (videoTag && shot.status === 'done') return '已就绪'
    return statusLabel(shot.status)
  }

  function stopShotGenPoll(shotId: number) {
    const t = shotGenPollTimers.get(shotId)
    if (t != null) {
      window.clearInterval(t)
      shotGenPollTimers.delete(shotId)
    }
  }

  function startShotGenPoll(shotId: number) {
    if (shotGenPollTimers.has(shotId)) return
    const timer = window.setInterval(async () => {
      await refreshShotFromServer(shotId)
      const s = activeEpisode.value?.shots?.find(x => x.id === shotId)
      if (!s || s.status !== 'generating') {
        stopShotGenPoll(shotId)
        if (generating.value === shotId && s?.status !== 'generating') generating.value = null
      }
    }, 3000)
    shotGenPollTimers.set(shotId, timer)
  }

  function syncShotGenPolls() {
    const shots = activeEpisode.value?.shots || []
    const generatingIds = new Set(shots.filter(s => s.status === 'generating').map(s => s.id))
    for (const id of [...shotGenPollTimers.keys()]) {
      if (!generatingIds.has(id)) stopShotGenPoll(id)
    }
    for (const id of generatingIds) startShotGenPoll(id)
  }

  watch(() => resourceFilter.value, () => {
    if (!active.value) return
    if (libraryParentId.value) {
      libraryParentId.value = null
      libraryParent.value = null
    }
    void loadLibraryPage({ resetPage: true })
  })
  let resourceQueryTimer: number | null = null
  watch(() => resourceQuery.value, () => {
    if (!active.value) return
    if (resourceQueryTimer != null) window.clearTimeout(resourceQueryTimer)
    resourceQueryTimer = window.setTimeout(() => {
      if (resourceLibraryTab.value === 'trash') void loadResourceTrash({ resetPage: true })
      else void loadLibraryPage({ resetPage: true })
    }, 280)
  })
  watch(resourcePage, () => {
    if (suppressResourcePageWatch || !active.value || !libraryReady.value) return
    void loadLibraryPage()
  })
  watch(trashPage, () => {
    if (suppressTrashPageWatch || !active.value || resourceLibraryTab.value !== 'trash') return
    void loadResourceTrash()
  })
  watch(shotPage, (page) => {
    if (suppressShotPageWatch || !active.value || !activeEpisode.value?.id) return
    void loadShotPage(page, { force: true })
  })
  watch(() => activeEpisode.value?.id, (id, prev) => {
    if (prev == null || id == null || id === prev) return
    clearAllShotDirty()
    suppressShotPageWatch = true
    shotPage.value = 1
    suppressShotPageWatch = false
    void loadShotPage(1, { force: true })
  })

  watch(() => (activeEpisode.value?.shots || []).map(s => `${s.id}:${s.status}`).join('|'), () => {
    syncShotGenPolls()
  })

  onMounted(async () => {
    loadProviders()
    await load()
    await applyRoute()
    syncShotGenPolls()
    startStudioSyncLoop()
    document.addEventListener('visibilitychange', onStudioVisibilityChange)
    studioSyncChannel?.addEventListener('message', onStudioSyncMessage)
  })

  onUnmounted(() => {
    stopStudioSyncLoop()
    document.removeEventListener('visibilitychange', onStudioVisibilityChange)
    studioSyncChannel?.removeEventListener('message', onStudioSyncMessage)
    try { studioSyncChannel?.close() } catch { /* ignore */ }
    stopCrewPoll()
  })

  // Ignore query-only changes (e.g. 分镜 ?page=) — those used to re-enter applyRoute and snap back to 分镜.
  watch(
    () => [String(route.name || ''), String(route.params.id || ''), String(route.params.episodeNumber || '')].join('|'),
    () => {
      if (!applyingRoute) void applyRoute()
    },
  )

  watch([studioTab, () => activeEpisode.value?.number], () => {
    if (applyingRoute || !active.value || view.value !== 'studio') return
    void navigateTo(studioPath())
  })

  watch(settingsTab, tab => {
    if (tab === 'trash' && view.value === 'settings') loadTrash()
    if (view.value === 'settings') navigateTo(settingsPath(tab))
  })

  function stopCrewPoll() {
    crewPollToken++
    if (crewPollTimer) {
      clearTimeout(crewPollTimer)
      crewPollTimer = null
    }
  }

  function applyCrewPayload(data: any, prev: CrewJob | null) {
    const job = data?.job as CrewJob | null
    crewJob.value = job || null
    if (typeof data?.shotCount === 'number' && activeEpisode.value) {
      shotTotal.value = data.shotCount
    }
    if (job?.scriptDraft && activeEpisode.value && activeEpisode.value.id === job.episodeId) {
      activeEpisode.value.script = job.scriptDraft
    }
    if (job?.directorPlan && activeEpisode.value && activeEpisode.value.id === job.episodeId) {
      activeEpisode.value.directorPlan = job.directorPlan
    }
    for (const img of job?.imageJobs || []) {
      upsertImageJobFromApi(img)
    }
    const justFinished = prev?.status === 'running' && job && job.status !== 'running'
    if (justFinished && job.assets && activeEpisode.value && activeEpisode.value.id === job.episodeId) {
      activeEpisode.value.assets = job.assets
      activeEpisode.value.crewStatus = job.status
      activeEpisode.value.crewStage = job.stage
      syncEpisode()
    }
    if (justFinished && job.stage === 'assets') {
      void loadLibraryPage({ resetPage: true })
    }
    if (justFinished && (job.stage === 'storyboard' || job.stage === 'qc')) {
      clearAllShotDirty()
      void loadShotPage(shotPage.value, { force: true })
    }
  }

  async function loadCrewJob() {
    if (!activeEpisode.value?.id) {
      crewJob.value = null
      return
    }
    const data = await api(`/episodes/${activeEpisode.value.id}/crew`)
    applyCrewPayload(data, crewJob.value)
  }

  async function sendCrewChat(text: string, thinkingLevel = 'off') {
    if (!activeEpisode.value?.id) return
    const value = text.trim()
    if (!value || crewChatBusy.value) return
    if (crewJob.value?.status === 'running' || crewBusy.value) {
      ElMessage.warning('剧组向导任务进行中，请稍后再聊')
      return
    }
    crewChatBusy.value = true
    error.value = ''
    try {
      const data = await api(`/episodes/${activeEpisode.value.id}/crew/chat`, {
        method: 'POST',
        body: JSON.stringify({ text: value, thinkingLevel }),
      })
      applyCrewPayload(data, crewJob.value)
      if (data?.job?.stage === 'storyboard' || data?.job?.stage === 'qc') {
        await loadShotPage(1, { force: true })
      }
    } catch (e: any) {
      error.value = e.message || '剧组聊天失败'
      ElMessage.error(error.value)
    } finally {
      crewChatBusy.value = false
    }
  }

  async function reconnectCrewChat() {
    await loadCrewJob()
  }

  async function clearCrewMemory(scope: 'messages' | 'summary' | 'all') {
    if (!activeEpisode.value?.id || crewChatBusy.value || crewBusy.value) return
    crewChatBusy.value = true
    try {
      const data = await api(`/episodes/${activeEpisode.value.id}/crew/memory`, {
        method: 'DELETE',
        body: JSON.stringify({ scope }),
      })
      applyCrewPayload(data, crewJob.value)
    } finally {
      crewChatBusy.value = false
    }
  }

  async function pollCrewJob() {
    const token = ++crewPollToken
    const tick = async () => {
      if (token !== crewPollToken) return
      try {
        await loadCrewJob()
      } catch (e: any) {
        error.value = e.message || '读取剧组任务失败'
      }
      if (token !== crewPollToken) return
      if (crewJob.value?.status === 'running') {
        crewPollTimer = setTimeout(tick, 2000)
      }
    }
    await tick()
  }

  watch(
    () => activeEpisode.value?.id,
    (id) => {
      stopCrewPoll()
      crewShotConflict.value = 0
      if (!id) {
        crewJob.value = null
        return
      }
      void loadCrewJob()
        .then(() => {
          if (crewJob.value?.status === 'running') void pollCrewJob()
        })
        .catch(() => { crewJob.value = null })
    },
  )

  async function saveEpisodeScript(script?: string) {
    if (!activeEpisode.value) return
    const next = script ?? activeEpisode.value.script ?? ''
    await saveEpisodeMeta({ script: next })
  }

  async function saveEpisodeMeta(patch: { script?: string; title?: string }) {
    if (!activeEpisode.value) return
    const body: Record<string, string> = {}
    if (patch.script != null) body.script = patch.script
    if (patch.title != null) body.title = patch.title
    if (!Object.keys(body).length) return
    const saved = await api(`/episodes/${activeEpisode.value.id}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    })
    if (patch.script != null) activeEpisode.value.script = saved.script ?? patch.script
    if (patch.title != null) activeEpisode.value.title = saved.title ?? patch.title
    syncEpisode()
  }

  async function refreshEpisodeMeta() {
    if (!active.value) return
    const raw = await api(`/projects/${active.value.id}`)
    const p = normalizeProject(raw)
    const current = activeEpisode.value
    active.value.episodes = p.episodes.map(ep => {
      if (current && ep.id === current.id) {
        return { ...ep, shots: current.shots, shotTotal: current.shotTotal ?? ep.shotTotal }
      }
      return ep
    })
    active.value.episodeCount = p.episodeCount
    if (current) {
      const next = active.value.episodes.find(e => e.id === current.id)
      if (next) {
        activeEpisode.value = {
          ...current,
          ...next,
          shots: current.shots,
          shotTotal: current.shotTotal ?? next.shotTotal,
        }
      }
    }
  }

  async function extractEpisodes(ids: number[]) {
    if (!active.value || extractingEpisodeIds.value.length) return
    const unique = [...new Set(ids.filter(id => id > 0))]
    const pool = unique.length
      ? unique
      : (active.value.episodes || []).map(ep => ep.id)
    const withScript = pool.filter((id) => {
      const ep = active.value!.episodes.find(e => e.id === id)
      return !!(ep?.script || '').trim()
    })
    if (!withScript.length) {
      error.value = '请先填写要提取的剧本'
      return
    }
    extractingEpisodeIds.value = withScript
    error.value = ''
    try {
      const data = await api(`/projects/${active.value.id}/crew/extract`, {
        method: 'POST',
        body: JSON.stringify({ episodeIds: withScript }),
      })
      const started: number[] = Array.isArray(data?.started) ? data.started : withScript
      extractingEpisodeIds.value = started
      const skipped = Array.isArray(data?.skipped) ? data.skipped : []
      if (skipped.length && !started.length) {
        error.value = skipped[0]?.reason || '没有可提取的剧本'
        return
      }
      const deadline = Date.now() + 8 * 60 * 1000
      while (Date.now() < deadline && extractingEpisodeIds.value.length) {
        await new Promise(resolve => setTimeout(resolve, 2000))
        await refreshEpisodeMeta()
        extractingEpisodeIds.value = started.filter((id) => {
          const ep = active.value?.episodes.find(e => e.id === id)
          return ep?.crewStatus === 'running'
        })
      }
      await refreshEpisodeMeta()
      const extracted = started.reduce((n, id) => {
        const ep = active.value?.episodes.find(e => e.id === id)
        return n + (ep?.assets?.length || 0)
      }, 0)
      if (!extracted) {
        const failed = started.some(id => active.value?.episodes.find(e => e.id === id)?.crewStatus === 'failed')
        error.value = failed ? '提取失败，请检查设置中心里的文本模型后重试' : '没有提取到角色/场景/道具'
        return
      }
      studioTab.value = 'resources'
      resourceLibraryTab.value = 'library'
      await loadLibraryPage({ resetPage: true })
      await resumeImageGenerationJobs(active.value.id)
      ElMessage.success(`已提取 ${extracted} 个资产到资源库，勾选后可生成提示词和图片`)
    } catch (e: any) {
      error.value = e.message || '提取资产失败'
    } finally {
      extractingEpisodeIds.value = []
    }
  }

  function removeEpisodes(eps: Episode[]) {
    if (!active.value) return
    const all = active.value.episodes || []
    const unique = [...new Map(eps.map(ep => [ep.id, ep])).values()]
    if (!unique.length) return
    if (unique.length >= all.length) {
      error.value = '至少保留 1 集'
      return
    }
    askConfirm({
      title: '删除剧本',
      message: `确定删除选中的 ${unique.length} 集？该集分镜会一并删除，其余分集会自动重编号。`,
      confirmText: '删除',
      danger: true,
      onConfirm: async () => {
        const keepId = activeEpisode.value && !unique.some(ep => ep.id === activeEpisode.value!.id)
          ? activeEpisode.value.id
          : null
        for (const ep of unique) {
          await api(`/episodes/${ep.id}`, { method: 'DELETE' })
        }
        const p = normalizeProject(await api(`/projects/${active.value!.id}`))
        active.value = p
        const next = keepId ? p.episodes.find(e => e.id === keepId) : p.episodes[0]
        activeEpisode.value = next ? normalizeEpisode(next) : null
        projects.value = await api('/projects')
        await navigateTo(studioPath())
      },
    })
  }

  async function focusEpisode(episodeNumber: number) {
    if (!active.value) return
    if (activeEpisode.value?.number === episodeNumber) return
    if (activeEpisode.value) {
      try { await saveEpisodeScript() } catch { /* still switch */ }
    }
    selectEpisodeByNumber(episodeNumber)
  }

  async function batchGeneratePrompts(ids: number[]) {
    if (!active.value || batchResourceBusy.value) return
    const unique = [...new Set(ids.filter(id => id > 0))]
    if (!unique.length) {
      error.value = '请先勾选要生成提示词的资源'
      return
    }
    batchResourceBusy.value = 'prompts'
    error.value = ''
    try {
      const data = await api(`/projects/${active.value.id}/resources/batch-prompts`, {
        method: 'POST',
        body: JSON.stringify({ resourceIds: unique }),
      })
      const items = Array.isArray(data?.items) ? data.items as Resource[] : []
      for (const item of items) upsertResourceInCaches(item)
      await loadLibraryPage()
      const withPrompt = items.filter(r => (r.genPrompt || '').trim()).length
      ElMessage.success(withPrompt ? `已生成 ${withPrompt} 条绘图提示词` : `已更新 ${items.length || unique.length} 条`)
    } catch (e: any) {
      error.value = e.message || '批量生成提示词失败'
    } finally {
      batchResourceBusy.value = ''
    }
  }

  async function batchGenerateImages(ids: number[]) {
    if (!active.value || batchResourceBusy.value) return
    const unique = [...new Set(ids.filter(id => id > 0))]
    if (!unique.length) {
      error.value = '请先勾选要生成图片的资源'
      return
    }
    batchResourceBusy.value = 'images'
    error.value = ''
    try {
      await api(`/projects/${active.value.id}/resources/batch-images`, {
        method: 'POST',
        body: JSON.stringify({
          resourceIds: unique,
          count: candidateCount.value || 1,
          resolution: imageResolution.value || '1k',
          modelId: effectiveImageModelId.value || undefined,
        }),
      })
      await resumeImageGenerationJobs(active.value.id)
      ElMessage.success('已开始批量生图，可在右下角任务列表查看进度')
    } catch (e: any) {
      error.value = e.message || '批量生成图片失败'
    } finally {
      batchResourceBusy.value = ''
    }
  }

  async function openCrewModal() {
    if (!activeEpisode.value) return
    crewModalOpen.value = true
    crewShotConflict.value = 0
    try {
      await loadCrewJob()
      if (crewJob.value?.status === 'running') await pollCrewJob()
    } catch (e: any) {
      error.value = e.message || '读取剧组任务失败'
    }
  }

  function closeCrewModal() {
    crewModalOpen.value = false
    crewShotConflict.value = 0
    // Running crew jobs continue in the background. StudioView exposes their
    // progress in the shared task panel and lets the user reopen this dialog.
    if (crewJob.value?.status !== 'running') stopCrewPoll()
  }

  async function startCrewPipeline() {
    if (!activeEpisode.value) return
    const script = (activeEpisode.value.script || '').trim()
    if (!script) {
      error.value = '请先粘贴本集剧本'
      episodeScriptOpen.value = true
      return
    }
    crewBusy.value = true
    crewShotConflict.value = 0
    error.value = ''
    try {
      await saveEpisodeScript(script)
      crewModalOpen.value = true
      const data = await api(`/episodes/${activeEpisode.value.id}/crew/start`, {
        method: 'POST',
        body: JSON.stringify({ script }),
      })
      applyCrewPayload(data, crewJob.value)
      await pollCrewJob()
    } catch (e: any) {
      error.value = e.message || '启动剧组失败'
    } finally {
      crewBusy.value = false
    }
  }

  async function resplitFromShot(shot: Shot) {
    if (!activeEpisode.value || !shot?.id) return
    const label = shot.label || `分镜${shot.sortOrder || ''}`
    const ok = confirm(
      `将删除「${label}」及之后的所有分镜（含已生成视频），保留前面的分镜与成片，然后按剧本从这里续拆。\n\n确定继续？`,
    )
    if (!ok) return
    crewBusy.value = true
    crewModalOpen.value = true
    error.value = ''
    try {
      const r = await fetch(`/api/episodes/${activeEpisode.value.id}/crew/resplit-from`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ fromShotId: shot.id }),
      })
      const raw = await r.text()
      let data: any = {}
      try { data = raw ? JSON.parse(raw) : {} } catch { throw new Error('后端服务不可用') }
      if (!r.ok) throw new Error(data.error || `续拆失败（HTTP ${r.status}）`)
      applyCrewPayload(data, crewJob.value)
      ElMessage.success('已开始从该镜续拆，前面分镜保持不变')
      await pollCrewJob()
      await loadShotPage(shotPage.value, { force: true })
    } catch (e: any) {
      error.value = e.message || '续拆失败'
      ElMessage.error(error.value)
    } finally {
      crewBusy.value = false
    }
  }

  async function continueCrewPipeline(opts?: { shotMode?: 'replace' | 'append'; skipImages?: boolean }) {
    if (!activeEpisode.value || !crewJob.value) return
    crewBusy.value = true
    error.value = ''
    try {
      const body: Record<string, unknown> = {}
      if (crewJob.value.stage === 'screenwriter') {
        body.script = crewJob.value.scriptDraft
      }
      if (crewJob.value.stage === 'director' || crewJob.value.stage === 'consistency') {
        body.plan = crewJob.value.directorPlan
        body.assets = (crewJob.value.assets || []).filter(a => a.name.trim())
      }
      if (opts?.shotMode) body.shotMode = opts.shotMode
      if (opts?.skipImages) body.skipImages = true
      const r = await fetch(`/api/episodes/${activeEpisode.value.id}/crew/continue`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const raw = await r.text()
      let data: any = {}
      try { data = raw ? JSON.parse(raw) : {} } catch { throw new Error('后端服务不可用') }
      if (r.status === 409) {
        crewShotConflict.value = Number(data.shotCount) || shotTotal.value
        if (data.job) crewJob.value = data.job
        ElMessage.warning(`本集已有 ${crewShotConflict.value} 个分镜，请选择替换或追加`)
        return
      }
      if (!r.ok) throw new Error(data.error || `请求失败（HTTP ${r.status}）`)
      crewShotConflict.value = 0
      applyCrewPayload(data, crewJob.value)
      await pollCrewJob()
    } catch (e: any) {
      error.value = e.message || '继续失败'
      ElMessage.error(error.value)
    } finally {
      crewBusy.value = false
    }
  }

  async function retryCrewPipeline() {
    if (!activeEpisode.value) return
    crewBusy.value = true
    error.value = ''
    try {
      const data = await api(`/episodes/${activeEpisode.value.id}/crew/retry`, { method: 'POST' })
      applyCrewPayload(data, crewJob.value)
      await pollCrewJob()
    } catch (e: any) {
      error.value = e.message || '重试失败'
    } finally {
      crewBusy.value = false
    }
  }

  async function rewindCrewPipeline(stage: string) {
    if (!activeEpisode.value || !stage) return
    crewBusy.value = true
    error.value = ''
    crewShotConflict.value = 0
    try {
      const data = await api(`/episodes/${activeEpisode.value.id}/crew/rewind`, {
        method: 'POST',
        body: JSON.stringify({ stage }),
      })
      applyCrewPayload(data, crewJob.value)
    } catch (e: any) {
      error.value = e.message || '回到该步骤失败'
    } finally {
      crewBusy.value = false
    }
  }

  function updateCrewScript(script: string) {
    if (!crewJob.value) return
    crewJob.value = { ...crewJob.value, scriptDraft: script }
  }

  function updateCrewPlan(plan: string) {
    if (!crewJob.value) return
    crewJob.value = { ...crewJob.value, directorPlan: plan }
  }

  function updateCrewAssets(assets: CrewAsset[]) {
    if (!crewJob.value) return
    crewJob.value = { ...crewJob.value, assets }
  }

  async function jumpCrewShot(shotId?: number, shotIndex?: number) {
    const ids = crewJob.value?.shotIds || []
    let resolved = Number(shotId) || 0
    const index = Number(shotIndex) || 0
    if (resolved && ids.length && !ids.includes(resolved) && resolved >= 1 && resolved <= ids.length) {
      resolved = ids[resolved - 1]
    }
    if (!resolved && index > 0 && ids[index - 1]) resolved = ids[index - 1]
    if (!resolved && index > 0 && activeEpisode.value?.shots?.[index - 1]) {
      resolved = activeEpisode.value.shots[index - 1].id
    }
    if (!resolved) {
      ElMessage.warning('这条问题没有对应分镜')
      return
    }

    let sortOrder = index
    const local = activeEpisode.value?.shots?.find(s => s.id === resolved)
    if (local) {
      sortOrder = local.sortOrder
    } else {
      try {
        const raw = await api(`/shots/${resolved}`)
        const shot = normalizeShot(raw)
        if (activeEpisode.value?.id && shot.episodeId && shot.episodeId !== activeEpisode.value.id) {
          ElMessage.warning('这条问题对应的分镜不在本集')
          return
        }
        resolved = shot.id
        sortOrder = shot.sortOrder
      } catch (e: any) {
        ElMessage.error(e.message || '无法定位分镜')
        return
      }
    }

    closeCrewModal()
    studioTab.value = 'episodes'
    const page = Math.floor(Math.max(0, (sortOrder || 1) - 1) / shotPageSize) + 1
    expandShot(resolved)
    highlightedShotId.value = resolved
    if (highlightShotTimer) clearTimeout(highlightShotTimer)
    highlightShotTimer = setTimeout(() => {
      if (highlightedShotId.value === resolved) highlightedShotId.value = 0
    }, 2800)
    await loadShotPage(page, { force: true })
    await nextTick()
    document.querySelector<HTMLElement>(`[data-shot-id="${resolved}"]`)?.scrollIntoView({
      behavior: 'smooth',
      block: 'center',
    })
  }

  async function fixCrewPipeline(issues: CrewQCIssue[]) {
    if (!activeEpisode.value || !crewJob.value) return
    if (!issues.length) {
      ElMessage.warning('请先勾选要修改的问题')
      return
    }
    crewBusy.value = true
    error.value = ''
    try {
      const data = await api(`/episodes/${activeEpisode.value.id}/crew/fix`, {
        method: 'POST',
        body: JSON.stringify({ issues }),
      })
      applyCrewPayload(data, crewJob.value)
      await pollCrewJob()
      const leftover = crewJob.value?.qc?.issues?.length || 0
      if (crewJob.value?.status !== 'failed') {
        ElMessage.success(leftover ? `已按建议改稿并复检，还剩 ${leftover} 项` : '已按建议改稿，质检通过')
      }
    } catch (e: any) {
      error.value = e.message || '按建议修改失败'
      ElMessage.error(error.value)
    } finally {
      crewBusy.value = false
    }
  }

  return {
    logoDark,
    defaultStyle,
    scriptPlaceholder,
    resolutionOptions,
    maxShotRefs,
    maxSceneReferences,
    maxPositioningRefs,
    maxMotionGridRefs,
    maxSceneGridRefs,
    maxSceneReverseRefs,
    projects,
    trashProjects,
    active,
    activeEpisode,
    providers,
    providerKeys,
    shownKeys,
    view,
    settingsTab,
    studioTab,
    resourceFilter,
    resourceQuery,
    resourceLibraryTab,
    resourceTrash,
    showAddForm,
    loading,
    projectLoading,
    providersLoading,
    saving,
    generating,
    uploadingShot,
    uploadingShotRef,
    applyingShotVideo,
    testing,
    error,
    form,
    resourceForm,
    regenerateResourceId,
    baseGenPrompt,
    promptRevision,
    isRegeneratingResource,
    isAddingDerivative,
    canHaveDerivatives,
    sceneReferences,
    sceneRefPickerOpen,
    refPickerReferences,
    refPickerMax,
    refPickerReplaceHint,
    positioningModal,
	positioningRefs,
	positioningReplaceIndex,
	positioningPickingSkeleton,
    motionGridModal,
    motionGridRefs,
    motionGridReplaceIndex,
    motionGridAnchor,
    sceneGridModal,
    sceneGridRefs,
    sceneGridReplaceIndex,
    sceneReverseModal,
    sceneReverseRefs,
    sceneReverseReplaceIndex,
    sceneReverseGridChoices,
    selectSceneReverseGrid,
    scenePanoramaModal,
    scenePanoramaRefs,
    maxScenePanoramaRefs,
    videoFiles,
    uploadingVideos,
    characterCandidates,
    selectedCandidate,
    generatingCharacter,
    lastCharacterPrompt,
    generatingScene,
    generatingProp,
    lastScenePrompt,
    lastPropPrompt,
    candidateCount,
    imageQuality,
    imageQualityOptions,
    imageResolution,
    imageResolutionOptions,
    imageModelId,
    imageModels,
    imageModelsByProvider,
    textModels,
    defaultTextModelId,
    defaultImageModelId,
    defaultImageModel,
    defaultVideoModelId,
    effectiveImageModelId,
    defaultImageModelLabel,
    imageModelLabel,
    imageGenProgress,
    imageGenJobs,
    visibleImageGenJobs,
    focusedImageJobId,
    submittingImageGen,
    hasReadyImageJob,
    dismissImageJobFromPanel,
    picker,
    pickerReplaceIndex,
    promptPreview,
    previewingPrompt,
    optimizingScripts,
    matchingShotRefs,
    extractingFrame,
    shotScriptEpoch,
    stylizingResources,
    stylizeJobs,
    isStylizingResource,
    dismissStylizeJob,
    focusStylizeJob,
    showAdvanced,
    confirmModal,
    confirmLoading,
    modelForm,
    modelFormSaving,
    CHARACTER_STYLIZE_PROMPT,
    SCENE_STYLIZE_PROMPT,
    stylizeModal,
    editResourceModal,
    updatingResource,
    DOUBAO_WEB_VIDEO_PRESETS,
    projectMenuOpen,
    imagePreview,
    panoramaViewer,
    directorDeskModal,
    characters,
    scenes,
    hasSceneReference,
    img2imgRefLabel,
    img2imgRefHint,
    sceneRefPickerTitle,
    sceneRefPickerHint,
    props,
    videoResources,
    resourceCounts,
    managedResourceDisplay,
    resourcePage,
    resourcePageSize,
    libraryTotal,
    libraryLoading,
    loadLibraryPage,
    libraryParentId,
    libraryParent,
    openResourceDerives,
    closeResourceDerives,
    managedResourceTrash,
    resourceTrashCounts,
    trashPage,
    trashPageSize,
    trashTotal,
    trashLoading,
    applyingPrimary,
    candidatesPersisted,
    showCreateButton,
    createResourceLabel,
    videoModels,
    defaultVideoModelLabel,
    pickerPrimaryCharacters,
    pickerPrimaryScenes,
    pickerFlatScenes,
    sceneGridsFor,
    sceneGridsMap,
    gridCellsFor,
    gridIsSplit,
    loadGridCells,
    sceneGridAngleOf,
    pickerPrimaryProps,
    pickerPrimaryOthers,
    recentShotRefs,
    aiGenerateLabel,
    parseCandidateName,
    existingLibraryBase,
    load,
    loadTrash,
    askConfirm,
    closeConfirm,
    runConfirm,
    deleteProject,
    restoreProject,
    purgeProject,
    openTrash,
    deleteActiveProject,
    addEpisode,
    addingEpisode,
    removeEpisode,
    goToEpisode,
    loadProviders,
    normalizeShot,
    normalizeEpisode,
    normalizeProject,
    openProject,
    goHome,
    create,
    saveProject,
    refreshStudio,
    refreshingStudio,
    selectEpisodeByNumber,
    addShot,
    moveShot,
    markShotDirty,
    isShotDirty,
    beginShotEditSession,
    revertShotEdits,
    reloadShotFromServer,
    shotTotal,
    shotPage,
    shotPageSize,
    shotPageLoading,
    loadShotPage,
    isShotExpanded,
    highlightedShotId,
    toggleShotExpand,
    shotSummary,
    shotTimeLabel,
    shotVideoMeta,
    shotVideoScript,
    saveShot,
    removeShot,
    generateShot,
    onShotVideoFile,
    previewShotPrompt,
    optimizeShotScript,
    rematchShotRefs,
    extractPreviousFrame,
    closePromptPreview,
    refKindLabel,
    replaceShot,
    shotRefKey,
    refThumb,
    refLabel,
    refDisplayName,
    refTag,
    removeShotRef,
    renameShotRef,
    updateShotRefLabel,
    seedShotRefLabelIfEmpty,
    sceneImage,
    characterImage,
    otherImage,
    pickerShot,
    openPicker,
    openReplacePicker,
    closePicker,
    pickerRefSelected,
    pickerRefDisabled,
    pickCharacterRef,
    pickSceneRef,
    pickPropRef,
    pickOtherRef,
    pickRecentRef,
    onShotRefFiles,
    onShotComposerPaste,
    pickerResourceName,
    usePrimaryResource,
    loadResourceTrash,
    toggleAddResourceForm,
    openResourceGenerateJob,
    isAnyResourceGenerating,
    openResourceLibrary,
    openResourceTrash,
    purgeResourceTrash,
    toggleAdvanced,
    shotVideoSrc,
    shotVideoVersions,
    shotActiveVideoResource,
    isActiveShotVideo,
    useShotVideo,
    goToShot,
    resourceDisplayName,
    resourceTypeLabel,
    resourceSourceLabel,
    resourceFormTypeLabel,
    providerName,
    videoModelLabel,
    ensureShotModel,
    sceneReferenceKey,
    isSceneRefSelected,
    isSceneRefDisabled,
    removeSceneReference,
    clearSceneReferences,
    removePositioningReference,
    clearPositioningReferences,
    renamePositioningRef,
    updatePositioningRefLabel,
    onSceneRefFiles,
    onSceneRefPaste,
	onPositioningRefFiles,
	onPositioningSkeletonFile,
    onPositioningRefPaste,
    onResourceFormPaste,
    onResourceFile,
    clearResourceImage,
    openSceneRefPicker,
    openPositioningRefPicker,
	openPositioningReplacePicker,
	openPositioningSkeletonPicker,
    closeSceneRefPicker,
    openPositioningModal,
    closePositioningModal,
    setPositioningPromptBody,
    positioningPromptBody,
    positioningPromptLegendLines,
    reanalyzePositioningPrompt,
    confirmPositioningSkeleton,
    positioningSkeletonSceneRef,
    confirmPositioningGenerate,
    openMotionGridModal,
    closeMotionGridModal,
    setMotionGridPromptBody,
    motionGridPromptBody,
    reanalyzeMotionGridPrompt,
    confirmMotionGridGenerate,
    renameMotionGridRef,
    updateMotionGridRefLabel,
    removeMotionGridReference,
    clearMotionGridReferences,
    openMotionGridRefPicker,
    openMotionGridReplacePicker,
    onMotionGridRefFiles,
    onMotionGridRefPaste,
    openSceneGridModal,
    closeSceneGridModal,
    confirmSceneGridGenerate,
    generateSceneGridOverhead,
    analyzeSceneGridShapeLegend,
    reanalyzeSceneGridShapeLegend,
    onSceneGridOverheadFile,
    openSceneGridOverheadPicker,
    clearSceneGridOverhead,
    refillSceneGridPrompt,
    isNanoBananaImageModel,
    effectiveImageModel,
    splittingGridIds,
    splitGridResource,
    splittingPanoramaIds,
    splitPanoramaResource,
    removeSceneGridReference,
    clearSceneGridReferences,
    openSceneGridRefPicker,
    openSceneGridReplacePicker,
    onSceneGridRefFiles,
    onSceneGridRefPaste,
    openSceneReverseModal,
    closeSceneReverseModal,
    confirmSceneReverseSkeleton,
    confirmSceneReverseGenerate,
    refillSceneReversePrompt,
    removeSceneReverseReference,
    clearSceneReverseReferences,
    openSceneReverseRefPicker,
    openSceneReverseSkeletonPicker,
    onSceneReverseSkeletonFile,
    onSceneReverseSkeletonPaste,
    openSceneReverseReplacePicker,
    onSceneReverseRefFiles,
    onSceneReverseRefPaste,
    openScenePanoramaModal,
    closeScenePanoramaModal,
    confirmScenePanoramaGenerate,
    refillScenePanoramaPrompt,
    removeScenePanoramaReference,
    clearScenePanoramaReferences,
    onScenePanoramaRefFiles,
    scenePanoramaGridChoices,
    selectScenePanoramaGrid,
    pickSceneCharacterRef,
    pickSceneSceneRef,
    pickScenePropRef,
    pickSceneOtherRef,
    pickSceneRecentRef,
    onVideoFiles,
    submitResourceForm,
    generateCharacterImages,
    generateSceneImages,
    generatePropImages,
    switchResourceType,
    selectCandidate,
    openImagePreview,
    closeImagePreview,
    openPanoramaViewer,
    closePanoramaViewer,
    openDirectorDeskForPositioning,
    openDirectorDeskBrowse,
    closeDirectorDesk,
    onDirectorDeskReady,
    onDirectorDeskCaptures,
    previewSelectCandidate,
    createResource,
    stylizeResource,
    openStylizeModal,
    closeStylizeModal,
    confirmStylize,
    openEditResourceModal,
    canRegenerateResource,
    openRegenerateResource,
    closeEditResourceModal,
    confirmEditResource,
    exportVideoResource,
    exportVideoResources,
    exportAllLibraryVideos,
    exportShotVideo,
    videoExportFilename,
    defaultStylizePrompt,
    deleteResource,
    saveProvider,
    toggleKey,
    openAddModel,
    openEditModel,
    closeModelForm,
    onModelPresetChange,
    saveModelForm,
    toggleModel,
    setDefaultModel,
    settingsModelLabel,
    testProvider,
    openSettings,
    openTts,
    shotUiStatus,
    shotUiStatusLabel,
    crewModalOpen,
    crewJob,
    crewBusy,
    crewChatBusy,
    sendCrewChat,
    reconnectCrewChat,
    clearCrewMemory,
    crewShotConflict,
    episodeScriptOpen,
    openCrewModal,
    closeCrewModal,
    startCrewPipeline,
    continueCrewPipeline,
    resplitFromShot,
    retryCrewPipeline,
    rewindCrewPipeline,
    updateCrewScript,
    updateCrewPlan,
    updateCrewAssets,
    jumpCrewShot,
    fixCrewPipeline,
    saveEpisodeScript,
    saveEpisodeMeta,
    extractEpisodes,
    extractingEpisodeIds,
    selectedAssetIds,
    batchResourceBusy,
    batchGeneratePrompts,
    batchGenerateImages,
    removeEpisodes,
    focusEpisode,
    refreshEpisodeMeta,
  }
}

export type NovalyContext = ReturnType<typeof useNovaly>

export const NovalyKey: InjectionKey<NovalyContext> = Symbol('novaly')

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Search } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useNovalyInject } from '@/composables/useNovalyInject'
import { getDownloadDirName } from '@/utils/downloadDir'
import type { Resource } from '@/types'

const {
  resourceFilter,
  resourceQuery,
  resourceLibraryTab,
  showAddForm,
  saving,
  uploadingVideos,
  resourceForm,
  baseGenPrompt,
  promptRevision,
  isRegeneratingResource,
  regenerateResourceId,
  isAddingDerivative,
  canHaveDerivatives,
  sceneReferences,
  hasSceneReference,
  img2imgRefLabel,
  img2imgRefHint,
  videoFiles,
  characterCandidates,
  selectedCandidate,
  lastCharacterPrompt,
  lastScenePrompt,
  lastPropPrompt,
  candidateCount,
  imageModelId,
  imageModelsByProvider,
  defaultImageModelLabel,
  imageModelLabel,
  imageResolution,
  imageResolutionOptions,
  imageGenProgress,
  imageGenJobs,
  submittingImageGen,
  isAnyResourceGenerating,
  isStylizingResource,
  applyingShotVideo,
  applyingPrimary,
  managedResourceDisplay,
  resourcePage,
  resourcePageSize,
  libraryTotal,
  libraryLoading,
  libraryParentId,
  libraryParent,
  openResourceDerives,
  closeResourceDerives,
  managedResourceTrash,
  resourceCounts,
  resourceTrashCounts,
  trashPage,
  trashPageSize,
  trashTotal,
  trashLoading,
  candidatesPersisted,
  showCreateButton,
  createResourceLabel,
  toggleAddResourceForm,
  openResourceLibrary,
  openResourceTrash,
  resourceFormTypeLabel,
  switchResourceType,
  onSceneRefFiles,
  onSceneRefPaste,
  onResourceFormPaste,
  openSceneRefPicker,
  sceneReferenceKey,
  removeSceneReference,
  clearSceneReferences,
  aiGenerateLabel,
  generateCharacterImages,
  generateSceneImages,
  generatePropImages,
  onVideoFiles,
  onResourceFile,
  clearResourceImage,
  submitResourceForm,
  selectCandidate,
  openImagePreview,
  resourceTypeLabel,
  resourceSourceLabel,
  resourceDisplayName,
  parseCandidateName,
  existingLibraryBase,
  isActiveShotVideo,
  goToShot,
  useShotVideo,
  stylizeResource,
  openStylizeModal,
  deleteResource,
  openEditResourceModal,
  canRegenerateResource,
  openRegenerateResource,
  exportVideoResource,
  exportVideoResources,
  exportAllLibraryVideos,
  usePrimaryResource,
  purgeResourceTrash,
  openSceneGridModal,
  openSceneReverseModal,
  openScenePanoramaModal,
  openPanoramaViewer,
  openDirectorDeskBrowse,
  splitGridResource,
  splitPanoramaResource,
  splittingGridIds,
  splittingPanoramaIds,
  gridIsSplit,
  gridCellsFor,
  loadGridCells,
  sceneGridAngleOf,
  selectedAssetIds,
  batchResourceBusy,
  batchGeneratePrompts,
  batchGenerateImages,
} = useNovalyInject()

function gridBadgeLabel(r: Resource): string {
  if (r.genType === 'scene_grid') return '9宫格'
  if (r.genType === 'motion_grid') return '9帧图'
  if (r.genType === 'scene_reverse') return '反打'
  if (r.genType === 'scene_reverse_skeleton') return '反打骨架'
  if (r.genType === 'scene_panorama') return '全景'
  if (r.genType === 'scene_panorama_view' && r.gridCell) return `机位${r.gridCell}`
  if (r.genType === 'motion_grid_cell' && r.gridCell) return `帧${r.gridCell}`
  if (r.genType === 'scene_grid_cell' && r.gridCell) return `格${r.gridCell}`
  return ''
}

const selectableAssets = computed(() =>
  managedResourceDisplay.value
    .map(e => e.resource)
    .filter(r => r.type === 'character' || r.type === 'scene' || r.type === 'prop'),
)

const selectedAssets = computed(() =>
  selectableAssets.value.filter(r => selectedAssetIds.value.includes(r.id)),
)

const allAssetsSelected = computed(() =>
  selectableAssets.value.length > 0
  && selectableAssets.value.every(r => selectedAssetIds.value.includes(r.id)),
)

function isSelectableAsset(item: Resource) {
  return item.type === 'character' || item.type === 'scene' || item.type === 'prop'
}

function toggleAssetSelect(item: Resource, checked: boolean) {
  if (!isSelectableAsset(item)) return
  if (checked) {
    if (!selectedAssetIds.value.includes(item.id)) {
      selectedAssetIds.value = [...selectedAssetIds.value, item.id]
    }
    return
  }
  selectedAssetIds.value = selectedAssetIds.value.filter(id => id !== item.id)
}

function toggleSelectAllAssets(checked: string | number | boolean) {
  selectedAssetIds.value = checked ? selectableAssets.value.map(r => r.id) : []
}

watch([resourceFilter, resourceLibraryTab, managedResourceDisplay], () => {
  const visible = new Set(selectableAssets.value.map(r => r.id))
  selectedAssetIds.value = selectedAssetIds.value.filter(id => visible.has(id))
})

function canBuildSceneGrid(r: Resource): boolean {
  if (r.type !== 'scene' || r.deletedAt || r.gridId) return false
  const g = r.genType || ''
  return !['scene_grid', 'scene_grid_cell', 'scene_reverse', 'scene_reverse_skeleton', 'scene_panorama', 'scene_panorama_view', 'positioning', 'positioning_skeleton', 'motion_grid', 'motion_grid_cell'].includes(g)
}

function canBuildSceneReverse(r: Resource): boolean {
  if (r.type !== 'scene' || r.deletedAt || r.gridId) return false
  if (!(r.imageUrl || r.stylizedImageUrl)) return false
  const g = r.genType || ''
  return !['scene_grid', 'scene_grid_cell', 'scene_reverse', 'scene_reverse_skeleton', 'scene_panorama', 'scene_panorama_view', 'positioning', 'positioning_skeleton', 'motion_grid', 'motion_grid_cell'].includes(g)
}

function canBuildScenePanorama(r: Resource): boolean {
  if (r.type !== 'scene' || r.deletedAt || r.gridId) return false
  if (!(r.imageUrl || r.stylizedImageUrl)) return false
  const g = r.genType || ''
  return !['scene_grid', 'scene_grid_cell', 'scene_reverse', 'scene_reverse_skeleton', 'scene_panorama', 'scene_panorama_view', 'positioning', 'positioning_skeleton', 'motion_grid', 'motion_grid_cell'].includes(g)
}

function canSplitGrid(r: Resource): boolean {
  return (r.genType === 'scene_grid' || r.genType === 'motion_grid') && !r.deletedAt
}

function canSplitPanorama(r: Resource): boolean {
  return r.genType === 'scene_panorama' && !r.deletedAt && !!(r.imageUrl || r.stylizedImageUrl)
}

function gridSplitDone(r: Resource): boolean {
  return canSplitGrid(r) && gridIsSplit(r.id)
}

function panoramaSplitDone(r: Resource): boolean {
  return canSplitPanorama(r) && gridIsSplit(r.id)
}

const expandedPromptIds = ref<number[]>([])

function drawingPrompt(item: Resource) {
  return (item.genPrompt || '').trim()
}

function isPromptExpanded(id: number) {
  return expandedPromptIds.value.includes(id)
}

function togglePromptExpand(id: number) {
  expandedPromptIds.value = isPromptExpanded(id)
    ? expandedPromptIds.value.filter(x => x !== id)
    : [...expandedPromptIds.value, id]
}

function onResourceMoreCommand(resource: Resource, command: string) {
  switch (command) {
    case 'scene-grid':
      openSceneGridModal(resource)
      break
    case 'scene-reverse':
      openSceneReverseModal(resource)
      break
    case 'scene-panorama':
      openScenePanoramaModal(resource)
      break
    case 'split-grid':
      void splitGridResource(resource)
      break
    case 'split-panorama':
      void splitPanoramaResource(resource)
      break
    case 'pano-view':
      openPanoramaViewer(resource.imageUrl || resource.stylizedImageUrl || '', resourceDisplayName(resource))
      break
    case 'desk-browse':
      void openDirectorDeskBrowse(resource)
      break
    case 'grid-viewer':
      void openGridViewer(resource)
      break
    case 'regenerate':
      openRegenerateResource(resource)
      break
    case 'export-video':
      void exportOneVideo(resource)
      break
    case 'go-shot':
      goToShot(resource)
      break
    case 'use-shot-video':
      void useShotVideo(resource)
      break
    case 'stylize':
      openStylizeModal(resource)
      break
    case 'delete':
      void deleteResource(resource)
      break
  }
}

const gridViewer = ref<Resource | null>(null)
const gridViewerLoading = ref(false)
const loadedGridCellIds = ref<Set<number>>(new Set())
const gridViewerCells = computed(() =>
  gridViewer.value ? gridCellsFor(gridViewer.value.id) : [],
)

async function openGridViewer(resource: Resource) {
  gridViewer.value = resource
  gridViewerLoading.value = true
  loadedGridCellIds.value = new Set()
  try {
    await loadGridCells(resource.id)
  } finally {
    gridViewerLoading.value = false
  }
}

function markGridCellLoaded(id: number) {
  const next = new Set(loadedGridCellIds.value)
  next.add(id)
  loadedGridCellIds.value = next
}

async function splitGridInViewer() {
  const resource = gridViewer.value
  if (!resource) return
  gridViewerLoading.value = true
  loadedGridCellIds.value = new Set()
  try {
    await splitGridResource(resource)
    await loadGridCells(resource.id)
  } finally {
    gridViewerLoading.value = false
  }
}

function openResourceImage(resource: Resource) {
  if (!resource.parentId && (resource.deriveCount || 0) > 0 && !resource.imageUrl) {
    openResourceDerives(resource)
    return
  }
  if (resource.genType === 'scene_grid') {
    void openGridViewer(resource)
    return
  }
  if (resource.genType === 'scene_panorama' && resource.imageUrl) {
    openPanoramaViewer(resource.imageUrl, resourceDisplayName(resource))
    return
  }
  if (resource.imageUrl) openImagePreview(resource.imageUrl, resourceDisplayName(resource))
}

function openBaseOrPreview(resource: Resource) {
  if (!resource.parentId && (resource.deriveCount || 0) > 0) {
    openResourceDerives(resource)
    return
  }
  openResourceImage(resource)
}

const selectedVideoIds = ref<number[]>([])
const exportingVideos = ref(false)
const exportMode = ref<'selected' | 'all' | null>(null)
const exportProgressLabel = ref('')
const exportDialogOpen = ref(false)
const exportPercent = ref(0)
const exportPhaseText = ref('')
const exportCurrentFile = ref('')
const exportDestHint = ref('')

onMounted(async () => {
  const name = await getDownloadDirName()
  exportDestHint.value = name
    ? `优先直连 COS 并行下载，保存到目录：${name}`
    : '优先直连 COS 并行下载，保存到浏览器默认下载文件夹（可在设置中心指定目录）'
})

function applyExportProgress(p: {
  phase: string
  done: number
  total: number
  current?: string
  message?: string
}) {
  exportPhaseText.value = p.message || ''
  exportCurrentFile.value = p.current || ''
  if (p.phase === 'pack') {
    exportPercent.value = Math.max(0, Math.min(100, p.done))
  } else if (p.total > 0) {
    // list/download: show completed ratio; during item i of N use i/N until last finishes
    const ratio = p.phase === 'download'
      ? Math.min(p.done + 0.15, p.total) / p.total
      : p.done / p.total
    exportPercent.value = Math.max(0, Math.min(99, Math.round(ratio * 100)))
  } else if (p.phase === 'list') {
    exportPercent.value = 2
  }
  if (p.phase === 'save') exportPercent.value = 99
  if (p.phase === 'done') exportPercent.value = 100
  exportProgressLabel.value = p.message || exportProgressLabel.value
}

function describeExportResult(result: {
  count: number
  filename: string
  destination: 'dir' | 'default'
  dirName: string | null
  skipped?: number
} | null) {
  if (!result) return
  const isFolder = !result.filename.toLowerCase().endsWith('.zip')
  const skip = result.skipped ? `，跳过 ${result.skipped} 个失败项` : ''
  if (result.destination === 'dir' && result.dirName) {
    const where = isFolder
      ? `目录「${result.dirName}」下的文件夹「${result.filename}」`
      : `目录「${result.dirName}」`
    ElMessage.success(
      isFolder
        ? `已导出 ${result.count} 个视频到${where}${skip}`
        : `已导出 ${result.count} 个视频到${where}${skip}：${result.filename}`,
    )
    return
  }
  if (result.filename.toLowerCase().endsWith('.zip') || result.filename.includes('分批 zip')) {
    ElMessage.success(`已导出约 ${result.count} 个视频（${result.filename}）。若浏览器拦截多文件下载，请点「允许」后继续${skip}`)
    return
  }
  ElMessage.success(`已导出 ${result.count} 个视频到浏览器默认下载文件夹${skip}：${result.filename}`)
}

const visibleVideos = computed(() =>
  managedResourceDisplay.value
    .filter(e => e.resource.type === 'video' && (e.resource.videoUrl || e.resource.id))
    .map(e => e.resource),
)

const selectedVideos = computed(() =>
  visibleVideos.value.filter(v => selectedVideoIds.value.includes(v.id)),
)

const showVideoExportBar = computed(() =>
  resourceLibraryTab.value === 'library'
  && (resourceFilter.value === 'video' || selectedVideos.value.length > 0 || resourceCounts.value.video > 0)
  && (visibleVideos.value.length > 0 || resourceCounts.value.video > 0),
)

const allVideoExportCount = computed(() => {
  if (resourceFilter.value === 'video') return libraryTotal.value
  return resourceCounts.value.video
})

const allVisibleVideosSelected = computed(() =>
  visibleVideos.value.length > 0
  && visibleVideos.value.every(v => selectedVideoIds.value.includes(v.id)),
)

watch([resourceFilter, resourceLibraryTab, managedResourceDisplay], () => {
  const visible = new Set(visibleVideos.value.map(v => v.id))
  selectedVideoIds.value = selectedVideoIds.value.filter(id => visible.has(id))
})

function toggleVideoSelect(item: Resource, checked: boolean) {
  if (checked) {
    if (!selectedVideoIds.value.includes(item.id)) {
      selectedVideoIds.value = [...selectedVideoIds.value, item.id]
    }
    return
  }
  selectedVideoIds.value = selectedVideoIds.value.filter(id => id !== item.id)
}

function toggleSelectAllVideos(checked: string | number | boolean) {
  selectedVideoIds.value = checked ? visibleVideos.value.map(v => v.id) : []
}

async function exportOneVideo(item: Resource) {
  exportingVideos.value = true
  exportMode.value = 'selected'
  exportDialogOpen.value = true
  exportPercent.value = 0
  exportPhaseText.value = '导出中…'
  exportCurrentFile.value = item.name || ''
  exportProgressLabel.value = '导出中…'
  try {
    const result = await exportVideoResource(item)
    applyExportProgress({ phase: 'done', done: 1, total: 1, message: '导出完成' })
    describeExportResult(result)
  } finally {
    exportingVideos.value = false
    exportMode.value = null
    exportProgressLabel.value = ''
    window.setTimeout(() => { exportDialogOpen.value = false }, 600)
  }
}

async function exportSelectedVideos() {
  if (!selectedVideos.value.length) return
  exportingVideos.value = true
  exportMode.value = 'selected'
  exportDialogOpen.value = true
  exportPercent.value = 0
  exportPhaseText.value = '准备导出…'
  exportCurrentFile.value = ''
  exportProgressLabel.value = '准备导出…'
  try {
    const result = await exportVideoResources(selectedVideos.value, applyExportProgress)
    describeExportResult(result)
  } finally {
    exportingVideos.value = false
    exportMode.value = null
    exportProgressLabel.value = ''
    window.setTimeout(() => { exportDialogOpen.value = false }, 800)
  }
}

async function exportAllVideos() {
  const hint = resourceQuery.value.trim()
    ? `将导出当前搜索下的全部视频（约 ${allVideoExportCount.value || '全部'} 个）。会直连对象存储分批下载（更快），浏览器若提示「允许多个下载」请点允许。继续？`
    : `将导出资源库中全部 ${allVideoExportCount.value || ''} 个视频。会直连对象存储分批下载（更快），浏览器若提示「允许多个下载」请点允许。继续？`
  if (allVideoExportCount.value > 20 && !confirm(hint)) return
  exportingVideos.value = true
  exportMode.value = 'all'
  exportDialogOpen.value = true
  exportPercent.value = 0
  exportPhaseText.value = '准备导出…'
  exportCurrentFile.value = ''
  exportProgressLabel.value = '准备导出…'
  try {
    const result = await exportAllLibraryVideos(applyExportProgress)
    describeExportResult(result)
  } finally {
    exportingVideos.value = false
    exportMode.value = null
    exportProgressLabel.value = ''
    window.setTimeout(() => { exportDialogOpen.value = false }, 800)
  }
}

const isImageType = computed(() => ['character', 'scene', 'prop'].includes(resourceForm.value.type))

const isGenerating = computed(() => submittingImageGen.value)

const localImageJobId = ref<number | null>(null)
const localImageGenProgress = computed(() => {
  if (!localImageJobId.value) return null
  const job = imageGenJobs.value.find(j => j.id === localImageJobId.value) || null
  if (!job) return null
  if (job.status !== 'pending' && job.status !== 'running') return null
  return {
    progress: job.progress,
    message: job.message,
    doneCount: job.doneCount,
    totalCount: job.totalCount,
  }
})
const localJobActive = computed(() => !!localImageGenProgress.value)
const showJobProgress = computed(() => localJobActive.value)
const otherJobsRunning = computed(() =>
  imageGenJobs.value.some(j =>
    (j.status === 'pending' || j.status === 'running')
    && j.id !== localImageJobId.value,
  ),
)
const generateButtonLabel = computed(() => {
  const prog = localImageGenProgress.value
  if (prog?.message) return prog.message
  if (localJobActive.value) {
    return `生成中（${prog?.totalCount || candidateCount.value} 张）…`
  }
  return aiGenerateLabel(resourceForm.value.type as 'character' | 'scene' | 'prop', false)
})

function bindLocalImageJobForTarget(targetId?: number) {
  if (!targetId) {
    localImageJobId.value = null
    return
  }
  const job = imageGenJobs.value.find(j => j.targetResourceId === targetId)
  localImageJobId.value = job?.id ?? null
}

const localImageCandidates = computed(() => {
  if (!localImageJobId.value) return characterCandidates.value
  const job = imageGenJobs.value.find(j => j.id === localImageJobId.value) || null
  return job?.images || []
})
const localCandidatesPersisted = computed(() => localImageCandidates.value.some(c => c.resourceId))

// Ensure "write" actions have a selected candidate even when global focused job
// didn't回填 `characterCandidates` (this happens after fixing progress cross-job).
watch(localImageCandidates, (candidates) => {
  if (!showAddForm.value) return
  if (!localImageJobId.value) return
  if (!Array.isArray(candidates) || candidates.length === 0) return
  // Keep the global "characterCandidates" in sync so createResource()
  // can find persisted candidates via (url + resourceId) and do the
  // correct merge/replace.
  if (!characterCandidates.value.length) {
    characterCandidates.value = candidates
  }
  if (selectedCandidate.value) return
  if (resourceForm.value.imageData) return
  selectCandidate(candidates[0].url)
})

watch(showAddForm, (open) => {
  if (!open) {
    localImageJobId.value = null
    return
  }
  if (isRegeneratingResource.value && regenerateResourceId.value) {
    bindLocalImageJobForTarget(regenerateResourceId.value)
  }
})

// Bind progress/candidates to the job for the resource being edited in this modal.
watch(
  () => [showAddForm.value, isRegeneratingResource.value, regenerateResourceId.value, imageGenJobs.value] as const,
  ([open, regenMode, targetId]) => {
    if (!open) return
    if (!regenMode || !targetId) {
      localImageJobId.value = null
      return
    }
    bindLocalImageJobForTarget(targetId)
  },
)

const aiPromptHint = computed(() => {
  if (resourceForm.value.type === 'character' && lastCharacterPrompt.value) {
    return hasSceneReference.value
      ? '提示词已包含：多图融合 + 三视图定妆照 + 面部特写'
      : '提示词已包含：三视图定妆照 + 面部特写 + 纯白背景'
  }
  if (resourceForm.value.type === 'scene' && lastScenePrompt.value) {
    return '场景提示词按描述原文生成，仅附加项目全局风格'
  }
  if (resourceForm.value.type === 'prop' && lastPropPrompt.value) {
    return hasSceneReference.value
      ? '提示词已包含：多图融合 + 产品摄影质感 + 材质细节'
      : '提示词已包含：产品摄影质感 + 材质细节 + 无人物'
  }
  return ''
})

const namePlaceholder = computed(() => {
  if (isAddingDerivative.value) {
    return resourceForm.value.type === 'scene' ? '例如：夜景、黄昏' : '例如：拳台装、赤膊赛后'
  }
  const t = resourceForm.value.type
  if (t === 'video') return '例如：花絮素材（可选，多文件时作前缀）'
  if (t === 'character') return '例如：小夏'
  if (t === 'prop') return '例如：白玉案留书卷'
  return '例如：九霄云殿'
})

const descPlaceholder = computed(() => {
  if (isAddingDerivative.value) {
    return resourceForm.value.type === 'scene'
      ? '与默认态的差异，例如：夜景、路灯、冷色调…'
      : '与默认态的差异，例如：换装、战损、赤膊、戴奖牌…'
  }
  const t = resourceForm.value.type
  if (t === 'video') return '可选备注，将应用于本次上传的所有视频'
  if (t === 'character') return '年龄、五官、发型、服装、配饰、气质…'
  if (t === 'prop') return '外观、材质、尺寸、年代、细节特征…'
  return '环境、建筑、光影、氛围、镜头质感…'
})

async function runAiGenerate() {
  localImageJobId.value = null
  const t = resourceForm.value.type
  if (t === 'character') localImageJobId.value = await generateCharacterImages()
  else if (t === 'scene') localImageJobId.value = await generateSceneImages()
  else if (t === 'prop') localImageJobId.value = await generatePropImages()
}
</script>

<template>
  <div class="resources-panel" :class="{ 'is-trash-view': resourceLibraryTab === 'trash' }">
    <div class="resource-center-head">
      <div>
        <h3>{{ resourceLibraryTab === 'trash' ? '资源回收站' : '资源管理中心' }}</h3>
        <p v-if="resourceLibraryTab === 'library'" class="hint center-hint">
          <template v-if="libraryParent">点「返回」可回到底模列表。点右上角「添加衍生」可手动新建换装/状态。</template>
          <template v-else>从剧本提取的角色/场景/道具会先出现在这里。角色和场景可点「添加衍生」手动新建换装/状态。</template>
        </p>
        <p v-else class="hint center-hint trash-hint">
          这里是已移出资源库的内容。可恢复使用，或彻底删除（不可恢复）。
        </p>
      </div>
      <div class="resource-head-actions">
        <el-input
          v-model="resourceQuery"
          clearable
          class="resource-search"
          placeholder="搜索资源名称或描述…"
          :prefix-icon="Search"
        />
        <el-button
          v-if="resourceLibraryTab === 'library'"
          type="primary"
          class="add-resource-btn"
          @click="toggleAddResourceForm"
        >
          ＋ {{ libraryParent ? '添加衍生' : '添加资源' }}
        </el-button>
        <el-button
          v-else
          class="back-to-library-btn"
          @click="openResourceLibrary"
        >
          ← 返回资源库
        </el-button>
      </div>
    </div>

    <div v-if="resourceLibraryTab === 'library'" class="resource-toolbar">
      <nav class="resource-library-tabs">
        <span class="library-tab-label on">资源库</span>
      </nav>
      <nav v-if="libraryParent" class="resource-derive-nav">
        <el-button text class="derive-back-btn" @click="closeResourceDerives">← 返回底模</el-button>
        <span class="derive-crumb">{{ resourceDisplayName(libraryParent) }}</span>
        <span class="derive-crumb-sub">衍生 {{ libraryTotal }}</span>
      </nav>
      <nav v-else class="resource-filters">
        <el-button text :class="{ on: resourceFilter === 'all' }" @click="resourceFilter = 'all'">
          全部 <span class="filter-count">{{ resourceCounts.all }}</span>
        </el-button>
        <el-button text :class="{ on: resourceFilter === 'character' }" @click="resourceFilter = 'character'">
          角色 <span class="filter-count">{{ resourceCounts.character }}</span>
        </el-button>
        <el-button text :class="{ on: resourceFilter === 'scene' }" @click="resourceFilter = 'scene'">
          场景 <span class="filter-count">{{ resourceCounts.scene }}</span>
        </el-button>
        <el-button text :class="{ on: resourceFilter === 'prop' }" @click="resourceFilter = 'prop'">
          道具 <span class="filter-count">{{ resourceCounts.prop }}</span>
        </el-button>
        <el-button text :class="{ on: resourceFilter === 'other' }" @click="resourceFilter = 'other'">
          其他 <span class="filter-count">{{ resourceCounts.other }}</span>
        </el-button>
        <el-button text :class="{ on: resourceFilter === 'video' }" @click="resourceFilter = 'video'">
          视频 <span class="filter-count">{{ resourceCounts.video }}</span>
        </el-button>
      </nav>
      <div class="resource-toolbar-right">
        <el-button text class="resource-trash-tab" @click="openResourceTrash">
          回收站 <span v-if="resourceTrashCounts.all" class="trash-count subtle">{{ resourceTrashCounts.all }}</span>
        </el-button>
      </div>
    </div>

    <div v-if="resourceLibraryTab === 'library' && selectableAssets.length" class="asset-batch-bar">
      <el-checkbox
        :model-value="allAssetsSelected"
        :indeterminate="selectedAssets.length > 0 && !allAssetsSelected"
        @update:model-value="toggleSelectAllAssets"
      >
        全选本页
      </el-checkbox>
      <span class="asset-batch-meta">已选 {{ selectedAssets.length }} / {{ selectableAssets.length }}</span>
      <el-button
        :disabled="!selectedAssets.length"
        :loading="batchResourceBusy === 'prompts'"
        @click="batchGeneratePrompts(selectedAssetIds)"
      >
        批量生成提示词
      </el-button>
      <el-button
        type="primary"
        :disabled="!selectedAssets.length"
        :loading="batchResourceBusy === 'images'"
        @click="batchGenerateImages(selectedAssetIds)"
      >
        批量生成图片
      </el-button>
    </div>

    <div v-if="showVideoExportBar" class="video-export-bar">
      <div class="video-export-bar-main">
        <el-checkbox
          :model-value="allVisibleVideosSelected"
          :indeterminate="selectedVideos.length > 0 && !allVisibleVideosSelected"
          :disabled="!visibleVideos.length"
          @update:model-value="toggleSelectAllVideos"
        >
          全选本页
        </el-checkbox>
        <span class="video-export-meta">
          <template v-if="selectedVideos.length">已选 {{ selectedVideos.length }} / {{ visibleVideos.length }}（本页）</template>
          <template v-else-if="visibleVideos.length">本页 {{ visibleVideos.length }} 个 · 全部约 {{ allVideoExportCount }} 个</template>
          <template v-else>共约 {{ allVideoExportCount }} 个视频</template>
        </span>
      </div>
      <div class="video-export-actions">
        <el-button
          class="video-export-btn"
          size="default"
          :disabled="!selectedVideos.length || exportingVideos"
          :loading="exportMode === 'selected'"
          @click="exportSelectedVideos"
        >
          {{ selectedVideos.length ? `导出选中 ${selectedVideos.length}` : '导出选中' }}
        </el-button>
        <el-button
          type="primary"
          class="video-export-btn"
          size="default"
          :disabled="!allVideoExportCount || exportingVideos"
          :loading="exportMode === 'all'"
          @click="exportAllVideos"
        >
          {{ allVideoExportCount ? `导出全部 ${allVideoExportCount}` : '导出全部' }}
        </el-button>
      </div>
    </div>

    <div v-else-if="resourceLibraryTab === 'trash'" class="trash-view-banner">
      <div class="trash-view-banner-main">
        <span class="trash-view-badge">回收站</span>
        <span class="trash-view-count">
          共 {{ trashTotal }} 项
          <template v-if="resourceQuery.trim()">（当前筛选）</template>
        </span>
      </div>
      <p class="trash-view-note">不按类型筛选，全部已删除资源集中展示</p>
    </div>

    <el-dialog
      v-model="exportDialogOpen"
      title="导出视频"
      width="440px"
      :close-on-click-modal="false"
      :close-on-press-escape="!exportingVideos"
      :show-close="!exportingVideos"
      append-to-body
    >
      <div class="export-progress-panel">
        <el-progress
          :percentage="exportPercent"
          :stroke-width="14"
          :status="exportPercent >= 100 ? 'success' : undefined"
        />
        <p class="export-progress-msg">{{ exportPhaseText || '准备中…' }}</p>
        <p v-if="exportCurrentFile" class="export-progress-file" :title="exportCurrentFile">
          {{ exportCurrentFile }}
        </p>
        <p class="export-progress-dest">{{ exportDestHint }}</p>
      </div>
    </el-dialog>

    <el-dialog
      v-model="showAddForm"
      :title="isRegeneratingResource ? '重新生成 · 保留原提示词' : (isAddingDerivative ? '添加衍生图' : '添加资源')"
      width="720px"
      class="add-resource-dialog modal-wide"
      align-center
      :close-on-click-modal="!isAnyResourceGenerating"
    >
      <div class="resource-form resource-form-modal">
        <div class="resource-form-head">
          <div class="resource-form-head-main">
            <div v-if="!libraryParent" class="resource-type">
              <button type="button" :class="{ on: resourceForm.type === 'character' }" @click="switchResourceType('character')">角色</button>
              <button type="button" :class="{ on: resourceForm.type === 'scene' }" @click="switchResourceType('scene')">场景</button>
              <button type="button" :class="{ on: resourceForm.type === 'prop' }" @click="switchResourceType('prop')">道具</button>
              <button type="button" :class="{ on: resourceForm.type === 'video' }" @click="switchResourceType('video')">视频</button>
            </div>
          </div>
        </div>

        <div class="resource-form-body">
          <section class="form-section">
            <div class="form-section-label">基本信息</div>
            <div class="form-fields">
              <label class="field-row">
                <span class="field-label">名称</span>
                <el-input v-model="resourceForm.name" :placeholder="namePlaceholder" />
              </label>
              <p v-if="existingLibraryBase" class="form-footnote existing-base-hint">
                库里已有「{{ resourceDisplayName(existingLibraryBase) }}」。点生成会写入这张{{ isAddingDerivative ? '衍生图' : '底模' }}，不会再新建一张同名卡。
              </p>
              <label class="field-row">
                <span class="field-label">{{ resourceForm.type === 'video' ? '备注' : '描述' }}</span>
                <el-input
                  v-model="resourceForm.description"
                  type="textarea"
                  :rows="3"
                  :placeholder="descPlaceholder"
                  :disabled="isRegeneratingResource"
                />
                <p v-if="isRegeneratingResource" class="form-footnote">资料描述保持原样，不会被本次重新生成覆盖。</p>
              </label>
              <label v-if="isRegeneratingResource" class="field-row regen-prompt-keep">
                <span class="field-label">当前绘图提示词</span>
                <el-input
                  v-model="baseGenPrompt"
                  type="textarea"
                  :rows="8"
                  placeholder="已保存的绘图提示词。本次修改有内容时不会重复发送。"
                />
                <p class="form-footnote">原提示词会完整保留；有“本次修改”时仅发送修改内容和参考图，不重复发送这段旧提示词。</p>
              </label>
              <label v-if="isRegeneratingResource" class="field-row">
                <span class="field-label">本次修改（仅本次）</span>
                <el-input
                  v-model="promptRevision"
                  type="textarea"
                  :rows="3"
                  placeholder="例如：正视图和侧视图的脸部严格参照。只影响本次生成，提交后自动清空。"
                />
              </label>
            </div>
          </section>

          <section v-if="isImageType" class="form-section">
            <div class="form-section-label">
              AI 生成
              <span class="form-section-sub">填写描述后可生成候选图，也可跳过直接上传成品</span>
            </div>

            <div class="ref-zone" @paste.stop="onSceneRefPaste">
              <div class="ref-zone-head">
                <span class="ref-zone-title">{{ img2imgRefLabel }}</span>
                <div class="ref-zone-actions">
                  <label class="ref-action-btn">
                    上传
                    <input type="file" accept="image/*" multiple @change="onSceneRefFiles" />
                  </label>
                  <button type="button" class="ref-action-btn" @click="openSceneRefPicker">资源库</button>
                  <button
                    v-if="sceneReferences.length"
                    type="button"
                    class="ref-action-btn muted"
                    @click="clearSceneReferences"
                  >
                    清空
                  </button>
                </div>
              </div>
              <p class="ref-zone-hint">支持上传、粘贴或从资源库选择参考图</p>
              <p v-if="hasSceneReference" class="ref-zone-hint accent">{{ img2imgRefHint }}</p>

              <div v-if="sceneReferences.length" class="scene-ref-grid compact">
                <div v-for="(ref, ri) in sceneReferences" :key="sceneReferenceKey(ref)" class="scene-ref-card">
                  <img :src="ref.previewUrl" :alt="ref.label" class="zoomable" @click="openImagePreview(ref.previewUrl, ref.label)" />
                  <div class="scene-ref-preview-meta">
                    <span>图{{ ri + 1 }} · {{ ref.label }}</span>
                    <button type="button" class="ref-remove-btn" @click="removeSceneReference(sceneReferenceKey(ref))">×</button>
                  </div>
                </div>
              </div>
              <div v-else class="ref-zone-empty">暂无参考图，可直接根据描述生成</div>

              <div class="generate-row">
                <label class="candidate-count-select">
                  模型
                  <el-select v-model="imageModelId" size="small" clearable filterable placeholder="默认" style="width: 260px">
                    <el-option :value="null" :label="defaultImageModelLabel" />
                    <el-option-group
                      v-for="group in imageModelsByProvider"
                      :key="group.providerId"
                      :label="group.providerName"
                    >
                      <el-option
                        v-for="m in group.models"
                        :key="m.id"
                        :value="m.id"
                        :label="imageModelLabel(m)"
                      />
                    </el-option-group>
                  </el-select>
                </label>
                <label class="candidate-count-select">
                  分辨率
                  <el-select v-model="imageResolution" size="small" style="width: 88px">
                    <el-option
                      v-for="opt in imageResolutionOptions"
                      :key="opt.value"
                      :value="opt.value"
                      :label="opt.label"
                    />
                  </el-select>
                </label>
                <label class="candidate-count-select">
                  候选
                  <el-select v-model.number="candidateCount" size="small" style="width: 88px">
                    <el-option v-for="n in 6" :key="n" :value="n" :label="`${n} 张`" />
                  </el-select>
                </label>
                <el-button
                  type="primary"
                  class="generate-btn"
                  :loading="isGenerating"
                  :disabled="isGenerating || localJobActive"
                  @click="runAiGenerate"
                >
                  {{ generateButtonLabel }}
                </el-button>
              </div>
              <p v-if="otherJobsRunning && !localJobActive" class="image-gen-progress-hint">
                其他资源正在后台生成，可同时提交本资源的任务；进度见右下角「生成任务」。
              </p>
              <div v-if="showJobProgress" class="image-gen-progress">
                <el-progress
                  :percentage="localImageGenProgress?.progress ?? 0"
                  :stroke-width="10"
                  :show-text="true"
                  :format="(p: number) => `${p}%`"
                  :indeterminate="!localImageGenProgress || localImageGenProgress.progress < 1"
                />
                <p class="image-gen-progress-msg">
                  {{ localImageGenProgress?.message || '任务已提交，等待开始…' }}
                  <template v-if="localImageGenProgress?.totalCount">
                    （{{ localImageGenProgress.doneCount ?? 0 }}/{{ localImageGenProgress.totalCount }}）
                  </template>
                </p>
              </div>
              <p v-if="aiPromptHint" class="prompt-preview">{{ aiPromptHint }}</p>
            </div>
          </section>

          <section v-else-if="resourceForm.type === 'video'" class="form-section">
            <div class="form-section-label">上传视频</div>
            <div class="ref-zone">
              <label class="video-upload-btn">
                选择视频文件
                <input type="file" accept="video/mp4,video/webm,video/quicktime,.mp4,.webm,.mov,.m4v" multiple @change="onVideoFiles" />
              </label>
              <p class="ref-zone-hint">支持 mp4 / webm / mov / m4v，单次最多 20 个</p>
              <ul v-if="videoFiles.length" class="video-file-list">
                <li v-for="(f, i) in videoFiles" :key="i">
                  {{ f.name }} <small>{{ (f.size / 1024 / 1024).toFixed(1) }} MB</small>
                </li>
              </ul>
            </div>
          </section>

          <section v-if="localImageCandidates.length" class="form-section">
            <div class="form-section-label">
              候选结果
              <span class="form-section-sub">点击选择候选图，点放大可预览</span>
            </div>
            <div class="candidate-grid">
              <button
                v-for="(img, i) in localImageCandidates"
                :key="i"
                type="button"
                class="candidate"
                :class="{ on: selectedCandidate === img.url }"
                @click="selectCandidate(img.url)"
              >
                <div class="candidate-thumb">
                  <img :src="img.url" :alt="'候选' + (i + 1)" />
                  <button
                    type="button"
                    class="candidate-zoom"
                    title="放大预览"
                    @click.stop="openImagePreview(img.url, '候选 ' + (i + 1), img.url)"
                  >
                    放大
                  </button>
                </div>
                <span>{{ selectedCandidate === img.url ? '已选 · ' : '' }}候选 {{ i + 1 }}</span>
              </button>
            </div>
            <p v-if="localCandidatesPersisted" class="form-footnote saved-hint">
              已自动写入资产库。第一张为主图，其余候选在回收站。
            </p>
          </section>

          <section v-if="isImageType" class="form-section form-section-inline" @paste="onResourceFormPaste">
            <div class="form-section-label">手动上传</div>
            <label class="manual-upload-btn">
              选择成品图
              <input type="file" accept="image/*" @change="onResourceFile" />
            </label>
            <span class="form-footnote">可粘贴图片到此处上传成品图</span>
            <div v-if="resourceForm.imageData" class="manual-upload-preview">
              <img
                :src="resourceForm.imageData"
                alt="成品图预览"
                class="zoomable"
                @click="openImagePreview(resourceForm.imageData, resourceForm.name || '成品图')"
              />
              <button type="button" class="ref-remove-btn" @click="clearResourceImage">×</button>
            </div>
          </section>
        </div>
      </div>

      <template #footer>
        <el-button @click="showAddForm = false">
          {{ isAnyResourceGenerating ? '后台生成' : '取消' }}
        </el-button>
        <el-button
          v-if="showCreateButton"
          type="primary"
          :disabled="saving || uploadingVideos || isGenerating"
          @click="submitResourceForm"
        >
          {{ createResourceLabel }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      :model-value="!!gridViewer"
      :title="gridViewer ? `${resourceDisplayName(gridViewer)} · 9宫格详情` : '9宫格详情'"
      width="980px"
      class="scene-grid-viewer-dialog"
      align-center
      append-to-body
      @close="gridViewer = null"
    >
      <div v-if="gridViewer" class="scene-grid-viewer">
        <section class="scene-grid-viewer-source">
          <div class="scene-grid-viewer-label">9宫格整图</div>
          <img
            v-if="gridViewer.imageUrl"
            :src="gridViewer.imageUrl"
            :alt="gridViewer.name"
            class="zoomable"
            @click="openImagePreview(gridViewer.imageUrl, gridViewer.name)"
          />
        </section>
        <section class="scene-grid-viewer-cells">
          <div
            v-for="cell in gridViewerCells"
            :key="cell.id"
            class="scene-grid-cell-card"
            :class="{ 'image-loading': !loadedGridCellIds.has(cell.id) }"
          >
            <div v-if="!loadedGridCellIds.has(cell.id)" class="scene-grid-cell-loading">
              <span class="loading-spinner" />
              图片加载中…
            </div>
            <img
              v-if="cell.imageUrl"
              :src="cell.imageUrl"
              :alt="cell.name"
              class="zoomable"
              @load="markGridCellLoaded(cell.id)"
              @error="markGridCellLoaded(cell.id)"
              @click="openImagePreview(cell.imageUrl, cell.name)"
            />
            <span class="scene-grid-cell-index">格{{ cell.gridCell }}</span>
            <b>{{ sceneGridAngleOf(cell) || cell.name }}</b>
            <el-button size="small" @click="gridViewer = null; openRegenerateResource(cell)">调整此格</el-button>
          </div>
          <div v-if="gridViewerLoading || (gridViewer && splittingGridIds.has(gridViewer.id))" class="scene-grid-viewer-empty loading">
            <span class="loading-spinner" />
            {{ gridViewer && splittingGridIds.has(gridViewer.id) ? '正在切分9格，请稍候…' : '正在加载机位图片…' }}
          </div>
          <div v-else-if="!gridViewerCells.length" class="scene-grid-viewer-empty">
            尚未切分，点击下方按钮生成 9 个机位格。
          </div>
        </section>
      </div>
      <template #footer>
        <el-button
          v-if="gridViewer && !gridViewerCells.length"
          type="primary"
          :loading="splittingGridIds.has(gridViewer.id)"
          @click="splitGridInViewer"
        >
          切分9格
        </el-button>
        <el-button @click="gridViewer = null">关闭</el-button>
      </template>
    </el-dialog>

    <div v-if="resourceLibraryTab === 'library'" class="resource-manager-grid" v-loading="libraryLoading">
      <template v-for="entry in managedResourceDisplay" :key="entry.resource.id">
        <el-card
          class="resource-card"
          :class="[
            entry.resource.type,
            {
              selected: (entry.resource.type === 'video' && selectedVideoIds.includes(entry.resource.id))
                || (isSelectableAsset(entry.resource) && selectedAssetIds.includes(entry.resource.id)),
            },
          ]"
          shadow="never"
        >
          <template v-if="entry.resource.type === 'video'">
            <label v-if="entry.resource.videoUrl" class="video-select" @click.stop>
              <el-checkbox
                :model-value="selectedVideoIds.includes(entry.resource.id)"
                @update:model-value="(v: string | number | boolean) => toggleVideoSelect(entry.resource, !!v)"
              />
            </label>
            <video v-if="entry.resource.videoUrl" :src="entry.resource.videoUrl" controls preload="none" class="resource-video" />
            <div v-else class="resource-video placeholder">视频不可用</div>
          </template>
          <template v-else-if="entry.resource.type === 'character' || entry.resource.type === 'other'">
            <label v-if="isSelectableAsset(entry.resource)" class="asset-select" @click.stop>
              <el-checkbox
                :model-value="selectedAssetIds.includes(entry.resource.id)"
                @update:model-value="(v: string | number | boolean) => toggleAssetSelect(entry.resource, !!v)"
              />
            </label>
            <div class="resource-dual">
              <div>
                <img
                  v-if="entry.resource.imageUrl"
                  :src="entry.resource.imageUrl"
                  :alt="entry.resource.name"
                  loading="lazy"
                  decoding="async"
                  class="zoomable"
                  @click="openResourceImage(entry.resource)"
                />
                <div
                  v-else
                  class="resource-pending"
                  :class="{ clickable: !entry.resource.parentId && (entry.resource.deriveCount || 0) > 0 }"
                  @click="openBaseOrPreview(entry.resource)"
                >待出图</div>
                <small>{{ entry.resource.type === 'character' ? '定妆照' : '原图' }}</small>
              </div>
              <div>
                <img
                  v-if="entry.resource.stylizedImageUrl"
                  :key="entry.resource.stylizedImageUrl"
                  :src="entry.resource.stylizedImageUrl"
                  :alt="entry.resource.name + '非真人'"
                  loading="lazy"
                  decoding="async"
                  class="zoomable"
                  @click="openImagePreview(entry.resource.stylizedImageUrl, entry.resource.name + ' · 非真人手绘图')"
                />
                <small v-if="entry.resource.stylizedImageUrl">非真人手绘图</small>
                <small v-else class="hint">尚未生成非真人图</small>
              </div>
            </div>
          </template>
          <template v-else-if="entry.resource.type === 'scene' || entry.resource.type === 'prop'">
            <label v-if="isSelectableAsset(entry.resource)" class="asset-select" @click.stop>
              <el-checkbox
                :model-value="selectedAssetIds.includes(entry.resource.id)"
                @update:model-value="(v: string | number | boolean) => toggleAssetSelect(entry.resource, !!v)"
              />
            </label>
            <img
              v-if="entry.resource.imageUrl"
              :src="entry.resource.imageUrl"
              :alt="entry.resource.name"
              loading="lazy"
              decoding="async"
              class="zoomable"
              @click="openResourceImage(entry.resource)"
            />
            <div
              v-else
              class="resource-pending"
              :class="{ clickable: !entry.resource.parentId && (entry.resource.deriveCount || 0) > 0 }"
              @click="openBaseOrPreview(entry.resource)"
            >待出图</div>
          </template>
          <template v-else>
            <label v-if="isSelectableAsset(entry.resource)" class="asset-select" @click.stop>
              <el-checkbox
                :model-value="selectedAssetIds.includes(entry.resource.id)"
                @update:model-value="(v: string | number | boolean) => toggleAssetSelect(entry.resource, !!v)"
              />
            </label>
            <img
              v-if="entry.resource.imageUrl"
              :src="entry.resource.imageUrl"
              :alt="entry.resource.name"
              loading="lazy"
              decoding="async"
              class="zoomable"
              @click="openResourceImage(entry.resource)"
            />
            <div v-else class="resource-pending">待出图</div>
          </template>
          <div class="resource-meta">
            <div class="resource-meta-top">
              <div class="resource-tags">
                <span class="tag">{{ resourceTypeLabel(entry.resource.type) }}</span>
                <span v-if="entry.resource.parentId" class="tag derive-tag">衍生</span>
                <span
                  v-else-if="(entry.resource.deriveCount || 0) > 0"
                  class="tag derive-tag clickable"
                  @click="openResourceDerives(entry.resource)"
                >衍生 {{ entry.resource.deriveCount }}</span>
                <span v-if="resourceSourceLabel(entry.resource.source)" class="tag source-tag">{{ resourceSourceLabel(entry.resource.source) }}</span>
                <span v-if="gridBadgeLabel(entry.resource)" class="tag grid-tag">{{ gridBadgeLabel(entry.resource) }}</span>
                <span v-if="gridSplitDone(entry.resource)" class="tag grid-split-tag">已切分</span>
                <span v-if="panoramaSplitDone(entry.resource)" class="tag grid-split-tag">已切机位</span>
                <span v-if="drawingPrompt(entry.resource)" class="tag prompt-ready-tag">已有提示词</span>
                <span v-if="entry.resource.type === 'character' && entry.resource.voicePrompt?.trim()" class="tag prompt-ready-tag">已有音色</span>
                <span v-if="!entry.resource.imageUrl && isSelectableAsset(entry.resource)" class="tag">待出图</span>
                <span v-if="entry.resource.isGroupPrimary" class="tag active-tag">使用中</span>
                <span v-else-if="isActiveShotVideo(entry.resource)" class="tag active-tag">分镜使用中</span>
              </div>
              <b
                class="resource-name"
                :class="{ clickable: !entry.resource.parentId && (entry.resource.deriveCount || 0) > 0 }"
                :title="resourceDisplayName(entry.resource)"
                @click="openBaseOrPreview(entry.resource)"
              >{{ resourceDisplayName(entry.resource) }}</b>
              <p v-if="parseCandidateName(entry.resource.name)" class="hint resource-subtitle">{{ entry.resource.name }}</p>
              <div v-if="entry.resource.type === 'video'" class="resource-video-meta-row">
                <span v-if="entry.resource.shotId" class="resource-subtitle">
                  {{ entry.resource.duration || 10 }}s · {{ entry.resource.resolution || '720p' }}
                </span>
                <button
                  type="button"
                  class="resource-remark-inline"
                  :class="{ empty: !entry.resource.remark?.trim() }"
                  :title="entry.resource.remark?.trim() || '添加备注（导出文件名会用到）'"
                  @click="openEditResourceModal(entry.resource)"
                >
                  {{ entry.resource.remark?.trim() || '备注' }}
                </button>
              </div>
              <p class="resource-desc" :class="{ empty: !entry.resource.description }" :title="entry.resource.description || ''">
                {{ entry.resource.description || '暂无描述' }}
              </p>
              <div v-if="isSelectableAsset(entry.resource)" class="resource-prompt-block">
                <span class="resource-prompt-label">绘图提示词</span>
                <p
                  v-if="drawingPrompt(entry.resource)"
                  class="resource-prompt"
                  :class="{ expanded: isPromptExpanded(entry.resource.id) }"
                  :title="drawingPrompt(entry.resource)"
                  @click="togglePromptExpand(entry.resource.id)"
                >{{ drawingPrompt(entry.resource) }}</p>
                <p v-else class="resource-prompt empty">尚未生成。勾选后点「批量生成提示词」</p>
              </div>
              <div v-if="entry.resource.type === 'character'" class="resource-prompt-block">
                <span class="resource-prompt-label">音色提示词</span>
                <p
                  v-if="entry.resource.voicePrompt?.trim()"
                  class="resource-prompt"
                  :title="entry.resource.voicePrompt"
                >{{ entry.resource.voicePrompt }}</p>
                <p v-else class="resource-prompt empty">尚未生成。提取角色或点「批量生成提示词」会自动写，也可点编辑修改</p>
              </div>
              <div v-if="entry.resource.genRefs?.length" class="resource-gen-refs">
                <span class="resource-gen-refs-label">生图参考</span>
                <div class="resource-gen-ref-thumbs">
                  <button
                    v-for="(gref, gi) in entry.resource.genRefs"
                    :key="`${gref.id}-${gref.variant || ''}-${gi}`"
                    type="button"
                    class="resource-gen-ref-thumb"
                    :title="`图${gi + 1} · ${gref.label || gref.id}`"
                    @click="gref.imageUrl && openImagePreview(gref.imageUrl, `图${gi + 1} · ${gref.label || gref.id}`)"
                  >
                    <img v-if="gref.imageUrl" :src="gref.imageUrl" :alt="gref.label || ''" />
                    <span v-else class="resource-gen-ref-missing">?</span>
                  </button>
                </div>
              </div>
            </div>
            <div class="resource-actions">
              <el-button size="small" class="resource-action-btn" @click="openEditResourceModal(entry.resource)">
                编辑
              </el-button>
              <el-button
                v-if="canHaveDerivatives(entry.resource)"
                size="small"
                class="resource-action-btn"
                @click="openResourceDerives(entry.resource)"
              >
                {{ (entry.resource.deriveCount || 0) > 0 ? `查看衍生 (${entry.resource.deriveCount})` : '添加衍生' }}
              </el-button>
              <el-dropdown
                trigger="click"
                class="resource-more-dropdown"
                @command="(cmd: string) => onResourceMoreCommand(entry.resource, cmd)"
              >
                <el-button size="small" class="resource-action-btn resource-more-btn">
                  更多
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item v-if="canBuildSceneGrid(entry.resource)" command="scene-grid">
                      生成9宫格
                    </el-dropdown-item>
                    <el-dropdown-item v-if="canBuildSceneReverse(entry.resource)" command="scene-reverse">
                      生成反打图
                    </el-dropdown-item>
                    <el-dropdown-item v-if="canBuildScenePanorama(entry.resource)" command="scene-panorama">
                      生成全景图
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="canSplitGrid(entry.resource)"
                      command="split-grid"
                      :disabled="splittingGridIds.has(entry.resource.id)"
                    >
                      {{ gridSplitDone(entry.resource) ? '重新切分' : '切分9格' }}
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="canSplitPanorama(entry.resource)"
                      command="split-panorama"
                      :disabled="splittingPanoramaIds.has(entry.resource.id)"
                    >
                      {{ panoramaSplitDone(entry.resource) ? '重新切机位' : '切出机位' }}
                    </el-dropdown-item>
                    <el-dropdown-item v-if="canSplitPanorama(entry.resource)" command="pano-view">
                      环视全景
                    </el-dropdown-item>
                    <el-dropdown-item v-if="canSplitPanorama(entry.resource)" command="desk-browse">
                      3D摆位
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="entry.resource.genType === 'scene_grid' && gridSplitDone(entry.resource)"
                      command="grid-viewer"
                    >
                      查看9格
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="canRegenerateResource(entry.resource)"
                      command="regenerate"
                      divided
                    >
                      重新生成
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="entry.resource.type === 'video' && entry.resource.videoUrl"
                      command="export-video"
                      :disabled="exportingVideos"
                    >
                      导出
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="entry.resource.type === 'video' && entry.resource.shotId"
                      command="go-shot"
                    >
                      查看分镜
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="entry.resource.type === 'video' && entry.resource.shotId"
                      command="use-shot-video"
                      :disabled="isActiveShotVideo(entry.resource) || applyingShotVideo === entry.resource.id"
                    >
                      {{ isActiveShotVideo(entry.resource) ? '使用中' : applyingShotVideo === entry.resource.id ? '设置中…' : '设为分镜' }}
                    </el-dropdown-item>
                    <el-dropdown-item
                      v-if="entry.resource.type === 'character' || entry.resource.type === 'other'"
                      command="stylize"
                      :disabled="isStylizingResource(entry.resource.id)"
                    >
                      {{ isStylizingResource(entry.resource.id) ? '非真人生成中…' : (entry.resource.stylizedImageUrl ? '重新生成非真人' : '生成非真人图') }}
                    </el-dropdown-item>
                    <el-dropdown-item command="delete" divided class="resource-more-delete">
                      删除
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </div>
        </el-card>
      </template>
      <p v-if="!managedResourceDisplay.length && !libraryLoading" class="hint resource-empty">
        {{
          libraryParent
            ? (resourceQuery.trim() ? '没有匹配的衍生资源' : '还没有衍生图。点「添加衍生」可手动新建换装/状态，也可从剧本提取。')
            : resourceQuery.trim()
              ? '没有匹配的资源'
              : (resourceFilter === 'all'
                ? '还没有任何资源。可点击「添加资源」上传或生成，分镜视频生成后也会自动归档。'
                : `暂无${resourceTypeLabel(resourceFilter)}资源`)
        }}
      </p>
      <div v-if="libraryTotal > resourcePageSize" class="resource-pagination">
        <el-pagination
          v-model:current-page="resourcePage"
          :page-size="resourcePageSize"
          :total="libraryTotal"
          layout="total, prev, pager, next"
          background
          small
        />
      </div>
    </div>

    <div v-else class="trash-panel">
      <div class="trash-list">
        <el-card v-for="item in managedResourceTrash" :key="item.id" class="trash-item-card" shadow="never">
          <div class="trash-item-media">
            <img v-if="item.imageUrl" :src="item.imageUrl" :alt="item.name" class="zoomable" @click="openImagePreview(item.imageUrl, item.name)" />
            <div v-else class="trash-item-placeholder">无预览</div>
          </div>
          <div class="trash-item-body">
            <div class="trash-item-tags">
              <span class="tag">{{ resourceTypeLabel(item.type) }}</span>
              <span class="tag trash-tag">已移入回收站</span>
            </div>
            <b class="resource-name" :title="item.name">{{ item.name }}</b>
            <p v-if="item.deletedAt" class="trash-time">移入于 {{ new Date(item.deletedAt).toLocaleString() }}</p>
            <p class="resource-desc" :class="{ empty: !item.description }" :title="item.description || ''">
              {{ item.description || '暂无描述' }}
            </p>
            <div class="trash-item-actions">
              <el-button size="small" type="primary" :disabled="applyingPrimary === item.id" @click="usePrimaryResource(item)">
                {{ applyingPrimary === item.id ? '恢复中…' : '恢复使用' }}
              </el-button>
              <el-button size="small" type="danger" @click="purgeResourceTrash(item)">彻底删除</el-button>
            </div>
          </div>
        </el-card>
      </div>
      <p v-if="!managedResourceTrash.length && !trashLoading" class="hint resource-empty trash-empty">
        {{ resourceQuery.trim() ? '没有匹配的回收站资源' : '回收站是空的。AI 生成后未选中的候选图会自动出现在这里。' }}
      </p>
      <div v-if="trashTotal > trashPageSize" class="resource-pagination">
        <el-pagination
          v-model:current-page="trashPage"
          :page-size="trashPageSize"
          :total="trashTotal"
          layout="total, prev, pager, next"
          background
          small
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.scene-grid-viewer {
  display: grid;
  gap: 18px;
}

.scene-grid-viewer-source {
  display: grid;
  gap: 8px;
}

.scene-grid-viewer-source img {
  width: 100%;
  max-height: 360px;
  object-fit: contain;
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  background: #12100e;
}

.scene-grid-viewer-label {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.scene-grid-viewer-cells {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.scene-grid-cell-card {
  position: relative;
  display: grid;
  gap: 7px;
  padding: 8px;
  color: var(--el-text-color-primary);
  text-align: left;
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  background: var(--el-fill-color-light);
  cursor: zoom-in;
}

.scene-grid-cell-card:hover {
  border-color: var(--el-color-primary);
}

.scene-grid-cell-card img {
  width: 100%;
  aspect-ratio: 16 / 9;
  object-fit: cover;
  border-radius: 7px;
}

.scene-grid-cell-card.image-loading img {
  opacity: 0;
}

.scene-grid-cell-loading {
  position: absolute;
  z-index: 2;
  inset: 8px 8px auto;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  aspect-ratio: 16 / 9;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  border-radius: 7px;
  background: var(--el-fill-color-darker);
}

.loading-spinner {
  width: 18px;
  height: 18px;
  border: 2px solid rgb(255 255 255 / 18%);
  border-top-color: var(--el-color-primary);
  border-radius: 50%;
  animation: grid-spin 0.8s linear infinite;
}

.scene-grid-cell-card b {
  padding: 0 2px 2px;
  font-size: 13px;
}

.scene-grid-cell-index {
  position: absolute;
  top: 14px;
  left: 14px;
  padding: 2px 7px;
  color: #fff;
  font-size: 12px;
  border-radius: 999px;
  background: rgb(0 0 0 / 65%);
}

.scene-grid-viewer-empty {
  grid-column: 1 / -1;
  padding: 36px;
  color: var(--el-text-color-secondary);
  text-align: center;
  border: 1px dashed var(--el-border-color);
  border-radius: 10px;
}

.scene-grid-viewer-empty.loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
}

@keyframes grid-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 760px) {
  .scene-grid-viewer-cells {
    grid-template-columns: 1fr;
  }
}
</style>

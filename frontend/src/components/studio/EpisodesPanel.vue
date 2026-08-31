<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus } from '@element-plus/icons-vue'
import BilibiliVideoPlayer from '@/components/studio/BilibiliVideoPlayer.vue'
import { useNovalyInject } from '@/composables/useNovalyInject'
import { visualManualVideoPrompt } from '@/data/projectManuals'
import type { Shot } from '@/types'

const {
  active,
  activeEpisode,
  studioTab,
  defaultStyle,
  scriptPlaceholder,
  resolutionOptions,
  maxShotRefs,
  generating,
  uploadingShot,
  uploadingShotRef,
  applyingShotVideo,
  previewingPrompt,
  optimizingScripts,
  extractingFrame,
  extractPreviousFrame,
  shotScriptEpoch,
  showAdvanced,
  videoModels,
  defaultVideoModelLabel,
  picker,
  pickerReplaceIndex,
  addShot,
  moveShot,
  isShotExpanded,
  highlightedShotId,
  toggleShotExpand,
  shotSummary,
  shotTimeLabel,
  shotVideoMeta,
  shotVideoScript,
  removeShot,
  generateShot,
  resplitFromShot,
  crewBusy,
  onShotVideoFile,
  previewShotPrompt,
  optimizeShotScript,
  rematchShotRefs,
  matchingShotRefs,
  openPositioningModal,
  openMotionGridModal,
  refThumb,
  refLabel,
  refDisplayName,
  refTag,
  removeShotRef,
  renameShotRef,
  shotRefKey,
  openPicker,
  onShotRefFiles,
  onShotComposerPaste,
  openReplacePicker,
  pickerShot,
  ensureShotModel,
  videoModelLabel,
  providerName,
  toggleAdvanced,
  shotVideoSrc,
  shotVideoVersions,
  shotActiveVideoResource,
  useShotVideo,
  exportShotVideo,
  saveShot,
  markShotDirty,
  isShotDirty,
  beginShotEditSession,
  revertShotEdits,
  reloadShotFromServer,
  shotUiStatus,
  shotUiStatusLabel,
  openImagePreview,
  shotTotal,
  shotPage,
  shotPageSize,
  shotPageLoading,
  crewJob,
  openCrewModal,
  addEpisode,
  addingEpisode,
  removeEpisode,
  goToEpisode,
} = useNovalyInject()

const route = useRoute()
const router = useRouter()

/** Draft labels while focused — status polls must not wipe mid-edit / IME composition. */
const editingRefLabels = ref<Record<string, string>>({})

function shotRefLegendKey(shotId: number, ri: number, ref: Shot['refs'][number]) {
  return `${shotId}:${ri}:${shotRefKey(ref)}`
}

function shotRefLegendValue(shot: Shot, ri: number, ref: Shot['refs'][number]) {
  const key = shotRefLegendKey(shot.id, ri, ref)
  if (Object.prototype.hasOwnProperty.call(editingRefLabels.value, key)) {
    return editingRefLabels.value[key]
  }
  const custom = (ref.label || '').trim()
  return refDisplayName(ref) || custom
}

function onShotRefLegendFocus(shot: Shot, ri: number, ref: Shot['refs'][number]) {
  const key = shotRefLegendKey(shot.id, ri, ref)
  if (Object.prototype.hasOwnProperty.call(editingRefLabels.value, key)) return
  editingRefLabels.value = {
    ...editingRefLabels.value,
    [key]: refDisplayName(ref),
  }
  markShotDirty(shot.id)
}

function onShotRefLegendInput(shot: Shot, ri: number, ref: Shot['refs'][number], value: string) {
  const key = shotRefLegendKey(shot.id, ri, ref)
  editingRefLabels.value = { ...editingRefLabels.value, [key]: value }
  markShotDirty(shot.id)
}

function onShotRefLegendCommit(shot: Shot, ri: number, ref: Shot['refs'][number], value: string) {
  const key = shotRefLegendKey(shot.id, ri, ref)
  const next = { ...editingRefLabels.value }
  delete next[key]
  editingRefLabels.value = next
  renameShotRef(shot, ri, value)
}

function parsePageQuery(raw: unknown): number {
  const value = Array.isArray(raw) ? raw[0] : raw
  const n = Number(value)
  return Number.isFinite(n) && n >= 1 ? Math.floor(n) : 1
}

const exportingShotId = ref<number | null>(null)
let syncingPageFromRoute = false

async function onExportShotVideo(shot: Shot) {
  exportingShotId.value = shot.id
  try {
    await exportShotVideo(shot)
  } finally {
    exportingShotId.value = null
  }
}

const pageShots = computed(() => activeEpisode.value?.shots || [])
const maxShotPage = computed(() => Math.max(1, Math.ceil(shotTotal.value / shotPageSize) || 1))
const pageOffset = computed(() => (shotPage.value - 1) * shotPageSize)
const episodeList = computed(() => active.value?.episodes || [])

function episodeScriptFilled(ep: { script?: string }) {
  return !!(ep.script || '').trim()
}

function clampShotPage(page: number) {
  return Math.min(Math.max(1, page), maxShotPage.value)
}

async function syncPageToRoute(page: number) {
  // Panel stays mounted under v-show; never rewrite URL while viewing 资源.
  if (studioTab.value !== 'episodes' || route.name === 'project-resources') return
  const query = { ...route.query } as Record<string, string | string[] | undefined>
  if (shotTotal.value <= shotPageSize) {
    if (query.page == null) return
    delete query.page
    await router.replace({ query })
    return
  }
  const next = String(page)
  if (String(query.page ?? '') === next) return
  query.page = next
  await router.replace({ query })
}

watch(
  () => shotTotal.value,
  (n) => {
    if (!n) return
    const fromUrl = parsePageQuery(route.query.page)
    const next = clampShotPage(fromUrl > 1 || route.query.page != null ? fromUrl : shotPage.value)
    if (shotPage.value !== next) shotPage.value = next
  },
)

watch(
  () => activeEpisode.value?.id,
  (id, prev) => {
    // Keep ?page= on first hydrate; only reset when switching episodes.
    if (prev == null || id == null || id === prev) return
    shotPage.value = 1
  },
)

watch(shotPage, (page) => {
  const next = clampShotPage(page)
  if (next !== page) {
    shotPage.value = next
    return
  }
  if (!syncingPageFromRoute) syncPageToRoute(next)
})

watch(
  () => route.query.page,
  (raw) => {
    const next = clampShotPage(parsePageQuery(raw))
    if (next === shotPage.value) return
    syncingPageFromRoute = true
    shotPage.value = next
    syncingPageFromRoute = false
  },
)

function goToShotPage(globalIndex: number) {
  shotPage.value = Math.floor(Math.max(0, globalIndex) / shotPageSize) + 1
}

async function addShotAt(globalIndex?: number) {
  const appendIndex = shotTotal.value
  await addShot(globalIndex)
  goToShotPage(globalIndex !== undefined ? globalIndex : appendIndex)
}

async function moveShotOnPage(shot: Shot, delta: -1 | 1) {
  const globalIndex = pageOffset.value + pageShots.value.findIndex(s => s.id === shot.id)
  await moveShot(shot, delta)
  if (globalIndex >= 0) goToShotPage(globalIndex + delta)
}

function onVideoInput(shot: Shot, e: Event) {
  onShotVideoFile(shot, e)
}

function generatingWaitHint(shot: Shot) {
  const eta = String(shot.videoEta || '').trim()
  if (eta) {
    return /完成后会自动更新/.test(eta) ? eta : `${eta}，完成后会自动更新`
  }
  return '正在提交到豆包，预计等待时间稍后显示'
}

function statusTagType(shot: Shot): 'success' | 'warning' | 'danger' | 'info' {
  const s = shotUiStatus(shot)
  if (s === 'done') return 'success'
  if (s === 'generating') return 'warning'
  if (s === 'error') return 'danger'
  return 'info'
}
</script>

<template>
  <div class="episodes-panel">
    <div v-if="activeEpisode" class="episode-workspace">
      <div class="episode-tabs">
        <div
          v-for="ep in episodeList"
          :key="ep.id"
          class="episode-tab-wrap"
        >
          <button
            type="button"
            :class="{ on: ep.id === activeEpisode.id }"
            @click="goToEpisode(ep.number)"
          >
            第{{ ep.number }}集
            <span
              v-if="episodeScriptFilled(ep)"
              class="ep-script-dot"
              title="已填剧本"
            />
          </button>
          <button
            v-if="episodeList.length > 1"
            type="button"
            class="episode-tab-del"
            title="删除这一集"
            @click="removeEpisode(ep)"
          >
            ×
          </button>
        </div>
        <button
          type="button"
          class="episode-add"
          :disabled="addingEpisode"
          @click="addEpisode"
        >
          {{ addingEpisode ? '添加中…' : '+ 添加一集' }}
        </button>
      </div>

      <div class="section-head">
        <div>
          <h3 class="section-title">分镜</h3>
          <p class="section-sub">第 {{ activeEpisode.number }} 集 · {{ shotTotal }} 个分镜 · 每页 {{ shotPageSize }} 条</p>
        </div>
        <div class="episode-head-actions">
          <el-button v-if="crewJob" @click="openCrewModal">剧组进度</el-button>
          <el-button type="primary" :icon="Plus" @click="() => addShotAt()">添加分镜</el-button>
        </div>
      </div>

      <el-empty v-if="!shotTotal && !shotPageLoading" description="还没有分镜">
        <template #image><span class="empty-icon">✦</span></template>
        <p class="empty-hint">剧本在「剧本」页填写。提取资产并出图后，再在这里拆镜或手动添加分镜。</p>
        <div class="empty-actions">
          <el-button type="primary" :icon="Plus" @click="() => addShotAt()">添加第一个分镜</el-button>
        </div>
      </el-empty>

      <div v-else class="shot-list">
        <div v-if="shotTotal > shotPageSize" class="shot-pagination shot-pagination-top">
          <el-pagination
            v-model:current-page="shotPage"
            background
            layout="prev, pager, next, jumper"
            :page-size="shotPageSize"
            :total="shotTotal"
            :pager-count="7"
          />
        </div>

        <template v-for="(shot, localIndex) in pageShots" :key="shot.id">
          <div class="shot-insert-row">
            <button
              type="button"
              class="shot-insert-btn"
              title="在此前插入分镜"
              @click="addShotAt(pageOffset + localIndex)"
            >
              <span class="shot-insert-icon">+</span>
              <span class="shot-insert-label">插入分镜</span>
            </button>
          </div>

          <div :data-shot-id="shot.id">
          <el-card
            class="shot-card"
            :class="{ collapsed: !isShotExpanded(shot.id), highlight: highlightedShotId === shot.id }"
            shadow="never"
          >
            <div
              class="shot-head"
              role="button"
              tabindex="0"
              @click="toggleShotExpand(shot.id)"
              @keydown.enter.prevent="toggleShotExpand(shot.id)"
            >
              <el-icon class="shot-chevron" :class="{ expanded: isShotExpanded(shot.id) }">
                <svg viewBox="0 0 1024 1024" width="1em" height="1em"><path fill="currentColor" d="M384 192v640l384-320z" /></svg>
              </el-icon>
              <span class="shot-index">分镜 {{ String(pageOffset + localIndex + 1).padStart(2, '0') }}</span>
              <el-input
                v-if="isShotExpanded(shot.id)"
                v-model="shot.label"
                size="small"
                class="shot-label-input"
                placeholder="标签"
                maxlength="24"
                clearable
                @click.stop
                @input="markShotDirty(shot.id)"
                @focus="beginShotEditSession(shot)"
                @change="saveShot(shot)"
              />
              <el-tag v-else-if="shot.label?.trim()" size="small" effect="plain" class="shot-label-tag">
                {{ shot.label.trim() }}
              </el-tag>
              <div class="shot-tags">
                <el-tag size="small" type="info" effect="plain">{{ shot.duration }}s · {{ shot.resolution }}</el-tag>
                <el-tag size="small" :type="statusTagType(shot)" effect="dark">
                  {{ shotUiStatusLabel(shot) }}
                </el-tag>
                <el-tag v-if="!isShotExpanded(shot.id) && shot.refs.length" size="small" effect="plain">
                  {{ shot.refs.length }} 张参考
                </el-tag>
                <el-tag v-if="!isShotExpanded(shot.id) && shot.videoUrl" size="small" type="success" effect="plain">
                  有视频
                </el-tag>
                <span v-if="shotTimeLabel(shot)" class="shot-time">{{ shotTimeLabel(shot) }}</span>
              </div>
              <div class="shot-actions" @click.stop>
                <el-button
                  text
                  size="small"
                  class="shot-move"
                  :disabled="pageOffset + localIndex === 0"
                  title="上移"
                  @click="moveShotOnPage(shot, -1)"
                >
                  ↑
                </el-button>
                <el-button
                  text
                  size="small"
                  class="shot-move"
                  :disabled="pageOffset + localIndex === shotTotal - 1"
                  title="下移"
                  @click="moveShotOnPage(shot, 1)"
                >
                  ↓
                </el-button>
                <el-button
                  text
                  size="small"
                  class="shot-resplit"
                  :disabled="crewBusy"
                  title="删除本镜及之后，按剧情续拆；前面分镜与成片保留"
                  @click="resplitFromShot(shot)"
                >
                  从此续拆
                </el-button>
                <el-button
                  text
                  type="danger"
                  size="small"
                  class="shot-delete"
                  @click="removeShot(shot)"
                >
                  删除
                </el-button>
              </div>
            </div>

        <p v-if="!isShotExpanded(shot.id)" class="shot-summary">{{ shotSummary(shot) }}</p>
        <div v-if="!isShotExpanded(shot.id) && shotVideoMeta(shot)?.model" class="shot-meta">
          <el-tag size="small" effect="plain" type="info">
            {{ shotVideoMeta(shot)?.model }}
          </el-tag>
        </div>
        <p v-if="!isShotExpanded(shot.id) && shot.note?.trim()" class="shot-note">
          <span class="shot-note-label">备注</span>{{ shot.note.trim() }}
        </p>
        <el-alert
          v-if="!isShotExpanded(shot.id) && shot.errorMessage && shot.status !== 'generating' && generating !== shot.id"
          :title="shot.errorMessage"
          type="error"
          :closable="false"
          show-icon
          class="shot-error-inline"
        />

        <template v-if="isShotExpanded(shot.id)">
          <div class="shot-composer" @paste="onShotComposerPaste(shot, $event)">
            <div class="composer-refs">
              <div class="ref-add-wrap">
                <button
                  type="button"
                  class="ref-add"
                  :class="{ loading: uploadingShotRef === shot.id }"
                  :disabled="shot.refs.length >= maxShotRefs || uploadingShotRef === shot.id"
                  @click="openPicker(shot)"
                >
                  <span class="ref-add-icon">+</span>
                  <small>{{ uploadingShotRef === shot.id ? '上传中…' : `参考 ${shot.refs.length}/${maxShotRefs}` }}</small>
                </button>
                <label
                  class="ref-upload-btn"
                  :class="{ disabled: shot.refs.length >= maxShotRefs || uploadingShotRef === shot.id }"
                  title="上传图片"
                >
                  ↑
                  <input
                    type="file"
                    accept="image/*"
                    multiple
                    :disabled="shot.refs.length >= maxShotRefs || uploadingShotRef === shot.id"
                    @change="onShotRefFiles(shot, $event)"
                  />
                </label>
              </div>
              <p class="composer-ref-hint">点击 + 从资源库选择 · 点 ↑ 上传 · 可粘贴图片 · 下方可改「图N为谁」</p>
              <div
                v-for="(ref, ri) in shot.refs"
                :key="shotRefKey(ref)"
                class="ref-thumb-wrap"
                :class="{ replacing: picker != null && pickerReplaceIndex === ri && pickerShot()?.id === shot.id }"
              >
                <div class="ref-thumb" :title="refLabel(ref)">
                  <img
                    v-if="refThumb(ref)"
                    :src="refThumb(ref)"
                    :alt="refLabel(ref)"
                    class="zoomable"
                    @click="openImagePreview(refThumb(ref), `${refLabel(ref)} · ${refTag(ref, ri)}`)"
                  />
                  <span class="ref-tag">{{ refTag(ref, ri) }}</span>
                  <button type="button" class="ref-replace" title="替换" @click.stop="openReplacePicker(shot, ri)">换</button>
                  <button type="button" class="ref-remove" title="移除" @click.stop="removeShotRef(shot, ref)">×</button>
                </div>
              </div>
              <div v-if="shot.refs.length" class="shot-ref-legend">
                <div class="shot-ref-legend-title">参考图对应（可改名称）</div>
                <ul class="shot-ref-legend-list">
                  <li v-for="(ref, ri) in shot.refs" :key="'legend-' + shotRefKey(ref)">
                    <span class="shot-ref-legend-prefix">图{{ ri + 1 }}为</span>
                    <input
                      class="shot-ref-legend-input"
                      type="text"
                      :value="shotRefLegendValue(shot, ri, ref)"
                      :title="'生成视频时按此对应「图' + (ri + 1) + '」'"
                      @click.stop
                      @mousedown.stop
                      @keydown.stop
                      @pointerdown.stop
                      @focus="onShotRefLegendFocus(shot, ri, ref)"
                      @input="onShotRefLegendInput(shot, ri, ref, ($event.target as HTMLInputElement).value)"
                      @blur="onShotRefLegendCommit(shot, ri, ref, ($event.target as HTMLInputElement).value)"
                    />
                  </li>
                </ul>
              </div>
            </div>

              <div v-if="isShotDirty(shot.id)" class="shot-edit-actions">
                <span class="shot-edit-hint">本地已修改 · 点击框外会自动保存</span>
                <el-button size="small" text type="warning" @click="revertShotEdits(shot)">
                  撤销修改
                </el-button>
              </div>

              <el-input
                :key="'shot-script-' + shot.id + '-' + (shotScriptEpoch[shot.id] || 0)"
                v-model="shot.script"
                type="textarea"
                class="composer-prompt"
                :rows="6"
                :placeholder="scriptPlaceholder"
                @focus="beginShotEditSession(shot)"
                @input="markShotDirty(shot.id)"
                @change="saveShot(shot)"
              />

              <div class="composer-bar">
                <el-select
                  v-model="shot.videoModelId"
                  size="small"
                  placeholder="视频模型"
                  class="composer-select"
                  @focus="ensureShotModel(shot)"
                  @change="() => { markShotDirty(shot.id); saveShot(shot) }"
                >
                <el-option :value="null" :label="defaultVideoModelLabel" />
                <el-option
                  v-for="m in videoModels"
                  :key="m.id"
                  :value="m.id"
                  :label="videoModelLabel(m)"
                >
                  <div class="video-model-option">
                    <span>
                      {{ providerName(m.providerId) }} · {{ m.name }}
                      <small v-if="m.isDefault"> · 默认</small>
                    </span>
                    <small>{{ m.modelId }}</small>
                  </div>
                </el-option>
              </el-select>
              <div class="composer-duration" title="视频时长 1–30 秒">
                <el-slider
                  v-model="shot.duration"
                  :min="1"
                  :max="30"
                  :step="1"
                  size="small"
                  :show-tooltip="true"
                  :format-tooltip="(v: number) => `${v}s`"
                  @change="() => { markShotDirty(shot.id); saveShot(shot) }"
                />
                <b class="composer-duration-val">{{ shot.duration }}s</b>
              </div>
              <el-select
                v-model="shot.resolution"
                size="small"
                class="composer-select-sm"
                @change="() => { markShotDirty(shot.id); saveShot(shot) }"
              >
                <el-option v-for="r in resolutionOptions" :key="r" :value="r" :label="r" />
              </el-select>
              <el-button size="small" text @click="toggleAdvanced(shot.id)">
                {{ showAdvanced[shot.id] ? '收起高级' : '高级' }}
              </el-button>
              <el-button size="small" text :loading="previewingPrompt === shot.id" @click="previewShotPrompt(shot)">
                预览提示词
              </el-button>
              <el-button
                size="small"
                text
                title="对照质检红线核对，不符合才改"
                :loading="optimizingScripts.has(shot.id)"
                :disabled="generating === shot.id || optimizingScripts.has(shot.id)"
                @click="optimizeShotScript(shot)"
              >
                AI优化文案
              </el-button>
              <el-button size="small" text @click="openPositioningModal(shot)">
                生成站位图
              </el-button>
              <el-button size="small" text @click="openMotionGridModal(shot)">
                生成9帧图
              </el-button>
              <el-button
                size="small"
                text
                :loading="extractingFrame === shot.id"
                :disabled="generating === shot.id || extractingFrame === shot.id"
                @click="extractPreviousFrame(shot)"
              >
                上一镜尾帧
              </el-button>
              <label class="shot-upload-chip" :class="{ disabled: uploadingShot === shot.id || generating === shot.id }">
                <span>{{ uploadingShot === shot.id ? '上传中…' : (shot.videoUrl ? '替换视频' : '上传视频') }}</span>
                <input
                  type="file"
                  accept="video/mp4,video/webm,video/quicktime,.mp4,.webm,.mov,.m4v"
                  :disabled="uploadingShot === shot.id || generating === shot.id"
                  @change="onVideoInput(shot, $event)"
                />
              </label>
              <el-button
                type="primary"
                :loading="generating === shot.id"
                :disabled="uploadingShot === shot.id"
                @click="generateShot(shot)"
              >
                {{ generating === shot.id ? '生成中…' : '生成视频' }}
              </el-button>
            </div>
            <div class="composer-bar composer-bar-extra">
              <el-button
                size="small"
                text
                title="按当前文案重新匹配资源库，站位图/尾帧/9帧图会保留"
                :loading="matchingShotRefs.has(shot.id)"
                :disabled="generating === shot.id || matchingShotRefs.has(shot.id) || optimizingScripts.has(shot.id)"
                @click="rematchShotRefs(shot)"
              >
                重新自动选择参考图
              </el-button>
              <el-button
                size="small"
                text
                title="丢弃本地改动，从服务器重新拉取这一镜"
                @click="reloadShotFromServer(shot)"
              >
                重新加载本镜
              </el-button>
            </div>

            <el-alert v-if="generating === shot.id || shot.status === 'generating'" type="warning" :closable="false" show-icon class="generating-alert">
              <template #title>视频生成中，请勿关闭页面</template>
              {{ generatingWaitHint(shot) }}
            </el-alert>

            <div v-if="showAdvanced[shot.id]" class="composer-advanced-panel">
              <label class="advanced-label">
                画面质感
                <span class="advanced-hint">留空则使用视觉手册的视频风格标签</span>
              </label>
              <el-input
                v-model="shot.visualStyle"
                type="textarea"
                :rows="3"
                :placeholder="visualManualVideoPrompt(active?.visualManual) || active?.style || defaultStyle"
                @input="markShotDirty(shot.id)"
                @change="saveShot(shot)"
              />
            </div>

            <div class="composer-note">
              <label class="advanced-label">备注</label>
              <el-input
                v-model="shot.note"
                type="textarea"
                :rows="2"
                placeholder="可选，折叠时显示在分镜卡片底部"
                maxlength="500"
                show-word-limit
                @input="markShotDirty(shot.id)"
                @change="saveShot(shot)"
              />
            </div>
          </div>

          <el-alert
            v-if="shot.errorMessage && shot.status !== 'generating' && generating !== shot.id"
            :title="shot.errorMessage"
            type="error"
            show-icon
            :closable="false"
            class="shot-error-block"
          />

          <div v-if="shot.videoUrl" class="shot-video-wrap">
            <div class="shot-video-head">
              <div class="shot-video-head-main">
                <b>分镜视频</b>
                <el-tag size="small" type="success" effect="plain">{{ shotUiStatusLabel(shot, true) }}</el-tag>
                <el-button
                  v-if="shot.videoUrl"
                  size="small"
                  :disabled="exportingShotId === shot.id"
                  @click="onExportShotVideo(shot)"
                >
                  {{ exportingShotId === shot.id ? '导出中…' : '导出' }}
                </el-button>
              </div>
              <div class="shot-video-head-meta">
                <span v-if="shotTimeLabel(shot)">{{ shotTimeLabel(shot) }}</span>
                <span>{{ shot.duration }}s</span>
                <span>{{ shot.resolution }}</span>
                <span v-if="shotVideoMeta(shot)?.model">{{ shotVideoMeta(shot)?.model }}</span>
              </div>
            </div>

            <BilibiliVideoPlayer
              :src="shotVideoSrc(shot)"
              :title="shot.label?.trim() || undefined"
            />

            <div v-if="shotVideoScript(shot)" class="bili-info-bar">
              <span class="bili-info-label">文案</span>
              <p class="bili-info-text">{{ shotVideoScript(shot) }}</p>
            </div>

            <div v-if="shotVideoVersions(shot.id).length > 1" class="bili-danmaku-bar">
              <div class="bili-version-inline">
                <span class="version-label">版本</span>
                <el-radio-group
                  :model-value="shot.activeVideoResourceId || shotVideoVersions(shot.id)[0]?.id"
                  size="small"
                  @change="(id: number) => { const v = shotVideoVersions(shot.id).find(x => x.id === id); if (v) useShotVideo(v) }"
                >
                  <el-radio-button
                    v-for="v in shotVideoVersions(shot.id)"
                    :key="v.id"
                    :value="v.id"
                    :disabled="applyingShotVideo === v.id"
                  >
                    {{ v.name }}
                  </el-radio-button>
                </el-radio-group>
              </div>
            </div>
          </div>
        </template>
          </el-card>
          </div>
        </template>

        <div class="shot-insert-row">
          <button
            type="button"
            class="shot-insert-btn"
            title="在末尾插入分镜"
            @click="addShotAt(shotTotal)"
          >
            <span class="shot-insert-icon">+</span>
            <span class="shot-insert-label">插入分镜</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.episodes-panel {
  margin-top: 4px;
}

.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.section-title {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: #eee9e1;
}

.section-sub {
  margin: 4px 0 0;
  font-size: 12px;
  color: #8f8880;
}

.empty-icon {
  font-size: 40px;
  color: #ff785a;
}

.empty-hint {
  margin: 0 0 16px;
  font-size: 13px;
  color: #9a9288;
}

.empty-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px;
}

.episode-tabs {
  margin: 0 0 18px;
}

.shot-card {
  margin-bottom: 12px;
  border-radius: 12px;
  border: 1px solid #2e2a26;
  background: rgba(255, 255, 255, 0.02);
  overflow: hidden;
}

.shot-card.highlight {
  border-color: #ff785a;
  box-shadow: 0 0 0 1px #ff785a;
}

.shot-card :deep(.el-card__body) {
  padding: 0;
}

.shot-card.collapsed :deep(.el-card__body) {
  padding: 0;
}

.shot-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 16px;
  cursor: pointer;
  user-select: none;
}

.shot-chevron {
  color: #8f8880;
  transition: transform 0.2s;
  flex-shrink: 0;
}

.shot-chevron.expanded {
  transform: rotate(90deg);
}

.shot-index {
  font-size: 14px;
  font-weight: 700;
  color: #eee9e1;
  flex-shrink: 0;
}

.shot-label-input {
  width: 120px;
  flex-shrink: 0;
}

.shot-label-input :deep(.el-input__wrapper) {
  background: rgba(255, 120, 90, 0.08);
  border-color: rgba(255, 120, 90, 0.35);
  box-shadow: none;
}

.shot-label-input :deep(.el-input__inner) {
  color: #ff9d85;
  font-weight: 600;
}

.shot-label-input :deep(.el-input__inner::placeholder) {
  color: #8f8880;
  font-weight: 400;
}

.shot-label-tag {
  flex-shrink: 0;
  border-color: rgba(255, 120, 90, 0.45) !important;
  background: rgba(255, 120, 90, 0.12) !important;
  color: #ff9d85 !important;
  font-weight: 600;
}

.shot-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  flex: 1;
  min-width: 0;
  align-items: center;
}

.shot-time {
  font: 11px 'DM Mono', monospace;
  color: #7a736a;
  white-space: nowrap;
}

.shot-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.15s;
}

.shot-card:hover .shot-actions {
  opacity: 1;
}

.shot-move {
  min-width: 28px;
  padding: 0 6px;
  font-size: 14px;
  color: #9a9288;
}

.shot-move:not(:disabled):hover {
  color: #ff9d85;
}

.shot-delete {
  margin-left: 4px;
}

.shot-list {
  display: flex;
  flex-direction: column;
}

.shot-insert-row {
  display: flex;
  justify-content: center;
  margin: 2px 0;
  opacity: 0;
  transition: opacity 0.15s;
}

.shot-list:hover .shot-insert-row,
.shot-insert-row:hover {
  opacity: 1;
}

.shot-insert-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border: 1px dashed #4a4540;
  border-radius: 999px;
  background: transparent;
  color: #8f8880;
  font-size: 12px;
  cursor: pointer;
}

.shot-insert-btn:hover {
  border-color: #ff785a;
  color: #ff9d85;
  background: rgba(255, 120, 90, 0.08);
}

.shot-insert-icon {
  font-size: 14px;
  color: #ff785a;
  line-height: 1;
}

.shot-insert-label {
  font-weight: 600;
}

.shot-pagination {
  display: flex;
  justify-content: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 20px;
  padding-bottom: 8px;
}

.shot-pagination-top {
  margin-top: 0;
  margin-bottom: 12px;
  padding-bottom: 0;
}

.shot-pagination :deep(.el-pagination.is-background .el-pager li) {
  background: #1a1816;
  color: #b9afa5;
}

.shot-pagination :deep(.el-pagination.is-background .el-pager li.is-active) {
  background: rgba(255, 120, 90, 0.2);
  color: #ff9d85;
}

.shot-pagination :deep(.el-pagination__jump) {
  color: #9a9288;
  margin-left: 4px;
}

.shot-pagination :deep(.el-pagination__editor.el-input) {
  width: 56px;
}

.shot-pagination :deep(.el-pagination__editor .el-input__wrapper) {
  background: #1a1816;
  box-shadow: 0 0 0 1px #2e2a26 inset;
}

.shot-pagination :deep(.el-pagination__editor .el-input__inner) {
  color: #e8e0d6;
}

.shot-summary {
  margin: 0;
  padding: 0 16px 6px 42px;
  font-size: 13px;
  color: #9a9288;
  line-height: 1.5;
  overflow: hidden;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  white-space: normal;
  word-break: break-word;
}

.shot-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 0 16px 14px 42px;
}

.shot-meta:has(+ .shot-note) {
  padding-bottom: 6px;
}

.shot-note {
  margin: 0;
  padding: 0 16px 14px 42px;
  font-size: 12px;
  color: #b8a99a;
  line-height: 1.55;
  white-space: pre-wrap;
  word-break: break-word;
}

.shot-note-label {
  display: inline-block;
  margin-right: 8px;
  padding: 1px 6px;
  border-radius: 4px;
  border: 1px solid #4a4038;
  color: #c4b5a5;
  font-size: 11px;
  font-weight: 600;
  vertical-align: baseline;
}

.shot-error-inline,
.shot-error-block {
  margin: 0 16px 12px;
}

.shot-composer {
  border-top: 1px solid #2e2a26;
  padding: 16px;
  background: rgba(0, 0, 0, 0.15);
}

.ref-add-wrap {
  position: relative;
  width: 72px;
  height: 72px;
  flex-shrink: 0;
}

.ref-add-wrap .ref-add {
  width: 100%;
  height: 100%;
}

.ref-add.loading {
  opacity: 0.7;
}

.ref-upload-btn {
  position: absolute;
  right: -4px;
  bottom: -4px;
  display: grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: #302a26;
  border: 1px solid #4a4540;
  color: #ff9d85;
  font-size: 11px;
  font-weight: 800;
  cursor: pointer;
}

.ref-upload-btn.disabled {
  opacity: 0.45;
  pointer-events: none;
}

.ref-upload-btn input {
  position: absolute;
  inset: 0;
  opacity: 0;
  cursor: pointer;
}

.composer-ref-hint {
  width: 100%;
  margin: -4px 0 0;
  font-size: 11px;
  color: #7a736a;
}

.composer-refs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
  align-items: flex-start;
}

.ref-add {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  width: 72px;
  height: 72px;
  border: 1px dashed #4a4540;
  border-radius: 10px;
  background: transparent;
  color: #9a9288;
  cursor: pointer;
}

.ref-add:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.ref-add-icon {
  font-size: 22px;
  color: #ff785a;
  line-height: 1;
}

.ref-add small {
  margin-top: 4px;
  font-size: 10px;
}

.ref-thumb-wrap {
  width: 72px;
}

.ref-thumb-wrap.replacing .ref-thumb {
  outline: 2px solid #c45c3e;
  outline-offset: 1px;
}

.ref-thumb {
  position: relative;
  width: 72px;
  height: 72px;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid #3c3731;
}

.ref-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.shot-ref-legend {
  width: 100%;
  flex-basis: 100%;
  border: 1px solid #3a352f;
  border-radius: 8px;
  background: #161412;
  padding: 10px 12px;
  margin-top: 2px;
}

.shot-ref-legend-title {
  font-size: 12px;
  font-weight: 700;
  color: #b8b0a6;
  margin-bottom: 6px;
}

.shot-ref-legend-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 6px;
}

.shot-ref-legend-list li {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: #ebe4db;
}

.shot-ref-legend-prefix {
  flex-shrink: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: #b8b0a6;
}

.shot-ref-legend-input {
  flex: 1;
  min-width: 0;
  border: 1px solid #3a352f;
  border-radius: 4px;
  background: #221f1c;
  color: #ebe4db;
  font-size: 12px;
  line-height: 1.4;
  padding: 4px 8px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.shot-ref-legend-input:hover {
  border-color: #5a524a;
}

.shot-ref-legend-input:focus {
  outline: none;
  border-color: #c45c3e;
}

.ref-tag {
  position: absolute;
  left: 4px;
  bottom: 4px;
  background: rgba(0, 0, 0, 0.75);
  color: #ff9d85;
  font-size: 9px;
  padding: 2px 5px;
  border-radius: 4px;
  font-family: 'DM Mono', monospace;
}

.ref-replace,
.ref-remove {
  position: absolute;
  top: 2px;
  border: 0;
  border-radius: 4px;
  font-size: 10px;
  cursor: pointer;
  background: rgba(0, 0, 0, 0.75);
  color: #eee9e1;
  padding: 2px 5px;
}

.ref-replace { left: 2px; color: #9fd4a8; }
.ref-remove { right: 2px; color: #ffb6a6; width: 18px; height: 18px; border-radius: 50%; }

.composer-prompt :deep(.el-textarea__inner) {
  font-family: 'DM Mono', monospace;
  font-size: 13px;
  line-height: 1.65;
  background: #151311;
  border-color: #3c3731;
}

.composer-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
}

.composer-bar-extra {
  margin-top: 4px;
}

.composer-select {
  width: 280px;
}

.video-model-option {
  display: grid;
  gap: 2px;
  line-height: 1.35;
  padding: 2px 0;
}

.video-model-option > small {
  font: 11px 'DM Mono', monospace;
  color: #938a80;
}

.composer-select-sm {
  width: 88px;
}

.composer-duration {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 168px;
  padding: 0 4px;
}

.composer-duration :deep(.el-slider) {
  flex: 1;
  min-width: 0;
}

.composer-duration-val {
  flex: none;
  min-width: 2.2em;
  font: 12px 'DM Mono', monospace;
  color: #e8e0d6;
  text-align: right;
}

.shot-upload-chip {
  position: relative;
  display: inline-flex;
  align-items: center;
  padding: 5px 12px;
  font-size: 12px;
  color: #b9afa5;
  border: 1px solid #3c3731;
  border-radius: 6px;
  cursor: pointer;
}

.shot-upload-chip.disabled {
  opacity: 0.5;
  pointer-events: none;
}

.shot-upload-chip input {
  position: absolute;
  inset: 0;
  opacity: 0;
  cursor: pointer;
}

.generating-alert {
  margin-top: 12px;
}

.composer-advanced-panel {
  margin-top: 12px;
}

.composer-note {
  margin-top: 12px;
}

.advanced-label {
  display: block;
  margin-bottom: 8px;
  font-size: 12px;
  font-weight: 600;
  color: #b9afa5;
}

.advanced-hint {
  display: block;
  font-weight: 400;
  font-size: 11px;
  color: #8f8880;
  margin-top: 2px;
}

.shot-video-wrap {
  margin: 0 16px 16px;
  border: 1px solid #2e2a26;
  border-radius: 10px;
  overflow: hidden;
  background: #111;
}

.shot-video-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 16px;
  padding: 10px 14px;
  border-bottom: 1px solid #2e2a26;
  background: #181614;
}

.shot-video-head-main {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.shot-video-head-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  font-size: 12px;
  color: #8f8880;
}

.bili-info-bar {
  padding: 10px 14px;
  border-top: 1px solid #2e2a26;
  background: #151311;
}

.bili-info-label {
  display: inline-block;
  font-size: 11px;
  color: #aaa197;
  font-weight: 700;
  margin-bottom: 4px;
}

.bili-info-text {
  margin: 0;
  font-size: 12px;
  line-height: 1.55;
  color: #b9afa5;
  white-space: pre-wrap;
  max-height: 72px;
  overflow: auto;
}

.bili-danmaku-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 14px;
  border-top: 1px solid #2e2a26;
  background: #1a1816;
  color: #b9afa5;
}

.bili-danmaku-hint {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bili-version-inline {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.bili-version-inline :deep(.el-radio-button__inner) {
  background: #211e1b;
  border-color: #3c3731;
  color: #b9afa5;
  font-size: 12px;
  padding: 6px 10px;
}

.bili-version-inline :deep(.el-radio-button__original-radio:checked + .el-radio-button__inner) {
  background: rgba(255, 120, 90, 0.18);
  border-color: #ff785a;
  color: #ff9d85;
  box-shadow: none;
}

.shot-version-picker {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-top: 1px solid #2e2a26;
}

.version-label {
  font-size: 12px;
  color: #8f8880;
}
</style>

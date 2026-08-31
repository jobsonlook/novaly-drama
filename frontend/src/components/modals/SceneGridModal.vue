<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  sceneGridModal,
  sceneGridRefs,
  maxSceneGridRefs,
  imageModelId,
  imageModelsByProvider,
  defaultImageModelLabel,
  imageModelLabel,
  imageResolution,
  imageResolutionOptions,
  closeSceneGridModal,
  confirmSceneGridGenerate,
  generateSceneGridOverhead,
  reanalyzeSceneGridShapeLegend,
  onSceneGridOverheadFile,
  clearSceneGridOverhead,
  openSceneGridOverheadPicker,
  refillSceneGridPrompt,
  openSceneGridRefPicker,
  openSceneGridReplacePicker,
  onSceneGridRefFiles,
  onSceneGridRefPaste,
  removeSceneGridReference,
  clearSceneGridReferences,
  openImagePreview,
  sceneReferenceKey,
} = useNovalyInject()

const overheadImgPending = ref(0)
const overheadPreviewLoading = ref(false)
let overheadImgToken = 0

function collectOverheadPreviewUrls() {
  const modal = sceneGridModal.value
  if (!modal) return [] as string[]
  const urls = new Set<string>()
  modal.overheadSketchCandidates?.forEach(c => {
    if (c?.url) urls.add(c.url)
  })
  if (modal.overheadSketch?.url) urls.add(modal.overheadSketch.url)
  return [...urls]
}

function preloadOverheadPreviewImages() {
  const urls = collectOverheadPreviewUrls()
  const token = ++overheadImgToken
  if (!urls.length) {
    overheadImgPending.value = 0
    overheadPreviewLoading.value = false
    return
  }
  overheadImgPending.value = urls.length
  overheadPreviewLoading.value = true
  urls.forEach((url) => {
    const img = new Image()
    const done = () => {
      if (token !== overheadImgToken) return
      overheadImgPending.value = Math.max(0, overheadImgPending.value - 1)
      if (overheadImgPending.value === 0) overheadPreviewLoading.value = false
    }
    img.onload = done
    img.onerror = done
    img.src = url
  })
}

watch(sceneGridModal, (modal) => {
  if (!modal) {
    overheadImgToken++
    overheadImgPending.value = 0
    overheadPreviewLoading.value = false
  }
})

watch(
  () => [
    sceneGridModal.value?.overheadSketch?.url,
    sceneGridModal.value?.overheadSketch?.resourceId,
    sceneGridModal.value?.overheadSketchCandidates?.map(c => `${c.resourceId || ''}:${c.url || ''}`).join('|') || '',
  ],
  () => preloadOverheadPreviewImages(),
  { immediate: true },
)

const hasOverheadPreview = computed(() =>
  !!sceneGridModal.value?.overheadSketch?.url
  || !!sceneGridModal.value?.overheadSketchCandidates?.length,
)

const showOverheadLoading = computed(() =>
  !!sceneGridModal.value?.overheadSubmitting
  || overheadPreviewLoading.value
  || overheadImgPending.value > 0,
)

const overheadLoadingText = computed(() => {
  if (sceneGridModal.value?.overheadSubmitting) return '正在生成二维建筑平面布局图，请稍候…'
  if (hasOverheadPreview.value && (overheadPreviewLoading.value || overheadImgPending.value > 0)) {
    return '平面图已生成，正在加载预览…'
  }
  return '正在处理平面图，请稍候…'
})

const prompt = computed({
  get: () => sceneGridModal.value?.prompt || '',
  set: (v: string) => {
    if (sceneGridModal.value) sceneGridModal.value = { ...sceneGridModal.value, prompt: v }
  },
})

const name = computed({
  get: () => sceneGridModal.value?.name || '',
  set: (v: string) => {
    if (sceneGridModal.value) sceneGridModal.value = { ...sceneGridModal.value, name: v }
  },
})

const usingLegacyPrompt = computed(() =>
  /同一建筑连续摄影|ArchViz|屋顶结构|【建筑主体】|【九宫格摄影机矩阵】|同一空间连续摄影/.test(prompt.value)
  && !/瓶口朝镜头/.test(prompt.value),
)

function pickSceneGridOverheadSketch(candidate: any) {
  if (!sceneGridModal.value) return
  sceneGridModal.value = { ...sceneGridModal.value, overheadSketch: candidate }
}

function isSelectedOverheadSketch(candidate: any) {
  const selected = sceneGridModal.value?.overheadSketch
  if (!selected) return false
  if (candidate?.resourceId && selected.resourceId && candidate.resourceId === selected.resourceId) return true
  return candidate?.url && candidate.url === selected.url
}
</script>

<template>
  <el-dialog
    :model-value="!!sceneGridModal"
    title="生成场景9宫格"
    width="780px"
    class="modal-wide"
    align-center
    :close-on-click-modal="false"
    @close="closeSceneGridModal"
  >
    <div v-if="sceneGridModal" class="pos-form" @paste="onSceneGridRefPaste">
      <p class="pos-hint">
        以场景原图为参考，一次生成同一空间的 9 个机位（正面/侧面/反打/俯视）。第三行必须是天花板往下看，能看见瓶口和地板；如果九格都是平视桌面就重出。
      </p>
      <p v-if="usingLegacyPrompt" class="pos-warn">
        这还是旧模板，九格容易都变成同一张平视桌面。请点「填入室内模板」后再生成。第三行必须是天花板俯视。
      </p>

      <div v-if="sceneGridModal.results?.length" class="result-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">AI 合成结果（{{ sceneGridModal.results.length }}）</span>
        </div>
        <p class="ref-zone-hint">本次任务已生成的9宫格，点击可放大查看；资源卡片上可再执行「切分9格」</p>
        <div class="scene-ref-grid compact results">
          <div
            v-for="(img, i) in sceneGridModal.results"
            :key="img.resourceId || img.url || i"
            class="scene-ref-card result-card"
          >
            <img
              :src="img.url"
              :alt="img.label || `候选 ${i + 1}`"
              class="zoomable"
              @click="openImagePreview(img.url, img.label || `候选 ${i + 1}`)"
            />
            <div class="scene-ref-preview-meta">
              <span>{{ img.label || `候选 ${i + 1}` }}</span>
            </div>
          </div>
        </div>
      </div>

      <section class="pos-section">
        <div class="pos-section-head">
          <span class="pos-label-text">场景名称</span>
        </div>
        <el-input v-model="name" placeholder="场景名称，如：麦城山口" :disabled="sceneGridModal.submitting" />
      </section>

      <section class="pos-section overhead-stage">
        <div class="pos-section-head">
          <div>
            <span class="pos-label-text">第一步：确认二维建筑平面布局图</span>
            <div class="pos-mini-hint">
              平面图应是白底黑线 CAD，不是实景空镜。先核对门窗/桌椅/通道方位；候选多张时优先选门洞墙面一致的；不对可重生成或上传线稿替换。
            </div>
          </div>
          <div class="ref-zone-actions">
            <button
              type="button"
              class="ref-action-btn"
              :disabled="!!sceneGridModal.submitting || !!sceneGridModal.overheadSubmitting"
              @click="openSceneGridOverheadPicker"
            >
              资源库
            </button>
            <label class="ref-action-btn">
              上传替换
              <input type="file" accept="image/*" @change="onSceneGridOverheadFile" />
            </label>
            <label class="pos-quality-select overhead-candidate-count">
              候选数量
              <el-select
                v-model.number="sceneGridModal.overheadSketchCandidateCount"
                size="small"
                style="width: 88px"
                :disabled="!!sceneGridModal.submitting || !!sceneGridModal.overheadSubmitting"
              >
                <el-option v-for="n in 6" :key="n" :value="n" :label="`${n}`" />
              </el-select>
              <span class="overhead-candidate-unit">张</span>
            </label>
            <button
              type="button"
              class="ref-action-btn"
              :disabled="!!sceneGridModal.submitting || !!sceneGridModal.overheadSubmitting"
              @click="generateSceneGridOverhead"
            >
              {{ sceneGridModal.overheadSketch ? '重新生成平面图' : '生成二维平面图' }}
            </button>
          </div>
        </div>
        <div class="overhead-legend-edit">
          <div class="overhead-legend-head">
            <div class="overhead-legend-title">图形语义标注（会带入平面图生成和9宫格提示词）</div>
            <button
              type="button"
              class="ref-action-btn"
              :disabled="!!sceneGridModal.submitting || !!sceneGridModal.overheadSubmitting || !!sceneGridModal.overheadShapeLegendAnalyzing"
              @click="reanalyzeSceneGridShapeLegend"
            >
              {{ sceneGridModal.overheadShapeLegendAnalyzing ? '分析中…' : '重新分析物体' }}
            </button>
          </div>
          <div v-if="sceneGridModal.overheadShapeLegendAnalyzing" class="overhead-legend-loading">
            正在根据场景描述分析物体与平面符号对照…
          </div>
          <el-input
            v-model="sceneGridModal.overheadShapeLegend"
            type="textarea"
            :rows="5"
            :disabled="!!sceneGridModal.submitting || !!sceneGridModal.overheadSubmitting || !!sceneGridModal.overheadShapeLegendAnalyzing"
            placeholder="打开弹窗后会自动分析；也可手动编辑或点「重新分析物体」。"
          />
        </div>
        <div v-if="showOverheadLoading && !hasOverheadPreview" class="overhead-loading">
          <span class="overhead-loading-spinner" aria-hidden="true" />
          <span>{{ overheadLoadingText }}</span>
        </div>
        <div v-else-if="hasOverheadPreview" class="overhead-preview" :class="{ 'is-loading': showOverheadLoading }">
          <div v-if="showOverheadLoading" class="overhead-loading-overlay">
            <span class="overhead-loading-spinner" aria-hidden="true" />
            <span>{{ overheadLoadingText }}</span>
          </div>
          <div v-if="sceneGridModal.overheadSketchCandidates?.length" class="overhead-candidate-grid">
            <button
              v-for="(c, idx) in sceneGridModal.overheadSketchCandidates"
              :key="c.resourceId || c.url || idx"
              type="button"
              class="overhead-candidate-btn"
              :class="{ on: isSelectedOverheadSketch(c) }"
              @click="pickSceneGridOverheadSketch(c)"
            >
              <img class="overhead-candidate-img" :src="c.url" :alt="`二维平面图候选 ${idx + 1}`" />
            </button>
          </div>
          <div v-if="sceneGridModal.overheadSketchCandidates?.length" class="overhead-pick-hint">
            提示：门洞开口最关键——必须在同一面墙、开口朝向尽量一致；再核对桌椅/沙发/通道的平面方位。
          </div>
          <img
            v-if="sceneGridModal.overheadSketch?.url"
            :src="sceneGridModal.overheadSketch.url"
            alt="二维建筑平面布局图（已选）"
            class="zoomable"
            @click="openImagePreview(sceneGridModal.overheadSketch.url, '二维建筑平面布局图（已选）')"
          />
          <button type="button" class="ref-remove-btn overlay" title="移除" @click="clearSceneGridOverhead">×</button>
          <span v-if="!showOverheadLoading">已作为九宫格空间方位参考</span>
        </div>
        <div v-else class="ref-zone-empty">尚未确认二维建筑平面布局图，暂不能生成9宫格</div>
      </section>

      <section class="pos-section">
        <div class="pos-section-head">
          <span class="pos-label-text">第二步：生成9宫格</span>
          <span class="pos-mini-hint">含场景锁定 + 室内环绕机位。点任务打开旧结果时会自动换成新模板。</span>
          <button
            type="button"
            class="ref-action-btn"
            :disabled="!!sceneGridModal.submitting"
            @click="refillSceneGridPrompt"
          >
            填入室内模板
          </button>
        </div>
        <el-input
          v-model="prompt"
          type="textarea"
          :rows="sceneGridModal.results?.length ? 8 : 14"
          :disabled="sceneGridModal.submitting"
          placeholder="9宫格提示词模板已自动填充，可逐段编辑"
        />
      </section>

      <div class="pos-prompt-actions">
        <label class="pos-quality-select">
          模型
          <el-select
            v-model="imageModelId"
            size="small"
            clearable
            filterable
            placeholder="默认"
            style="width: 260px"
            :disabled="sceneGridModal.submitting"
          >
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
        <label class="pos-quality-select">
          分辨率
          <el-select v-model="imageResolution" size="small" style="width: 88px" :disabled="sceneGridModal.submitting">
            <el-option
              v-for="opt in imageResolutionOptions"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label"
            />
          </el-select>
        </label>
        <span class="pos-res-hint">整图固定 16:9，不跟项目竖屏走。建议 2K/4K，切格后每格仍有可用像素</span>
      </div>

      <div class="ref-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">参考图（{{ sceneGridRefs.length }}/{{ maxSceneGridRefs }}）</span>
          <div class="ref-zone-actions">
            <label class="ref-action-btn">
              上传
              <input type="file" accept="image/*" multiple @change="onSceneGridRefFiles" />
            </label>
            <button type="button" class="ref-action-btn" @click="openSceneGridRefPicker">资源库</button>
            <button
              v-if="sceneGridRefs.length"
              type="button"
              class="ref-action-btn muted"
              @click="clearSceneGridReferences"
            >
              清空
            </button>
          </div>
        </div>
        <p class="ref-zone-hint">已自动带入场景原图，用来锁定这个房间/场地的材质与陈设。九宫格会补拍侧面、背面和俯视，不必再挂很多张。若原图是桌面特写，建议再补一张能看见整间屋子的空镜。</p>
        <div v-if="sceneGridRefs.length" class="scene-ref-grid compact">
          <div v-for="(ref, ri) in sceneGridRefs" :key="sceneReferenceKey(ref)" class="scene-ref-card">
            <div class="scene-ref-thumb">
              <img
                :src="ref.previewUrl"
                :alt="ref.label"
                class="zoomable"
                @click="openImagePreview(ref.previewUrl, ref.label)"
              />
              <button
                type="button"
                class="ref-remove-btn overlay"
                title="移除"
                @click="removeSceneGridReference(sceneReferenceKey(ref))"
              >
                ×
              </button>
            </div>
            <div class="scene-ref-preview-meta">
              <span class="ref-meta-label">图{{ ri + 1 }} · {{ ref.label }}</span>
              <button
                type="button"
                class="ref-replace-btn"
                :disabled="!!sceneGridModal.submitting"
                @click="openSceneGridReplacePicker(ri)"
              >
                替换
              </button>
            </div>
          </div>
        </div>
        <div v-else class="ref-zone-empty">暂无参考图：建议至少保留场景原图</div>
      </div>
    </div>
    <template #footer>
      <el-button :disabled="!!sceneGridModal?.submitting" @click="closeSceneGridModal">取消</el-button>
      <el-button
        type="primary"
        :loading="!!sceneGridModal?.submitting"
        :disabled="!sceneGridModal?.prompt.trim() || !sceneGridModal?.name.trim() || !sceneGridModal?.overheadSketch || !!sceneGridModal?.overheadSubmitting"
        @click="confirmSceneGridGenerate"
      >
        生成9宫格
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.pos-form {
  display: grid;
  gap: 14px;
}

.pos-hint {
  margin: 0;
  font-size: 13px;
  color: #9a9288;
  line-height: 1.6;
}

.pos-warn {
  margin: 0;
  font-size: 13px;
  color: #e8b949;
  line-height: 1.55;
}

.pos-section {
  display: grid;
  gap: 8px;
}

.overhead-stage {
  padding: 12px;
  border: 1px solid #4c443a;
  border-radius: 10px;
  background: #1b1815;
}

.overhead-legend-edit {
  display: grid;
  gap: 6px;
}

.overhead-legend-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.overhead-legend-loading {
  font-size: 12px;
  color: #b6aa9c;
}

.overhead-legend-title {
  font-size: 12px;
  color: #a99f93;
}

.overhead-loading,
.overhead-preview {
  min-height: 110px;
  border: 1px dashed #51483e;
  border-radius: 8px;
}

.overhead-loading {
  display: grid;
  place-items: center;
  gap: 10px;
  color: #b6aa9c;
  font-size: 13px;
}

.overhead-loading-spinner {
  width: 22px;
  height: 22px;
  border: 2px solid #51483e;
  border-top-color: #c9a96e;
  border-radius: 50%;
  animation: overhead-spin 0.8s linear infinite;
}

.overhead-preview.is-loading {
  min-height: 180px;
}

.overhead-loading-overlay {
  position: absolute;
  inset: 0;
  z-index: 2;
  display: grid;
  place-items: center;
  gap: 10px;
  border-radius: 8px;
  background: rgba(20, 18, 16, 0.82);
  color: #d8cfc4;
  font-size: 13px;
}

@keyframes overhead-spin {
  to { transform: rotate(360deg); }
}

.overhead-preview {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 10px;
  color: #9fce83;
  font-size: 12px;
}

.overhead-preview img {
  width: 190px;
  max-height: 130px;
  object-fit: contain;
  border-radius: 6px;
  background: #fff;
}

.overhead-candidate-grid {
  width: 100%;
  display: flex;
  justify-content: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 4px;
}

.overhead-candidate-btn {
  border: 1px solid #3a352f;
  background: transparent;
  padding: 0;
  border-radius: 8px;
  cursor: pointer;
}

.overhead-candidate-btn.on {
  border-color: #f56c6c;
}

.overhead-candidate-img {
  width: 120px !important;
  max-height: 85px !important;
}

.overhead-selected {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
}

.overhead-pick-hint {
  width: 100%;
  text-align: center;
  font-size: 12px;
  color: #8a8278;
  line-height: 1.4;
  margin-top: 2px;
}

.pos-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.pos-label-text {
  font-size: 13px;
  font-weight: 700;
  color: #d3ccc2;
}

.pos-mini-hint {
  font-size: 11px;
  color: #8a8278;
}

.pos-prompt-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.pos-quality-select {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 600;
  color: #9a9288;
}

.pos-res-hint {
  font-size: 12px;
  color: #8a8278;
}

.ref-zone {
  border: 1px solid #3a352f;
  border-radius: 10px;
  padding: 12px;
  background: #1a1816;
}

.result-zone {
  border: 1px solid #4a3a28;
  border-radius: 10px;
  padding: 12px;
  background: #1f1a14;
}

.result-card img {
  aspect-ratio: 16 / 9;
}

.ref-zone-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 6px;
}

.ref-zone-title {
  font-size: 13px;
  font-weight: 700;
  color: #ebe4db;
}

.ref-zone-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.ref-action-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 4px 10px;
  border: 1px solid #3a352f;
  border-radius: 6px;
  background: #221f1c;
  color: #d3ccc2;
  font-size: 12px;
  cursor: pointer;
  position: relative;
  overflow: hidden;
}

.ref-action-btn input {
  position: absolute;
  inset: 0;
  opacity: 0;
  cursor: pointer;
}

.ref-action-btn.muted {
  color: #9a9288;
}

.ref-zone-hint {
  margin: 0 0 10px;
  font-size: 12px;
  color: #8a8278;
}

.ref-zone-empty {
  padding: 18px 12px;
  border: 1px dashed #3a352f;
  border-radius: 8px;
  text-align: center;
  color: #8a8278;
  font-size: 13px;
}

.scene-ref-grid.compact {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(132px, 1fr));
  gap: 8px;
}

.scene-ref-card {
  position: relative;
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid #3a352f;
  background: #221f1c;
}

.scene-ref-thumb {
  position: relative;
}

.scene-ref-card img {
  display: block;
  width: 100%;
  aspect-ratio: 16 / 10;
  object-fit: cover;
}

.scene-ref-preview-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 6px;
  font-size: 11px;
  color: #b8b0a6;
}

.ref-meta-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ref-replace-btn {
  flex-shrink: 0;
  border: 1px solid #4a433c;
  border-radius: 4px;
  background: #2a2622;
  color: #d3ccc2;
  font-size: 11px;
  padding: 2px 6px;
  cursor: pointer;
}

.ref-replace-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.ref-remove-btn {
  border: none;
  background: transparent;
  color: #c45c3e;
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
}

.ref-remove-btn.overlay {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: rgba(20, 16, 12, 0.72);
  color: #f0a090;
  font-size: 16px;
}
</style>

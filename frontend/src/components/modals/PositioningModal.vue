<script setup lang="ts">
import { computed } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  positioningModal,
  positioningRefs,
  maxPositioningRefs,
  imageModelId,
  imageModels,
  imageModelsByProvider,
  defaultImageModelLabel,
  imageModelLabel,
  imageResolution,
  imageResolutionOptions,
  closePositioningModal,
  reanalyzePositioningPrompt,
  confirmPositioningSkeleton,
  positioningSkeletonSceneRef,
  confirmPositioningGenerate,
  openPositioningRefPicker,
  openPositioningReplacePicker,
  openPositioningSkeletonPicker,
  onPositioningRefFiles,
  onPositioningSkeletonFile,
  onPositioningRefPaste,
  removePositioningReference,
  clearPositioningReferences,
  renamePositioningRef,
  updatePositioningRefLabel,
  setPositioningPromptBody,
  positioningPromptBody,
  openDirectorDeskForPositioning,
  openImagePreview,
  sceneReferenceKey,
} = useNovalyInject()

const promptBody = computed({
  get: () => positioningPromptBody(),
  set: (v: string) => setPositioningPromptBody(v),
})

const skeletonSceneHint = computed(() => {
  const ref = positioningSkeletonSceneRef()
  if (!ref) {
    return '当前没有场景九宫格格子或空镜，骨架会在纯白背景上画。有九宫格的话会按对应机位的格子描房间。'
  }
  return `骨架将按「${ref.label}」描房间结构（九宫格单格/空镜），人物仍是火柴人，不会用角色定妆图。`
})
</script>

<template>
  <el-dialog
    :model-value="!!positioningModal"
    title="生成场景站位图"
    width="780px"
    class="modal-wide"
    align-center
    :close-on-click-modal="false"
    @close="closePositioningModal"
  >
    <div v-if="positioningModal" class="pos-form" @paste="onPositioningRefPaste">
      <p class="pos-hint">
        根据「{{ positioningModal.shotLabel || '当前分镜' }}」文案生成站位图；分析时会联合参考前 12 镜与后 4 镜，优先提取最近的同场分镜来锁定位置。
        多人用九格站位：韩铮(左前)3/4正面朝右；阿彪(右中)3/4正面朝左。同场不要跳轴。
        可先点「3D全景摆位」：以场景全景为背景在导演台摆人，截图回填为骨架；或走 AI「生成火柴人骨架」。骨架对了再点「骨架符合，生成站位图」。
        有场景九宫格时，AI 骨架会用对应机位的格子当空间底板（没有格子再用空镜）。
      </p>

      <div v-if="positioningModal.initializing" class="pos-fixed-rule">
        弹窗已打开，正在后台保存分镜并加载可用参考图，不影响先查看或编辑站位提示词…
      </div>

      <div v-if="positioningModal.skeleton" class="result-zone skeleton-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">火柴人骨架（请核对）</span>
          <div class="ref-zone-actions">
            <label class="ref-action-btn">
              上传替换
              <input type="file" accept="image/*" @change="onPositioningSkeletonFile" />
            </label>
            <button type="button" class="ref-action-btn" @click="openPositioningSkeletonPicker">从资源库替换</button>
          </div>
        </div>
        <p class="ref-zone-hint">看人数、左右、前后、谁坐谁站是否和文案一致；桌子门窗位置应跟场景底板一致。不对就重新生成骨架，对了再生成正式站位图。</p>
        <div class="scene-ref-grid compact results">
          <div class="scene-ref-card result-card">
            <img
              :src="positioningModal.skeleton.url"
              alt="火柴人骨架"
              class="zoomable"
              @click="openImagePreview(positioningModal.skeleton.url, '火柴人骨架')"
            />
            <div class="scene-ref-preview-meta">
              <span>火柴人站位骨架</span>
            </div>
          </div>
        </div>
      </div>

      <div v-if="positioningModal.results?.length" class="result-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">AI 合成结果（{{ positioningModal.results.length }}）</span>
        </div>
        <p class="ref-zone-hint">本次任务已生成的站位图，点击可放大查看</p>
        <div class="scene-ref-grid compact results">
          <div
            v-for="(img, i) in positioningModal.results"
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
          <span class="pos-label-text">站位提示词</span>
          <el-button
            size="small"
            :loading="positioningModal.analyzing"
            :disabled="positioningModal.submitting"
            @click="reanalyzePositioningPrompt"
          >
            {{ positioningModal.analyzing ? '分析中…' : (promptBody.trim() ? '重新分析文案' : '分析文案') }}
          </el-button>
        </div>
        <div class="pos-fixed-rule">
          黄色说明只给操作员看，不会发给模型。发给模型的是下面正文 + 文首「骨架优先（按图1）」+ 文末马赛克要求。点任务面板打开完成后的任务，可在下方看到上次实际提示词。
        </div>
        <el-input
          v-model="promptBody"
          type="textarea"
          :rows="(positioningModal.results?.length || positioningModal.skeleton) ? 5 : 8"
          :disabled="positioningModal.analyzing || positioningModal.submitting"
          :placeholder="positioningModal.analyzing ? '正在联合分析前后分镜与同场站位连续性…' : '点击「分析文案」根据当前镜及前后同场分镜生成站位提示词，或自行填写'"
        />
        <div v-if="positioningRefs.length" class="pos-legend">
          <div class="pos-legend-title">参考图对应（可改名称）</div>
          <ul class="pos-legend-list">
            <li v-for="(ref, i) in positioningRefs" :key="sceneReferenceKey(ref)">
              <span class="pos-legend-prefix">图{{ i + 1 }}为</span>
              <input
                class="pos-legend-input"
                type="text"
                :value="ref.label"
                :disabled="!!positioningModal.analyzing || !!positioningModal.submitting"
                @change="renamePositioningRef(i, ($event.target as HTMLInputElement).value)"
              />
            </li>
          </ul>
        </div>
        <div v-else class="pos-legend empty">
          分析文案后会按角色/场景名自动从资源库选择参考图；也可先手动添加，「图N为谁」名称可改
        </div>
        <div v-if="positioningModal.lastSentPrompt" class="pos-sent-prompt">
          <div class="pos-legend-title">上次实际发给模型的提示词</div>
          <pre class="pos-sent-prompt-body">{{ positioningModal.lastSentPrompt }}</pre>
        </div>
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
            :disabled="positioningModal.submitting"
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
          <el-select v-model="imageResolution" size="small" style="width: 88px" :disabled="positioningModal.submitting">
            <el-option
              v-for="opt in imageResolutionOptions"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label"
            />
          </el-select>
        </label>
      </div>

      <div class="ref-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">参考图（{{ positioningRefs.length }}/{{ maxPositioningRefs }}）</span>
          <div class="ref-zone-actions">
            <label class="ref-action-btn">
              上传
              <input type="file" accept="image/*" multiple @change="onPositioningRefFiles" />
            </label>
            <button type="button" class="ref-action-btn" @click="openPositioningRefPicker">资源库</button>
            <button
              v-if="positioningRefs.length"
              type="button"
              class="ref-action-btn muted"
              @click="clearPositioningReferences"
            >
              清空
            </button>
          </div>
        </div>
        <p class="ref-zone-hint">建议挂 1 张场景空镜或九宫格格子 + 出场角色真人原图。生成骨架只用场景底板；正式站位图为三人以上时只发送已确认骨架 + 1 张场景图，避免多张定妆图抢掉骨架人数。请先删掉旧的错误站位图。</p>
        <p class="ref-zone-hint">{{ skeletonSceneHint }}</p>
        <div v-if="positioningRefs.length" class="scene-ref-grid compact">
          <div v-for="(ref, ri) in positioningRefs" :key="sceneReferenceKey(ref)" class="scene-ref-card">
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
                @click="removePositioningReference(sceneReferenceKey(ref))"
              >
                ×
              </button>
            </div>
            <div class="scene-ref-preview-meta">
              <span class="ref-meta-label">图{{ ri + 1 }} · {{ ref.label }}{{ ref.variant === 'original' ? ' · 真人' : ref.variant === 'stylized' ? ' · 非真人' : '' }}</span>
              <button
                type="button"
                class="ref-replace-btn"
                :disabled="!!positioningModal.analyzing || !!positioningModal.submitting"
                @click="openPositioningReplacePicker(ri)"
              >
                替换
              </button>
            </div>
          </div>
        </div>
        <div v-else class="ref-zone-empty">暂无参考图：点「分析文案」自动匹配，或从资源库选择 / 上传</div>
      </div>
    </div>
    <template #footer>
      <el-button :disabled="!!positioningModal?.submitting" @click="closePositioningModal">取消</el-button>
      <el-button
        :disabled="!!positioningModal?.analyzing || !!positioningModal?.submitting"
        @click="openDirectorDeskForPositioning()"
      >
        3D全景摆位
      </el-button>
      <el-button
        :loading="positioningModal?.submittingStep === 'skeleton'"
        :disabled="!!positioningModal?.analyzing || !!positioningModal?.submitting || !positioningModal?.prompt.trim()"
        @click="confirmPositioningSkeleton"
      >
        {{ positioningModal?.skeleton ? '重新生成骨架' : '生成火柴人骨架' }}
      </el-button>
      <el-button
        type="primary"
        :loading="positioningModal?.submittingStep === 'final'"
        :disabled="!!positioningModal?.analyzing || !!positioningModal?.submitting || !positioningModal?.skeleton?.url"
        @click="confirmPositioningGenerate"
      >
        {{ positioningModal?.submittingStep === 'final' ? '正在准备并提交…' : '骨架符合，生成站位图' }}
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

.pos-section {
  display: grid;
  gap: 8px;
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

.pos-fixed-rule {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid #4a3f2a;
  border-radius: 8px;
  background: linear-gradient(180deg, rgba(196, 148, 60, 0.14), rgba(22, 20, 18, 0.9));
  color: #e6d3a8;
  font-size: 12px;
  line-height: 1.55;
}

.pos-legend {
  border: 1px solid #3a352f;
  border-radius: 8px;
  background: #161412;
  padding: 10px 12px;
}

.pos-legend.empty {
  font-size: 12px;
  color: #8a8278;
}

.pos-legend-title {
  font-size: 12px;
  font-weight: 700;
  color: #b8b0a6;
  margin-bottom: 6px;
}

.pos-sent-prompt {
  border: 1px solid #3a352f;
  border-radius: 8px;
  background: #12100e;
  padding: 10px 12px;
}

.pos-sent-prompt-body {
  margin: 0;
  max-height: 180px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 11px;
  line-height: 1.5;
  color: #c8c0b6;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.pos-legend-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 6px;
}

.pos-legend-list li {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  line-height: 1.5;
  color: #ebe4db;
}

.pos-legend-prefix {
  flex-shrink: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  color: #b8b0a6;
}

.pos-legend-input {
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

.pos-legend-input:focus {
  outline: none;
  border-color: #6a5a48;
}

.pos-legend-input:disabled {
  opacity: 0.6;
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

.skeleton-zone {
  border-color: #6a5a38;
  background: #1c1a14;
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

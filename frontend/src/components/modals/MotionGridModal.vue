<script setup lang="ts">
import { computed } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  motionGridModal,
  motionGridRefs,
  motionGridAnchor,
  maxMotionGridRefs,
  imageModelId,
  imageModelsByProvider,
  defaultImageModelLabel,
  imageModelLabel,
  imageResolution,
  imageResolutionOptions,
  closeMotionGridModal,
  reanalyzeMotionGridPrompt,
  confirmMotionGridGenerate,
  openMotionGridRefPicker,
  openMotionGridReplacePicker,
  onMotionGridRefFiles,
  onMotionGridRefPaste,
  removeMotionGridReference,
  clearMotionGridReferences,
  renameMotionGridRef,
  updateMotionGridRefLabel,
  setMotionGridPromptBody,
  motionGridPromptBody,
  openImagePreview,
  sceneReferenceKey,
} = useNovalyInject()

const promptBody = computed({
  get: () => motionGridPromptBody(),
  set: (v: string) => setMotionGridPromptBody(v),
})

const anchorKey = computed(() => motionGridAnchor.value?.key || '')
</script>

<template>
  <el-dialog
    :model-value="!!motionGridModal"
    title="生成9帧连续画面"
    width="780px"
    class="modal-wide"
    align-center
    :close-on-click-modal="false"
    @close="closeMotionGridModal"
  >
    <div v-if="motionGridModal" class="pos-form" @paste="onMotionGridRefPaste">
      <p class="pos-hint">
        根据「{{ motionGridModal.shotLabel || '当前分镜' }}」文案生成一张 3×3 的9帧连续画面：格1起势承接上一镜收势，格2~8按动作节拍推进，格9收势将作为下一镜的开场。
        生成后整图会挂为本镜视频参考，武打多人也能保持前后一致。
      </p>

      <div v-if="motionGridModal.results?.length" class="result-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">AI 合成结果（{{ motionGridModal.results.length }}）</span>
        </div>
        <p class="ref-zone-hint">本次任务已生成的9帧图，点击可放大查看</p>
        <div class="scene-ref-grid compact results">
          <div
            v-for="(img, i) in motionGridModal.results"
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
          <span class="pos-label-text">9帧提示词</span>
          <el-button
            size="small"
            :loading="motionGridModal.analyzing"
            :disabled="motionGridModal.submitting"
            @click="reanalyzeMotionGridPrompt"
          >
            {{ motionGridModal.analyzing ? '分析中…' : (promptBody.trim() ? '重新分析文案' : '分析文案') }}
          </el-button>
        </div>
        <div class="pos-fixed-rule">
          统一附加（每次生成自动带上）：9格为同一镜头的连续画面，人物面容/服装/武器与场景光照全部锁定，仅允许时间推进与连续运镜；格内不出现文字与水印。有上一镜9帧图时，图1自动锚定其收势帧。
        </div>
        <el-input
          v-model="promptBody"
          type="textarea"
          :rows="motionGridModal.results?.length ? 5 : 10"
          :disabled="motionGridModal.analyzing"
          :placeholder="motionGridModal.analyzing ? '正在结合前 10 镜与上一镜收势帧分析分镜文案…' : '点击「分析文案」生成9帧连续画面提示词（起势→动作节拍→收势），或自行填写'"
        />
        <div v-if="motionGridRefs.length" class="pos-legend">
          <div class="pos-legend-title">参考图对应（可改名称）</div>
          <ul class="pos-legend-list">
            <li v-for="(ref, i) in motionGridRefs" :key="sceneReferenceKey(ref)">
              <span class="pos-legend-prefix">图{{ i + 1 }}为</span>
              <input
                class="pos-legend-input"
                type="text"
                :value="ref.label"
                :disabled="!!motionGridModal.analyzing || !!motionGridModal.submitting"
                @input="updateMotionGridRefLabel(i, ($event.target as HTMLInputElement).value)"
                @change="renameMotionGridRef(i, ($event.target as HTMLInputElement).value)"
              />
            </li>
          </ul>
        </div>
        <div v-else class="pos-legend empty">
          分析文案后会自动锚定上一镜收势帧（如有）并按角色/场景名匹配参考图；也可先手动添加
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
            :disabled="motionGridModal.submitting"
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
          <el-select v-model="imageResolution" size="small" style="width: 88px" :disabled="motionGridModal.submitting">
            <el-option
              v-for="opt in imageResolutionOptions"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label"
            />
          </el-select>
        </label>
        <span class="pos-res-hint">整图固定 16:9。建议 2K/4K：整图会切成9格使用</span>
      </div>

      <div class="ref-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">参考图（{{ motionGridRefs.length }}/{{ maxMotionGridRefs }}）</span>
          <div class="ref-zone-actions">
            <label class="ref-action-btn">
              上传
              <input type="file" accept="image/*" multiple @change="onMotionGridRefFiles" />
            </label>
            <button type="button" class="ref-action-btn" @click="openMotionGridRefPicker">资源库</button>
            <button
              v-if="motionGridRefs.length"
              type="button"
              class="ref-action-btn muted"
              @click="clearMotionGridReferences"
            >
              清空
            </button>
          </div>
        </div>
        <p class="ref-zone-hint">图1建议为上一镜收势帧（自动锚定，可替换/移除）；其余为角色、场景参考；支持上传、粘贴</p>
        <div v-if="motionGridRefs.length" class="scene-ref-grid compact">
          <div
            v-for="(ref, ri) in motionGridRefs"
            :key="sceneReferenceKey(ref)"
            class="scene-ref-card"
            :class="{ 'anchor-card': sceneReferenceKey(ref) === anchorKey }"
          >
            <div class="scene-ref-thumb">
              <img
                :src="ref.previewUrl"
                :alt="ref.label"
                class="zoomable"
                @click="openImagePreview(ref.previewUrl, ref.label)"
              />
              <span v-if="sceneReferenceKey(ref) === anchorKey" class="anchor-badge">上一镜收势帧</span>
              <button
                type="button"
                class="ref-remove-btn overlay"
                title="移除"
                @click="removeMotionGridReference(sceneReferenceKey(ref))"
              >
                ×
              </button>
            </div>
            <div class="scene-ref-preview-meta">
              <span class="ref-meta-label">图{{ ri + 1 }} · {{ ref.label }}</span>
              <button
                type="button"
                class="ref-replace-btn"
                :disabled="!!motionGridModal.analyzing || !!motionGridModal.submitting"
                @click="openMotionGridReplacePicker(ri)"
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
      <el-button :disabled="!!motionGridModal?.submitting" @click="closeMotionGridModal">取消</el-button>
      <el-button
        type="primary"
        :loading="!!motionGridModal?.submitting"
        :disabled="!!motionGridModal?.analyzing || !motionGridModal?.prompt.trim() || !motionGridRefs.length"
        @click="confirmMotionGridGenerate"
      >
        生成9帧图
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

.scene-ref-card.anchor-card {
  border-color: #c4943c;
  box-shadow: 0 0 0 1px rgba(196, 148, 60, 0.35);
}

.anchor-badge {
  position: absolute;
  left: 4px;
  top: 4px;
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(196, 148, 60, 0.88);
  color: #20180a;
  font-size: 10px;
  font-weight: 700;
  line-height: 1.3;
  pointer-events: none;
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

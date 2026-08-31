<script setup lang="ts">
import { computed } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  sceneReverseModal,
  sceneReverseRefs,
  maxSceneReverseRefs,
  sceneReverseGridChoices,
  selectSceneReverseGrid,
  splittingGridIds,
  imageModelId,
  imageModelsByProvider,
  defaultImageModelLabel,
  imageModelLabel,
  imageResolution,
  imageResolutionOptions,
  closeSceneReverseModal,
  confirmSceneReverseSkeleton,
  confirmSceneReverseGenerate,
  refillSceneReversePrompt,
  openSceneReverseRefPicker,
  openSceneReverseSkeletonPicker,
  onSceneReverseSkeletonFile,
  onSceneReverseSkeletonPaste,
  openSceneReverseReplacePicker,
  onSceneReverseRefFiles,
  onSceneReverseRefPaste,
  removeSceneReverseReference,
  clearSceneReverseReferences,
  openImagePreview,
  sceneReferenceKey,
} = useNovalyInject()

const prompt = computed({
  get: () => sceneReverseModal.value?.prompt || '',
  set: (v: string) => {
    if (sceneReverseModal.value) sceneReverseModal.value = { ...sceneReverseModal.value, prompt: v }
  },
})

const name = computed({
  get: () => sceneReverseModal.value?.name || '',
  set: (v: string) => {
    if (sceneReverseModal.value) sceneReverseModal.value = { ...sceneReverseModal.value, name: v }
  },
})

const previewRefs = computed(() => {
  const refs = sceneReverseRefs.value.map(r => ({
    key: sceneReferenceKey(r),
    previewUrl: r.previewUrl,
    label: r.label,
    locked: false,
  }))
  const sk = sceneReverseModal.value?.skeleton
  if (!sk?.url) return refs
  return [
    { key: 'reverse-skeleton', previewUrl: sk.url, label: '反打线稿', locked: true },
    ...refs.filter(r => r.previewUrl !== sk.url),
  ]
})

const refHint = computed(() => {
  if (sceneReverseModal.value?.skeleton) {
    return '发给模型：图1反打线稿，图2原镜头人物，图3俯视全景只锁平面，图4反打一侧空镜（背面）只锁对面房间。'
  }
  return '先点选一套 9 宫格。系统会自动带上「俯视全景」来画线稿；线稿核对后再自动带上对面机位空镜出成片。'
})

function replacePreviewRef(key: string) {
  const idx = sceneReverseRefs.value.findIndex(r => sceneReferenceKey(r) === key)
  if (idx >= 0) openSceneReverseReplacePicker(idx)
}

function focusSkeletonZone(e: MouseEvent) {
  ;(e.currentTarget as HTMLElement | null)?.focus()
}
</script>

<template>
  <el-dialog
    :model-value="!!sceneReverseModal"
    title="生成场景反打图"
    width="780px"
    class="modal-wide"
    align-center
    :close-on-click-modal="false"
    @close="closeSceneReverseModal"
  >
    <div v-if="sceneReverseModal" class="pos-form" @paste="onSceneReverseRefPaste">
      <p class="pos-hint">
        手动选一套 9 宫格 → 自动用俯视全景生成线稿 → 核对线稿后，再按线稿 + 人物图 + 对面机位空镜生成反打成片。
      </p>

      <div
        v-if="sceneReverseModal.skeleton"
        class="result-zone skeleton-zone"
        tabindex="0"
        @click.capture="focusSkeletonZone"
        @paste.stop="onSceneReverseSkeletonPaste"
      >
        <div class="ref-zone-head">
          <span class="ref-zone-title">反打线稿（请核对）</span>
          <div class="ref-zone-actions">
            <label class="ref-action-btn">
              上传修改后的线稿
              <input type="file" accept="image/*" @change="onSceneReverseSkeletonFile" />
            </label>
            <button type="button" class="ref-action-btn" @click="openSceneReverseSkeletonPicker">
              从资源库更换线稿
            </button>
          </div>
        </div>
        <p class="ref-zone-hint">点击此区域后可按 Ctrl+V / Command+V 粘贴图片替换。应是平视反打画面：前景过肩/背影，对面正脸；门若在原图近处，线稿里应在远处。不要俯视、不要 A/B 两个机位图标。</p>
        <div class="scene-ref-grid compact results">
          <div class="scene-ref-card result-card">
            <img
              :src="sceneReverseModal.skeleton.url"
              alt="反打线稿"
              class="zoomable"
              @click="openImagePreview(sceneReverseModal.skeleton.url, '反打线稿')"
            />
            <div class="scene-ref-preview-meta">
              <span>反打机位 · 对调前后景</span>
            </div>
          </div>
        </div>
      </div>

      <div v-if="sceneReverseModal.results?.length" class="result-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">AI 合成结果（{{ sceneReverseModal.results.length }}）</span>
        </div>
        <p class="ref-zone-hint">本次任务已生成的反打镜头，点击可放大查看</p>
        <div class="scene-ref-grid compact results">
          <div
            v-for="(img, i) in sceneReverseModal.results"
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
        <el-input v-model="name" placeholder="场景名称，如：私人会所包厢" :disabled="sceneReverseModal.submitting" />
      </section>

      <section class="pos-section">
        <div class="pos-section-head">
          <span class="pos-label-text">9宫格</span>
          <span class="pos-mini-hint">点选后自动取俯视全景；线稿通过后自动取对面机位（正面↔背面）。未切分会先切成 9 格。</span>
        </div>
        <div v-if="sceneReverseGridChoices.length" class="scene-ref-grid compact grid-pick">
          <button
            v-for="g in sceneReverseGridChoices"
            :key="g.id"
            type="button"
            class="scene-ref-card grid-pick-card"
            :class="{ on: sceneReverseModal.gridId === g.id }"
            :disabled="!!sceneReverseModal.submitting || splittingGridIds.has(g.id)"
            @click="selectSceneReverseGrid(g)"
          >
            <div class="scene-ref-thumb">
              <img :src="g.imageUrl || g.stylizedImageUrl" :alt="g.name" />
            </div>
            <div class="scene-ref-preview-meta">
              <span>{{ g.name.replace(/\s*·\s*9宫格.*$/, '') || g.name }}</span>
            </div>
          </button>
        </div>
        <div v-else class="ref-zone-empty">资源库还没有场景 9 宫格。请先给该场景生成 9 宫格。</div>
      </section>

      <section class="pos-section">
        <div class="pos-section-head">
          <span class="pos-label-text">{{ sceneReverseModal.skeleton ? '反打提示词' : '反打线稿提示词' }}</span>
          <span class="pos-mini-hint">{{ sceneReverseModal.skeleton ? '图1线稿定构图，图2人物定妆，俯视只锁平面，对面空镜只锁房间。' : '图1原镜头对调前后景；俯视格只看平面布局，线稿必须平视。' }}</span>
          <button
            type="button"
            class="ref-action-btn"
            :disabled="!!sceneReverseModal.submitting"
            @click="refillSceneReversePrompt"
          >
            填入模板
          </button>
        </div>
        <el-input
          v-model="prompt"
          type="textarea"
          :rows="(sceneReverseModal.results?.length || sceneReverseModal.skeleton) ? 8 : 12"
          :disabled="sceneReverseModal.submitting"
          placeholder="提示词模板已自动填充，可按房间结构编辑"
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
            :disabled="sceneReverseModal.submitting"
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
          <el-select v-model="imageResolution" size="small" style="width: 88px" :disabled="sceneReverseModal.submitting">
            <el-option
              v-for="opt in imageResolutionOptions"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label"
            />
          </el-select>
        </label>
        <span class="pos-res-hint">线稿固定 16:9 1K；正式反打用这里的分辨率，画幅跟项目走</span>
      </div>

      <div class="ref-zone">
        <div class="ref-zone-head">
          <span class="ref-zone-title">参考图（{{ previewRefs.length }}/{{ maxSceneReverseRefs }}）</span>
          <div class="ref-zone-actions">
            <label class="ref-action-btn">
              上传
              <input type="file" accept="image/*" multiple @change="onSceneReverseRefFiles" />
            </label>
            <button type="button" class="ref-action-btn" @click="openSceneReverseRefPicker">资源库</button>
            <button
              v-if="sceneReverseRefs.length"
              type="button"
              class="ref-action-btn muted"
              @click="clearSceneReverseReferences"
            >
              清空
            </button>
          </div>
        </div>
        <p class="ref-zone-hint">{{ refHint }}</p>
        <div v-if="previewRefs.length" class="scene-ref-grid compact">
          <div v-for="(ref, ri) in previewRefs" :key="ref.key" class="scene-ref-card">
            <div class="scene-ref-thumb">
              <img
                :src="ref.previewUrl"
                :alt="ref.label"
                class="zoomable"
                @click="openImagePreview(ref.previewUrl, ref.label)"
              />
              <button
                v-if="!ref.locked"
                type="button"
                class="ref-remove-btn overlay"
                title="移除"
                @click="removeSceneReverseReference(ref.key)"
              >
                ×
              </button>
            </div>
            <div class="scene-ref-preview-meta">
              <span class="ref-meta-label">图{{ ri + 1 }} · {{ ref.label }}</span>
              <button
                v-if="!ref.locked"
                type="button"
                class="ref-replace-btn"
                :disabled="!!sceneReverseModal.submitting"
                @click="replacePreviewRef(ref.key)"
              >
                替换
              </button>
            </div>
          </div>
        </div>
        <div v-else class="ref-zone-empty">暂无参考图：请至少保留场景原图，用来画反打线稿</div>
      </div>
    </div>
    <template #footer>
      <el-button @click="closeSceneReverseModal">关闭</el-button>
      <el-button
        :loading="sceneReverseModal?.submittingStep === 'skeleton'"
        :disabled="!!sceneReverseModal?.submitting || !sceneReverseModal?.prompt.trim() || !sceneReverseModal?.name.trim()"
        @click="confirmSceneReverseSkeleton"
      >
        {{ sceneReverseModal?.skeleton ? '后台重新生成线稿' : '后台生成火柴人图' }}
      </el-button>
      <el-button
        type="primary"
        :loading="sceneReverseModal?.submittingStep === 'final'"
        :disabled="!!sceneReverseModal?.submitting || !sceneReverseModal?.skeleton?.url"
        @click="confirmSceneReverseGenerate"
      >
        线稿符合，生成反打图
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
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 10px;
}

.scene-ref-card {
  display: grid;
  gap: 6px;
}

.scene-ref-thumb {
  position: relative;
}

.scene-ref-card img {
  width: 100%;
  aspect-ratio: 16 / 9;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid #3a352f;
  background: #121110;
}

.scene-ref-card img.zoomable {
  cursor: zoom-in;
}

.grid-pick-card {
  padding: 0;
  border: 1px solid #3a352f;
  border-radius: 8px;
  background: #141210;
  color: inherit;
  cursor: pointer;
  text-align: left;
}

.grid-pick-card.on {
  border-color: #c4a574;
  box-shadow: 0 0 0 1px #c4a574;
}

.grid-pick-card:disabled {
  opacity: 0.55;
  cursor: wait;
}

.ref-remove-btn.overlay {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 22px;
  height: 22px;
  border: 0;
  border-radius: 50%;
  background: rgba(20, 18, 16, 0.78);
  color: #ebe4db;
  cursor: pointer;
}

.scene-ref-preview-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  font-size: 11px;
  color: #9a9288;
}

.ref-meta-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ref-replace-btn {
  flex-shrink: 0;
  border: 1px solid #3a352f;
  border-radius: 4px;
  background: #221f1c;
  color: #d3ccc2;
  font-size: 11px;
  padding: 2px 6px;
  cursor: pointer;
}
</style>

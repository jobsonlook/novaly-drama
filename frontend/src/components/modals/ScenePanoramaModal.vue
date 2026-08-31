<script setup lang="ts">
import { computed } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  scenePanoramaModal,
  scenePanoramaRefs,
  maxScenePanoramaRefs,
  scenePanoramaGridChoices,
  selectScenePanoramaGrid,
  splittingGridIds,
  imageModelId,
  imageModelsByProvider,
  defaultImageModelLabel,
  imageModelLabel,
  imageResolution,
  imageResolutionOptions,
  closeScenePanoramaModal,
  confirmScenePanoramaGenerate,
  refillScenePanoramaPrompt,
  removeScenePanoramaReference,
  clearScenePanoramaReferences,
  onScenePanoramaRefFiles,
  openImagePreview,
  openPanoramaViewer,
  sceneReferenceKey,
} = useNovalyInject()

const prompt = computed({
  get: () => scenePanoramaModal.value?.prompt || '',
  set: (v: string) => {
    if (scenePanoramaModal.value) scenePanoramaModal.value = { ...scenePanoramaModal.value, prompt: v }
  },
})

const name = computed({
  get: () => scenePanoramaModal.value?.name || '',
  set: (v: string) => {
    if (scenePanoramaModal.value) scenePanoramaModal.value = { ...scenePanoramaModal.value, name: v }
  },
})
</script>

<template>
  <el-dialog
    v-if="scenePanoramaModal"
    :model-value="true"
    title="生成场景全景图（参照九宫格展开）"
    width="780px"
    class="scene-panorama-dialog"
    align-center
    destroy-on-close
    :close-on-click-modal="!scenePanoramaModal.submitting"
    @close="closeScenePanoramaModal"
  >
    <p class="pano-lead">
      先选一套已切分的场景 9 宫格，系统自动带上正面/背面/俯视（+侧面）机位格，再展开成 2:1 等距柱状 360°。质量取决于九宫格是否一致。
    </p>

    <section class="pos-section">
      <div class="pos-section-head">
        <span class="pos-label-text">9宫格</span>
        <span class="pos-mini-hint">点选后自动取格1正面、格5背面、格7俯视；未切分会先切成 9 格。</span>
      </div>
      <div v-if="scenePanoramaGridChoices.length" class="scene-ref-grid compact grid-pick">
        <button
          v-for="g in scenePanoramaGridChoices"
          :key="g.id"
          type="button"
          class="scene-ref-card grid-pick-card"
          :class="{ on: scenePanoramaModal.gridId === g.id }"
          :disabled="!!scenePanoramaModal.submitting || splittingGridIds.has(g.id)"
          @click="selectScenePanoramaGrid(g)"
        >
          <div class="scene-ref-thumb">
            <img :src="g.imageUrl || g.stylizedImageUrl" :alt="g.name" />
          </div>
          <div class="scene-ref-preview-meta">
            <span>{{ g.name.replace(/\s*·\s*9宫格.*$/, '') || g.name }}</span>
          </div>
        </button>
      </div>
      <div v-else class="ref-zone-empty">资源库还没有场景 9 宫格。请先给该场景生成并切分 9 宫格，再来展开全景。</div>
    </section>

    <div class="ref-zone">
      <div class="ref-zone-head">
        <span class="ref-zone-title">参考图（{{ scenePanoramaRefs.length }}/{{ maxScenePanoramaRefs }}）</span>
        <div class="ref-zone-actions">
          <label class="ref-action-btn">
            上传
            <input type="file" accept="image/*" multiple :disabled="!!scenePanoramaModal.submitting" @change="onScenePanoramaRefFiles" />
          </label>
          <button
            v-if="scenePanoramaRefs.length"
            type="button"
            class="ref-action-btn muted"
            :disabled="!!scenePanoramaModal.submitting"
            @click="clearScenePanoramaReferences"
          >
            清空
          </button>
        </div>
      </div>
      <p class="ref-zone-hint">建议顺序：正面底板 → 正面全景格 → 背面全景格 → 俯视全景格。选中九宫格后会自动填充。</p>
      <div v-if="scenePanoramaRefs.length" class="scene-ref-grid compact">
        <div v-for="(ref, ri) in scenePanoramaRefs" :key="sceneReferenceKey(ref)" class="scene-ref-card">
          <img :src="ref.previewUrl" :alt="ref.label" class="zoomable" @click="openImagePreview(ref.previewUrl, ref.label)" />
          <div class="scene-ref-preview-meta">
            <span>图{{ ri + 1 }} · {{ ref.label }}</span>
            <button type="button" class="ref-remove-btn" :disabled="!!scenePanoramaModal.submitting" @click="removeScenePanoramaReference(sceneReferenceKey(ref))">×</button>
          </div>
        </div>
      </div>
      <div v-else class="ref-zone-empty">请先选择上方 9 宫格，或手动上传正面底板</div>
    </div>

    <section class="pos-section">
      <div class="pos-section-head">
        <span class="pos-label-text">场景名称</span>
      </div>
      <el-input v-model="name" placeholder="场景名称" :disabled="!!scenePanoramaModal.submitting" />
    </section>

    <section class="pos-section">
      <div class="pos-section-head">
        <span class="pos-label-text">全景提示词</span>
        <button type="button" class="ref-action-btn muted" :disabled="!!scenePanoramaModal.submitting" @click="refillScenePanoramaPrompt">恢复默认</button>
      </div>
      <el-input v-model="prompt" type="textarea" :rows="9" :disabled="!!scenePanoramaModal.submitting" />
    </section>

    <section class="pos-section generate-row">
      <label class="candidate-count-select">
        模型
        <el-select v-model="imageModelId" size="small" clearable filterable placeholder="默认" style="width: 240px" :disabled="!!scenePanoramaModal.submitting">
          <el-option :value="null" :label="defaultImageModelLabel" />
          <el-option-group v-for="group in imageModelsByProvider" :key="group.providerId" :label="group.providerName">
            <el-option v-for="m in group.models" :key="m.id" :value="m.id" :label="imageModelLabel(m)" />
          </el-option-group>
        </el-select>
      </label>
      <label class="candidate-count-select">
        分辨率
        <el-select v-model="imageResolution" size="small" style="width: 88px" :disabled="!!scenePanoramaModal.submitting">
          <el-option v-for="opt in imageResolutionOptions" :key="opt.value" :value="opt.value" :label="opt.label" />
        </el-select>
      </label>
    </section>

    <div v-if="scenePanoramaModal.results?.length" class="result-zone">
      <div class="ref-zone-head">
        <span class="ref-zone-title">生成结果（{{ scenePanoramaModal.results.length }}）</span>
      </div>
      <div class="scene-ref-grid compact results">
        <div
          v-for="(img, i) in scenePanoramaModal.results"
          :key="img.resourceId || img.url || i"
          class="scene-ref-card result-card"
        >
          <img
            :src="img.url"
            :alt="img.label || `全景 ${i + 1}`"
            class="zoomable"
            @click="openPanoramaViewer(img.url, img.label || `全景 ${i + 1}`)"
          />
          <div class="scene-ref-preview-meta">
            <span>{{ img.label || `全景 ${i + 1}` }}</span>
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <el-button :disabled="!!scenePanoramaModal.submitting" @click="closeScenePanoramaModal">关闭</el-button>
      <el-button type="primary" :loading="!!scenePanoramaModal.submitting" @click="confirmScenePanoramaGenerate">
        {{ scenePanoramaModal.submitting ? '展开中…' : '按九宫格生成全景' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.pano-lead {
  margin: 0 0 14px;
  font-size: 13px;
  line-height: 1.55;
  color: #b9afa5;
}
.pos-section {
  margin-top: 14px;
}
.pos-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
  flex-wrap: wrap;
}
.pos-mini-hint {
  font-size: 12px;
  color: #9a9086;
  flex: 1;
  min-width: 180px;
}
.grid-pick-card.on {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 1px;
}
</style>

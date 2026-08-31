<script setup lang="ts">
import { useNovalyInject } from '@/composables/useNovalyInject'

const { promptPreview, closePromptPreview, refKindLabel } = useNovalyInject()
</script>

<template>
  <el-dialog
    :model-value="!!promptPreview"
    title="最终提示词预览"
    width="720px"
    class="modal-wide"
    align-center
    @close="closePromptPreview"
  >
    <p class="modal-hint">
      以下为发送给视频模型的完整文本提示词；参考图按顺序单独附加（共 {{ promptPreview?.refImages.length ?? 0 }} 张）
    </p>
    <div v-if="promptPreview" class="prompt-preview-body">
      <div class="prompt-meta">
        <span>模型：{{ promptPreview.modelName }} <small>({{ promptPreview.modelId }})</small></span>
        <span>{{ promptPreview.ratio }} · {{ promptPreview.duration }}s · {{ promptPreview.resolution }}</span>
      </div>
      <el-input type="textarea" class="prompt-preview-text" readonly :rows="12" :model-value="promptPreview.prompt" />
      <div v-if="promptPreview.refImages.length" class="prompt-ref-list">
        <h4>参考图顺序</h4>
        <div v-for="ref in promptPreview.refImages" :key="ref.index" class="prompt-ref-item">
          <b>图{{ ref.index }}</b>
          <span class="variant-badge">{{ refKindLabel(ref.kind, ref.variant) }}</span>
          <span>{{ ref.label || ref.name }}</span>
        </div>
      </div>
    </div>
    <template #footer>
      <el-button type="primary" @click="closePromptPreview">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { useNovalyInject } from '@/composables/useNovalyInject'

const { imagePreview, closeImagePreview, previewSelectCandidate } = useNovalyInject()
</script>

<template>
  <el-dialog
    :model-value="!!imagePreview"
    :title="imagePreview?.label"
    width="90%"
    class="image-preview-dialog"
    align-center
    @close="closeImagePreview"
  >
    <div class="image-preview-body">
      <img v-if="imagePreview" :src="imagePreview.url" :alt="imagePreview.label" />
    </div>
    <template #footer>
      <el-button v-if="imagePreview?.selectUrl" type="primary" @click="previewSelectCandidate">选用此图</el-button>
      <el-button @click="closeImagePreview">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.image-preview-body {
  display: flex;
  justify-content: center;
  max-height: 70vh;
  overflow: auto;
}
.image-preview-body img {
  max-width: 100%;
  max-height: 70vh;
  object-fit: contain;
}
</style>

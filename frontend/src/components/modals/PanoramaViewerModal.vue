<script setup lang="ts">
import { useNovalyInject } from '@/composables/useNovalyInject'
import PanoramaViewer from './PanoramaViewer.vue'

const { panoramaViewer, closePanoramaViewer } = useNovalyInject()
</script>

<template>
  <el-dialog
    :model-value="!!panoramaViewer"
    :title="panoramaViewer?.label || '环视全景'"
    width="92%"
    class="panorama-viewer-dialog"
    align-center
    destroy-on-close
    @close="closePanoramaViewer"
  >
    <PanoramaViewer
      v-if="panoramaViewer"
      :src="panoramaViewer.url"
      :initial-yaw-deg="panoramaViewer.initialYawDeg ?? -90"
    />
    <template #footer>
      <el-button @click="closePanoramaViewer">关闭</el-button>
    </template>
  </el-dialog>
</template>

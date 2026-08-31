<script setup lang="ts">
import ConfirmModal from './ConfirmModal.vue'
import ImagePreviewModal from './ImagePreviewModal.vue'
import PromptPreviewModal from './PromptPreviewModal.vue'
import ShotRefPickerModal from './ShotRefPickerModal.vue'
import SceneRefPickerModal from './SceneRefPickerModal.vue'
import PositioningModal from './PositioningModal.vue'
import MotionGridModal from './MotionGridModal.vue'
import SceneGridModal from './SceneGridModal.vue'
import SceneReverseModal from './SceneReverseModal.vue'
import ScenePanoramaModal from './ScenePanoramaModal.vue'
import PanoramaViewerModal from './PanoramaViewerModal.vue'
import DirectorDeskModal from './DirectorDeskModal.vue'
import ModelFormModal from './ModelFormModal.vue'
import StylizePromptModal from './StylizePromptModal.vue'
import ResourceEditModal from './ResourceEditModal.vue'
import CrewPipelineModal from './CrewPipelineModal.vue'
</script>

<template>
  <ConfirmModal />
  <ModelFormModal />
  <StylizePromptModal />
  <PositioningModal />
  <MotionGridModal />
  <SceneGridModal />
  <SceneReverseModal />
  <ScenePanoramaModal />
  <PanoramaViewerModal />
  <DirectorDeskModal />
  <ResourceEditModal />
  <CrewPipelineModal />
  <ImagePreviewModal />
  <PromptPreviewModal />
  <ShotRefPickerModal />
  <SceneRefPickerModal />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  modelForm,
  modelFormSaving,
  DOUBAO_WEB_VIDEO_PRESETS,
  closeModelForm,
  onModelPresetChange,
  saveModelForm,
} = useNovalyInject()

const title = computed(() => (modelForm.value?.editingId ? '编辑模型' : '添加模型'))
const showVideoPresets = computed(
  () => modelForm.value?.providerSlug === 'doubao-web-api' && modelForm.value?.capability === 'video',
)
</script>

<template>
  <el-dialog
    :model-value="!!modelForm"
    :title="title"
    width="480px"
    align-center
    @close="closeModelForm"
  >
    <div v-if="modelForm" class="model-form">
      <label>
        显示名称
        <el-input v-model="modelForm.name" placeholder="例如 Seedance 2.0 Fast" />
      </label>
      <label>
        模型 ID
        <el-select
          v-if="showVideoPresets"
          v-model="modelForm.modelId"
          filterable
          allow-create
          default-first-option
          placeholder="选择或输入模型 ID"
          @change="onModelPresetChange"
        >
          <el-option
            v-for="preset in DOUBAO_WEB_VIDEO_PRESETS"
            :key="preset.modelId"
            :label="preset.modelId"
            :value="preset.modelId"
          >
            <span>{{ preset.name }}</span>
            <small>{{ preset.modelId }}</small>
          </el-option>
        </el-select>
        <el-input v-else v-model="modelForm.modelId" placeholder="模型 ID / 接入点 ID" />
      </label>
    </div>
    <template #footer>
      <el-button :disabled="modelFormSaving" @click="closeModelForm">取消</el-button>
      <el-button type="primary" :loading="modelFormSaving" @click="saveModelForm">
        {{ modelFormSaving ? '保存中…' : '保存' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.model-form {
  display: grid;
  gap: 16px;
}

.model-form label {
  display: grid;
  gap: 8px;
  font-size: 13px;
  font-weight: 700;
}

.model-form :deep(.el-select) {
  width: 100%;
}

.model-form :deep(.el-select-dropdown__item) {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  line-height: 1.4;
  height: auto;
  padding-top: 8px;
  padding-bottom: 8px;
}

.model-form :deep(.el-select-dropdown__item small) {
  font: 11px 'DM Mono', monospace;
  color: #938a80;
}
</style>

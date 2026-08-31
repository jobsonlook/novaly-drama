<script setup lang="ts">
import { computed } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  stylizeModal,
  isStylizingResource,
  imageModelId,
  imageModels,
  imageModelsByProvider,
  defaultImageModelLabel,
  imageModelLabel,
  defaultImageModel,
  effectiveImageModelId,
  imageResolution,
  imageResolutionOptions,
  closeStylizeModal,
  confirmStylize,
} = useNovalyInject()

const stylizeUsesNanoBanana = computed(() => {
  const id = effectiveImageModelId.value
  const m = (id && imageModels.value.find(x => x.id === id)) || defaultImageModel.value
  if (!m) return false
  return /nano[\s_-]*banana/i.test(`${m.name} ${m.modelId}`)
})
</script>

<template>
  <el-dialog
    :model-value="!!stylizeModal"
    title="生成非真人图"
    width="560px"
    align-center
    :close-on-click-modal="true"
    @close="closeStylizeModal"
  >
    <div v-if="stylizeModal" class="stylize-form">
      <p class="stylize-hint">
        为「{{ stylizeModal.resourceName }}」生成非真人图。可编辑下方提示词，留空则使用默认模板。
        开始后弹窗会立即关闭，可继续操作其他内容；进度在右下角「生成任务」面板显示。
      </p>
      <el-alert
        v-if="stylizeUsesNanoBanana"
        type="warning"
        :closable="false"
        show-icon
        title="当前是 Nano Banana"
        description="用它做非真人图容易跑偏，建议改选 Seedream。点「开始生成」还会再确认一次。"
      />
      <div class="stylize-gen-opts">
        <label>
          模型
          <el-select v-model="imageModelId" size="small" clearable filterable placeholder="默认" style="width: 100%">
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
        <label>
          分辨率
          <el-select v-model="imageResolution" size="small" style="width: 100%">
            <el-option
              v-for="opt in imageResolutionOptions"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label"
            />
          </el-select>
        </label>
      </div>
      <label>
        提示词
        <el-input
          v-model="stylizeModal.prompt"
          type="textarea"
          :rows="6"
          placeholder="输入非真人图生成提示词…"
        />
      </label>
    </div>
    <template #footer>
      <el-button @click="closeStylizeModal">取消</el-button>
      <el-button
        type="primary"
        :disabled="!!stylizeModal && isStylizingResource(stylizeModal.resourceId)"
        @click="confirmStylize"
      >
        开始生成
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.stylize-form {
  display: grid;
  gap: 14px;
}

.stylize-hint {
  margin: 0;
  font-size: 13px;
  color: #9a9288;
  line-height: 1.6;
}

.stylize-gen-opts {
  display: grid;
  grid-template-columns: 1fr 100px;
  gap: 10px;
}

.stylize-form label {
  display: grid;
  gap: 8px;
  font-size: 13px;
  font-weight: 700;
  color: #d3ccc2;
}
</style>

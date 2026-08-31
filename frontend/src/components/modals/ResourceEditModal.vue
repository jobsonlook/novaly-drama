<script setup lang="ts">
import { computed } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  editResourceModal,
  updatingResource,
  closeEditResourceModal,
  confirmEditResource,
} = useNovalyInject()

const loading = () => updatingResource.value === editResourceModal.value?.resourceId
const isVideo = computed(() => editResourceModal.value?.type === 'video')
const isCharacter = computed(() => editResourceModal.value?.type === 'character')
</script>

<template>
  <el-dialog
    :model-value="!!editResourceModal"
    title="编辑资源"
    width="640px"
    align-center
    @close="closeEditResourceModal"
  >
    <div v-if="editResourceModal" class="resource-edit-form">
      <label>
        名称
        <el-input v-model="editResourceModal.name" placeholder="资源名称" />
      </label>
      <label v-if="isVideo">
        备注（仅本视频）
        <el-input
          v-model="editResourceModal.remark"
          type="textarea"
          :rows="2"
          placeholder="例如：狼女对峙；导出文件名为 分镜xx+备注"
        />
      </label>
      <label>
        描述
        <el-input
          v-model="editResourceModal.description"
          type="textarea"
          :rows="isVideo ? 4 : 3"
          :placeholder="isVideo ? '分镜原文 / 提示词摘要…' : '剧情身份、关系、关键外观线索…'"
        />
      </label>
      <label v-if="isCharacter">
        音色提示词
        <el-input
          v-model="editResourceModal.voicePrompt"
          type="textarea"
          :rows="3"
          placeholder="各分镜视频共用同一句，例如：30岁左右男性，声线低沉沙哑带磁性，不是少年音"
        />
      </label>
      <label v-if="!isVideo">
        绘图提示词
        <el-input
          v-model="editResourceModal.genPrompt"
          type="textarea"
          :rows="7"
          placeholder="用于生图的外形圣经：画风、五官、发型、服装、构图约束…"
        />
      </label>
      <p v-if="isCharacter" class="edit-hint">
        豆包网页生成视频时会带上【声音要求】。同一角色请保持这句不变，跨分镜、跨账号才能锁住声线。
      </p>
      <p v-if="!isVideo" class="edit-hint">
        描述是剧情信息；绘图提示词才会拿去出图。可在卡片上点「批量生成提示词」自动写，或在这里改。
      </p>
    </div>
    <template #footer>
      <el-button :disabled="loading()" @click="closeEditResourceModal">取消</el-button>
      <el-button type="primary" :loading="loading()" @click="confirmEditResource">
        {{ loading() ? '保存中…' : '保存' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.resource-edit-form {
  display: grid;
  gap: 14px;
}

.resource-edit-form label {
  display: grid;
  gap: 8px;
  font-size: 13px;
  font-weight: 700;
  color: #d3ccc2;
}

.edit-hint {
  margin: 0;
  font-size: 12px;
  color: #9a9288;
  line-height: 1.5;
}
</style>

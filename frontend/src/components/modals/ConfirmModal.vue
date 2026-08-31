<script setup lang="ts">
import { useNovalyInject } from '@/composables/useNovalyInject'

const { confirmModal, confirmLoading, closeConfirm, runConfirm } = useNovalyInject()
</script>

<template>
  <el-dialog
    :model-value="!!confirmModal"
    :title="confirmModal?.title"
    width="480px"
    align-center
    @close="closeConfirm"
  >
    <p class="confirm-message">{{ confirmModal?.message }}</p>
    <template #footer>
      <el-button :disabled="confirmLoading" @click="closeConfirm">取消</el-button>
      <el-button
        :type="confirmModal?.danger ? 'danger' : 'primary'"
        :loading="confirmLoading"
        @click="runConfirm"
      >
        {{ confirmLoading ? '处理中…' : confirmModal?.confirmText }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { useNovalyInject } from '@/composables/useNovalyInject'

defineProps<{ embedded?: boolean }>()

const { trashProjects, restoreProject, purgeProject } = useNovalyInject()
</script>

<template>
  <section :class="embedded ? 'trash-embedded' : 'page-workspace'">
    <header v-if="!embedded" class="page-header">
      <el-breadcrumb separator="/">
        <el-breadcrumb-item>回收站</el-breadcrumb-item>
      </el-breadcrumb>
      <h1>已删除的项目</h1>
      <p class="page-desc">项目数据会保留，可恢复或永久删除。</p>
    </header>

    <div v-else class="trash-embedded-head">
      <h2>已删除的项目</h2>
      <p class="page-desc">项目数据会保留，可恢复或永久删除。</p>
    </div>

    <el-empty v-if="!trashProjects.length" description="回收站是空的">
      <template #image><span class="empty-icon">🗑</span></template>
    </el-empty>

    <div v-else class="trash-list">
      <el-card v-for="p in trashProjects" :key="p.id" class="trash-card" shadow="never">
        <div class="trash-card-inner">
          <div>
            <b class="trash-title">{{ p.title }}</b>
            <small class="trash-meta">{{ p.shotCount }} 分镜</small>
            <p v-if="p.deletedAt" class="trash-time">删除于 {{ new Date(p.deletedAt).toLocaleString() }}</p>
          </div>
          <div class="trash-actions">
            <el-button @click="restoreProject(p)">恢复</el-button>
            <el-button type="danger" plain @click="purgeProject(p)">永久删除</el-button>
          </div>
        </div>
      </el-card>
    </div>
  </section>
</template>

<style scoped>
.trash-embedded {
  padding: 0;
}

.trash-embedded-head {
  margin-bottom: 20px;
}

.trash-embedded-head h2 {
  margin: 0 0 8px;
  font-size: 18px;
  font-weight: 700;
  color: #eee9e1;
}

.page-workspace {
  max-width: 800px;
  padding: 32px 40px 48px;
}

.page-header {
  margin-bottom: 28px;
}

.page-header h1 {
  margin: 12px 0 8px;
  font-size: 24px;
  font-weight: 700;
  color: #eee9e1;
}

.page-desc {
  margin: 0;
  font-size: 13px;
  color: #9a9288;
}

.page-error {
  margin-bottom: 20px;
}

.empty-icon {
  font-size: 48px;
}

.trash-list {
  display: grid;
  gap: 12px;
}

.trash-card {
  border-radius: 12px;
  border: 1px solid #2e2a26;
  background: rgba(255, 255, 255, 0.02);
}

.trash-card-inner {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.trash-title {
  display: block;
  font-size: 15px;
  color: #eee9e1;
}

.trash-meta {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: #8f8880;
}

.trash-time {
  margin: 8px 0 0;
  font-size: 11px;
  color: #7a736a;
}

.trash-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
</style>

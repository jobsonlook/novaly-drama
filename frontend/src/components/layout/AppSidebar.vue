<script setup lang="ts">
import { Plus, Headset } from '@element-plus/icons-vue'
import { useRoute } from 'vue-router'
import { useNovalyInject } from '@/composables/useNovalyInject'

const route = useRoute()

const {
  logoDark,
  projects,
  active,
  view,
  openProject,
  goHome,
  openTts,
} = useNovalyInject()
</script>

<template>
  <div class="app-sidebar">
    <div class="sidebar-brand">
      <img :src="logoDark" alt="Novaly" class="brand-logo" />
    </div>

    <el-button type="primary" size="large" round class="sidebar-new" :icon="Plus" @click="goHome">
      我的项目
    </el-button>

    <el-button
      size="default"
      class="sidebar-tts"
      :class="{ active: view === 'tts' || route.name === 'tts' }"
      :icon="Headset"
      @click="openTts"
    >
      分镜配音
    </el-button>
    <div class="sidebar-section">
      <span class="sidebar-label">我的项目</span>
      <el-scrollbar class="project-scroll">
        <div
          v-for="p in projects"
          :key="p.id"
          class="project-card"
          :class="{ active: active?.id === p.id && view === 'studio' }"
          @click="openProject(p.id)"
        >
          <span class="project-title">{{ p.title }}</span>
          <span class="project-meta">{{ p.shotCount }} 分镜</span>
        </div>
        <el-empty v-if="!projects.length" description="暂无项目" :image-size="48" />
      </el-scrollbar>
    </div>
  </div>
</template>

<style scoped>
.app-sidebar {
  display: flex;
  flex-direction: column;
  height: 100vh;
  padding: 20px 14px;
  background: linear-gradient(180deg, #1a1816 0%, #141210 100%);
}

.sidebar-brand {
  display: flex;
  justify-content: center;
  padding: 4px 8px 20px;
}

.brand-logo {
  max-width: 148px;
  height: auto;
  object-fit: contain;
}

.sidebar-new {
  width: 100%;
  margin-bottom: 12px;
  font-weight: 700;
  letter-spacing: 0.02em;
}

.sidebar-tts {
  width: 100%;
  margin-bottom: 20px;
  border: 1px solid #3c3731;
  background: #211e1b;
  color: #d3ccc2;
}

.sidebar-tts:hover,
.sidebar-tts.active {
  border-color: #ff785a;
  color: #ff9d85;
  background: rgba(255, 120, 90, 0.12);
}

.sidebar-section {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding-bottom: 8px;
}

.sidebar-label {
  display: block;
  padding: 0 8px 10px;
  font-family: 'DM Mono', monospace;
  font-size: 10px;
  letter-spacing: 1.5px;
  color: #8f8880;
  text-transform: uppercase;
}

.project-scroll {
  flex: 1;
}

.project-card {
  padding: 10px 12px;
  margin-bottom: 4px;
  border-radius: 10px;
  cursor: pointer;
  border: 1px solid transparent;
  transition: background 0.15s, border-color 0.15s;
}

.project-card:hover {
  background: rgba(255, 255, 255, 0.04);
}

.project-card.active {
  background: rgba(255, 120, 90, 0.12);
  border-color: rgba(255, 120, 90, 0.35);
}

.project-title {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: #eee9e1;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.project-meta {
  display: block;
  margin-top: 3px;
  font-size: 11px;
  color: #8f8880;
}
</style>

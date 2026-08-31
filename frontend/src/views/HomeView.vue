<script setup lang="ts">
import { computed, ref } from 'vue'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { useNovalyInject } from '@/composables/useNovalyInject'
import type { ProjectSummary } from '@/types'
import { visualManualName } from '@/data/projectManuals'
import ProjectMetaFields from '@/components/studio/ProjectMetaFields.vue'

const { form, error, create, projects, load, openProject, loading, projectLoading } = useNovalyInject()

const query = ref('')
const refreshing = ref(false)
const createOpen = ref(false)
const creating = ref(false)
const openingId = ref<number | null>(null)

const pageBusy = computed(() => loading.value || refreshing.value)

const filteredProjects = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return projects.value
  return projects.value.filter(p => {
    const hay = `${p.title} ${p.genre || ''} ${p.synopsis || ''} ${p.style || ''}`.toLowerCase()
    return hay.includes(q)
  })
})

async function refreshProjects() {
  refreshing.value = true
  try {
    await load()
  } finally {
    refreshing.value = false
  }
}

async function onSearch() {
  await refreshProjects()
}

async function onCreate() {
  creating.value = true
  try {
    await create()
    createOpen.value = false
  } finally {
    creating.value = false
  }
}

async function onOpenProject(id: number) {
  if (projectLoading.value) return
  openingId.value = id
  try {
    await openProject(id)
  } finally {
    openingId.value = null
  }
}

function formatUpdatedAt(p: ProjectSummary) {
  const raw = p.updatedAt || p.createdAt
  if (!raw) return ''
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  return d.toLocaleString('zh-CN', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function stylePreview(style: string) {
  const s = (style || '').trim().replace(/\s+/g, ' ')
  if (!s) return '暂无画面质感描述'
  return s.length > 110 ? `${s.slice(0, 110)}…` : s
}
</script>

<template>
  <section
    class="home-page"
    v-loading="pageBusy"
    element-loading-text="加载项目列表…"
    element-loading-background="rgba(17, 16, 15, 0.55)"
  >
    <div class="home-inner">
      <header class="home-header">
        <div>
          <h1>我的项目</h1>
          <p>管理您的 AI 漫剧协作项目</p>
        </div>
        <div class="home-search-row">
          <el-input
            v-model="query"
            clearable
            placeholder="搜索项目名称或描述…"
            class="home-search"
            :prefix-icon="Search"
            @keyup.enter="onSearch"
          />
          <el-button type="primary" @click="onSearch">搜索</el-button>
          <el-button :icon="Refresh" :loading="refreshing" circle @click="refreshProjects" title="刷新" />
        </div>
      </header>

      <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="home-error" />

      <div class="project-grid">
        <button type="button" class="project-card create-card" @click="createOpen = true">
          <span class="create-plus"><el-icon :size="40"><Plus /></el-icon></span>
          <strong>新建项目</strong>
        </button>

        <button
          v-for="p in filteredProjects"
          :key="p.id"
          type="button"
          class="project-card"
          :class="{ opening: openingId === p.id }"
          :disabled="projectLoading"
          @click="onOpenProject(p.id)"
        >
          <div v-if="openingId === p.id" class="project-card-opening">打开中…</div>
          <div class="project-card-top">
            <h2>{{ p.title || '未命名项目' }}</h2>
            <p>{{ p.genre || visualManualName(p.visualManual) || stylePreview(p.style) }}</p>
          </div>
          <div class="project-card-foot">
            <div class="project-card-meta">
              <span class="meta-chip">{{ p.shotCount }} 分镜</span>
              <span class="meta-chip">{{ p.videoRatio || '16:9' }}</span>
            </div>
            <time v-if="formatUpdatedAt(p)">{{ formatUpdatedAt(p) }}</time>
          </div>
        </button>
      </div>

      <el-empty
        v-if="!filteredProjects.length && query.trim()"
        description="没有匹配的项目"
        :image-size="72"
      />
    </div>

    <el-dialog
      v-model="createOpen"
      title="新建项目"
      width="980px"
      class="create-dialog"
      align-center
      destroy-on-close
    >
      <el-form label-position="top" @submit.prevent="onCreate">
        <ProjectMetaFields v-model="form" />
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="onCreate">创建并打开</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<style scoped>
.home-page {
  min-height: calc(100vh - 64px);
  padding: 36px 24px 64px;
}

.home-inner {
  max-width: 1200px;
  margin: 0 auto;
}

.home-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  flex-wrap: wrap;
  margin-bottom: 28px;
}

.home-header h1 {
  margin: 0 0 8px;
  font-size: 34px;
  font-weight: 750;
  letter-spacing: -0.03em;
  color: #f2ebe3;
}

.home-header p {
  margin: 0;
  font-size: 14px;
  color: #8f857c;
}

.home-search-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.home-search {
  width: min(340px, 68vw);
}

.home-error {
  margin-bottom: 18px;
}

.project-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
  gap: 18px;
}

.project-card {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  text-align: left;
  min-height: 210px;
  padding: 22px;
  border-radius: 18px;
  border: 1px solid #2e2a26;
  background: #1a1816;
  box-shadow: 0 10px 28px rgba(0, 0, 0, 0.18);
  color: inherit;
  cursor: pointer;
  transition: border-color 0.15s ease, transform 0.15s ease, background 0.15s ease;
}

.project-card.opening {
  pointer-events: none;
  border-color: #ff785a;
}

.project-card-opening {
  position: absolute;
  inset: 0;
  z-index: 2;
  display: grid;
  place-items: center;
  border-radius: inherit;
  background: rgba(17, 16, 15, 0.72);
  color: #ff9d85;
  font-size: 14px;
  font-weight: 700;
}

.project-card:hover {
  border-color: #514a43;
  background: #211e1b;
  transform: translateY(-2px);
}

.create-card {
  align-items: center;
  justify-content: center;
  gap: 14px;
  border-style: dashed;
  border-color: #3c3731;
  background: rgba(255, 120, 90, 0.04);
  box-shadow: none;
}

.create-card:hover {
  border-color: #ff785a;
  background: rgba(255, 120, 90, 0.09);
}

.create-plus {
  display: grid;
  place-items: center;
  width: 64px;
  height: 64px;
  border-radius: 18px;
  background: rgba(255, 120, 90, 0.14);
  color: #ff9d85;
}

.create-card strong {
  font-size: 16px;
  font-weight: 700;
  color: #f2ebe3;
}

.project-card-top {
  flex: 1;
}

.project-card-top h2 {
  margin: 0 0 12px;
  font-size: 18px;
  font-weight: 700;
  color: #f2ebe3;
  line-height: 1.35;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.project-card-top p {
  margin: 0;
  font-size: 13px;
  line-height: 1.6;
  color: #9a9288;
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.project-card-foot {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 20px;
  padding-top: 14px;
  border-top: 1px solid #2e2a26;
}

.project-card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.meta-chip {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 10px;
  border-radius: 999px;
  background: rgba(255, 120, 90, 0.1);
  color: #ff9d85;
  font-size: 12px;
  font-weight: 650;
}

.project-card-foot time {
  font-size: 12px;
  color: #7a736a;
}

@media (max-width: 720px) {
  .home-page {
    padding: 24px 16px 48px;
  }

  .home-header h1 {
    font-size: 28px;
  }

  .home-search-row {
    width: 100%;
  }

  .home-search {
    flex: 1;
    width: auto;
  }
}
</style>

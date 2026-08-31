<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Download, Document, Refresh, Setting } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useNovalyInject } from '@/composables/useNovalyInject'
import { saveBlobDownload } from '@/utils/downloadDir'

const props = defineProps<{
  mode?: 'full' | 'compact'
}>()

const route = useRoute()
const {
  logoDark,
  view,
  trashProjects,
  openSettings,
  goHome,
  openTts,
} = useNovalyInject()

const isFull = computed(() => props.mode === 'full')
const isWorkspace = computed(() => view.value === 'studio' && route.name !== 'editor')
const isSettings = computed(() => view.value === 'settings')
const isTts = computed(() => view.value === 'tts' || route.name === 'tts')

const logsOpen = ref(false)
const logsLoading = ref(false)
const logsDownloading = ref(false)
const logsContent = ref('')
const logsMeta = ref('')
const logsPre = ref<HTMLElement | null>(null)

async function fetchLogs() {
  logsLoading.value = true
  try {
    const r = await fetch('/api/logs?lines=800', { headers: { Accept: 'application/json' } })
    const data = await r.json().catch(() => ({}))
    if (!r.ok) throw new Error(data.error || `读取日志失败（HTTP ${r.status}）`)
    logsContent.value = data.content || (data.message || '（空日志）')
    const parts = []
    if (data.updatedAt) parts.push(`更新 ${new Date(data.updatedAt).toLocaleString('zh-CN')}`)
    if (typeof data.size === 'number') parts.push(`${Math.round(data.size / 1024)} KB`)
    if (data.exists === false) parts.push('文件尚未生成')
    logsMeta.value = parts.join(' · ')
    await nextTick()
    if (logsPre.value) logsPre.value.scrollTop = logsPre.value.scrollHeight
  } catch (e: any) {
    ElMessage.error(e?.message || '读取日志失败')
  } finally {
    logsLoading.value = false
  }
}

async function openLogs() {
  logsOpen.value = true
  await fetchLogs()
}

async function downloadLogs() {
  logsDownloading.value = true
  try {
    const r = await fetch('/api/logs/download')
    if (!r.ok) {
      const data = await r.json().catch(() => ({}))
      throw new Error(data.error || `下载失败（HTTP ${r.status}）`)
    }
    const blob = await r.blob()
    const cd = r.headers.get('Content-Disposition') || ''
    const m = /filename="([^"]+)"/.exec(cd)
    const filename = m?.[1] || `novaly-${Date.now()}.log`
    await saveBlobDownload(blob, filename)
    ElMessage.success('日志已开始下载')
  } catch (e: any) {
    ElMessage.error(e?.message || '下载日志失败')
  } finally {
    logsDownloading.value = false
  }
}
</script>

<template>
  <header v-if="isFull" class="site-header">
    <div class="site-header-inner">
      <button type="button" class="brand" @click="goHome">
        <img :src="logoDark" alt="Novaly" class="brand-logo" />
        <span class="brand-badge">Studio</span>
      </button>

      <nav class="site-nav">
        <button type="button" class="nav-link" :class="{ active: isWorkspace }" @click="goHome">工作区</button>
        <button type="button" class="nav-link" :class="{ active: isTts }" @click="openTts">分镜配音</button>
        <button type="button" class="nav-link" :class="{ active: isSettings }" @click="openSettings()">设置中心</button>
      </nav>

      <div class="site-actions">
        <el-button class="header-btn" :icon="Document" @click="openLogs">查看日志</el-button>
        <el-button class="header-btn" :icon="Download" :loading="logsDownloading" @click="downloadLogs">
          下载日志
        </el-button>
        <el-badge :value="trashProjects.length || undefined" :hidden="!trashProjects.length">
          <el-button circle class="header-icon-btn" :class="{ active: isSettings }" @click="openSettings()">
            <el-icon :size="18"><Setting /></el-icon>
          </el-button>
        </el-badge>
      </div>
    </div>
  </header>

  <div v-else class="app-topbar">
    <el-button class="topbar-btn" :icon="Document" @click="openLogs">查看日志</el-button>
    <el-button class="topbar-btn" :icon="Download" :loading="logsDownloading" @click="downloadLogs">
      下载日志
    </el-button>
    <el-badge :value="trashProjects.length || undefined" :hidden="!trashProjects.length" class="settings-badge">
      <el-button
        circle
        class="settings-btn"
        :class="{ active: view === 'settings' }"
        aria-label="设置中心"
        @click="openSettings()"
      >
        <el-icon :size="18"><Setting /></el-icon>
      </el-button>
    </el-badge>
  </div>

  <el-dialog
    v-model="logsOpen"
    title="服务日志"
    width="min(920px, 94vw)"
    class="logs-dialog"
    append-to-body
    destroy-on-close
  >
    <div class="logs-toolbar">
      <span class="logs-meta">{{ logsMeta || '最近输出' }}</span>
      <div class="logs-actions">
        <el-button size="small" :icon="Refresh" :loading="logsLoading" @click="fetchLogs">刷新</el-button>
        <el-button size="small" type="primary" :icon="Download" :loading="logsDownloading" @click="downloadLogs">
          下载
        </el-button>
      </div>
    </div>
    <pre ref="logsPre" class="logs-pre">{{ logsLoading && !logsContent ? '加载中…' : logsContent }}</pre>
  </el-dialog>
</template>

<style scoped>
.site-header {
  position: sticky;
  top: 0;
  z-index: 50;
  border-bottom: 1px solid #2a2622;
  background: rgba(17, 16, 15, 0.92);
  backdrop-filter: blur(12px);
}

.site-header-inner {
  display: flex;
  align-items: center;
  gap: 28px;
  max-width: 1280px;
  margin: 0 auto;
  padding: 14px 40px;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0;
  border: 0;
  background: none;
  cursor: pointer;
}

.brand-logo {
  height: 28px;
  width: auto;
  object-fit: contain;
}

.brand-badge {
  display: inline-flex;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  border-radius: 999px;
  border: 1px solid #3c3731;
  background: #1a1816;
  color: #b9afa5;
  font-size: 11px;
  font-weight: 700;
}

.site-nav {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
}

.nav-link {
  border: 0;
  background: transparent;
  color: #9a9288;
  font-size: 14px;
  font-weight: 600;
  padding: 8px 14px;
  border-radius: 999px;
  cursor: pointer;
}

.nav-link:hover {
  color: #eee9e1;
  background: rgba(255, 255, 255, 0.04);
}

.nav-link.active {
  color: #ff9d85;
  background: rgba(255, 120, 90, 0.12);
}

.site-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}

.header-btn {
  border: 1px solid #3c3731;
  background: #1a1816;
  color: #b9afa5;
}

.header-btn:hover {
  border-color: #5a5248;
  color: #eee9e1;
  background: #211e1b;
}

.header-icon-btn {
  width: 36px;
  height: 36px;
  border: 1px solid #3c3731;
  background: #1a1816;
  color: #b9afa5;
}

.header-icon-btn.active {
  border-color: #ff785a;
  color: #ff9d85;
  background: rgba(255, 120, 90, 0.12);
}

.app-topbar {
  position: fixed;
  top: 14px;
  right: 18px;
  z-index: 200;
  display: flex;
  align-items: center;
  gap: 8px;
}

.topbar-btn {
  border: 1px solid #3c3731;
  background: rgba(26, 24, 22, 0.92);
  color: #b9afa5;
  backdrop-filter: blur(8px);
}

.topbar-btn:hover {
  border-color: #5a5248;
  color: #eee9e1;
  background: rgba(40, 36, 32, 0.95);
}

.settings-btn {
  width: 40px;
  height: 40px;
  border: 1px solid #3c3731;
  background: rgba(26, 24, 22, 0.92);
  color: #b9afa5;
  backdrop-filter: blur(8px);
}

.settings-btn:hover {
  border-color: #5a5248;
  color: #eee9e1;
  background: rgba(40, 36, 32, 0.95);
}

.settings-btn.active {
  border-color: #ff785a;
  color: #ff9d85;
  background: rgba(255, 120, 90, 0.12);
}

.settings-badge :deep(.el-badge__content) {
  background: #ff785a;
  border: none;
}

.logs-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.logs-meta {
  font-size: 12px;
  color: #8f857c;
}

.logs-actions {
  display: flex;
  gap: 8px;
}

.logs-pre {
  margin: 0;
  max-height: min(62vh, 640px);
  overflow: auto;
  padding: 14px 16px;
  border-radius: 10px;
  border: 1px solid #2e2a26;
  background: #141210;
  color: #d7cdc2;
  font: 12px/1.55 'DM Mono', ui-monospace, monospace;
  white-space: pre-wrap;
  word-break: break-word;
}

@media (max-width: 900px) {
  .site-header-inner {
    flex-wrap: wrap;
    gap: 12px;
    padding: 12px 20px;
  }

  .site-nav {
    order: 3;
    width: 100%;
    overflow-x: auto;
  }

  .site-actions {
    margin-left: 0;
  }
}
</style>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Delete, Plus, Search, Upload } from '@element-plus/icons-vue'
import { useNovalyInject } from '@/composables/useNovalyInject'
import type { Episode } from '@/types'

const {
  active,
  activeEpisode,
  addingEpisode,
  addEpisode,
  extractingEpisodeIds,
  extractEpisodes,
  removeEpisode,
  removeEpisodes,
  focusEpisode,
  goToEpisode,
  saveEpisodeMeta,
  error,
} = useNovalyInject()

const query = ref('')
const selectedIds = ref<number[]>([])
const editorOpen = ref(false)
const editorTitle = ref('')
const editorScript = ref('')
const savingEditor = ref(false)
const assetHydrationTried = new Set<number>()

const episodes = computed(() => active.value?.episodes || [])

async function hydrateMissingEpisodeAssets() {
  const targets = episodes.value.filter(ep =>
    !!(ep.script || '').trim()
    && !(ep.assets || []).length
    && !assetHydrationTried.has(ep.id),
  )
  if (!targets.length) return
  for (const ep of targets) assetHydrationTried.add(ep.id)

  const hydrateOne = async (ep: Episode) => {
    try {
      const response = await fetch(`/api/episodes/${ep.id}/crew`, { headers: { Accept: 'application/json' } })
      if (!response.ok) return
      const data = await response.json().catch(() => ({}))
      const extracted = Array.isArray(data?.job?.assets) ? data.job.assets : []
      if (!extracted.length) return
      ep.assets = extracted
        .map((item: any) => ({ name: String(item?.name || '').trim(), type: String(item?.type || '').trim() }))
        .filter((item: { name: string; type: string }) => item.name && item.type)
    } catch {
      // 卡片仍可正常进入分镜；下次重新进入项目时由项目接口再次尝试。
    }
  }

  // 小批并发，避免几十集同时请求剧组状态。
  for (let i = 0; i < targets.length; i += 4) {
    await Promise.all(targets.slice(i, i + 4).map(hydrateOne))
  }
}

watch(
  () => episodes.value.map(ep => `${ep.id}:${(ep.assets || []).length}:${(ep.script || '').length}`).join('|'),
  () => { void hydrateMissingEpisodeAssets() },
  { immediate: true },
)

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return episodes.value
  return episodes.value.filter((ep) => {
    const hay = `${ep.title || ''} ${ep.script || ''} ${ep.number}`.toLowerCase()
    return hay.includes(q)
  })
})

const allVisibleSelected = computed(() =>
  filtered.value.length > 0 && filtered.value.every(ep => selectedIds.value.includes(ep.id)),
)

const extractingEpisodeNumbers = computed(() =>
  extractingEpisodeIds.value
    .map(id => episodes.value.find(ep => ep.id === id)?.number)
    .filter((n): n is number => typeof n === 'number')
    .sort((a, b) => a - b),
)

const extractingHint = computed(() => {
  const nums = extractingEpisodeNumbers.value
  if (!nums.length) return ''
  const label = nums.length === 1 ? `第${nums[0]}集` : `第${nums.join('、')}集`
  return `正在提取${label}的角色 / 场景 / 道具…`
})

function stripEpisodePrefix(text: string) {
  return text.replace(/^第\d+集[：:\s·\-]*/, '').trim()
}

function cardSubtitle(ep: Episode) {
  const t = (ep.title || '').trim()
  if (t && !/^第\d+集$/.test(t)) {
    const stripped = stripEpisodePrefix(t)
    return (stripped || t).slice(0, 48)
  }
  const line = (ep.script || '').split('\n').map(s => s.trim()).find(Boolean) || ''
  if (line) return stripEpisodePrefix(line).slice(0, 48) || line.slice(0, 48)
  return ''
}

function cardTitle(ep: Episode) {
  const sub = cardSubtitle(ep)
  const prefix = `第${ep.number}集`
  return sub ? `${prefix} · ${sub}` : prefix
}

function cardSummary(ep: Episode) {
  const lines = (ep.script || '').split('\n').map(s => s.trim()).filter(Boolean)
  const keyword = lines.find(l => /^关键词[：:]/.test(l))
  if (keyword) return keyword.replace(/^关键词[：:]\s*/, '')
  const highlight = lines.find(l => /^(看点|亮点)[：:]/.test(l))
  if (highlight) return highlight.replace(/^[^：:]+[：:]\s*/, '')
  const sub = cardSubtitle(ep)
  const rest = lines.filter(l => l !== sub && stripEpisodePrefix(l) !== sub).slice(0, 2)
  return rest.join(' ') || '还没有填写剧本正文'
}

function assetTypeLabel(type: string) {
  if (type === 'character') return '角色'
  if (type === 'scene') return '场景'
  if (type === 'prop') return '道具'
  return type
}

function isExtracting(ep: Episode) {
  return extractingEpisodeIds.value.includes(ep.id) || ep.crewStatus === 'running'
}

function extractingLabel(ep: Episode) {
  return `正在提取第${ep.number}集资产…`
}

function toggleSelect(id: number, on?: boolean) {
  const has = selectedIds.value.includes(id)
  const nextOn = on ?? !has
  selectedIds.value = nextOn
    ? [...new Set([...selectedIds.value, id])]
    : selectedIds.value.filter(x => x !== id)
}

function toggleSelectAll() {
  if (allVisibleSelected.value) {
    const visible = new Set(filtered.value.map(ep => ep.id))
    selectedIds.value = selectedIds.value.filter(id => !visible.has(id))
    return
  }
  selectedIds.value = [...new Set([...selectedIds.value, ...filtered.value.map(ep => ep.id)])]
}

async function openEditor(ep: Episode) {
  await focusEpisode(ep.number)
  editorTitle.value = ep.title || `第${ep.number}集`
  editorScript.value = ep.script || ''
  editorOpen.value = true
}

async function onCreate() {
  await addEpisode()
  const ep = activeEpisode.value
  if (!ep) return
  selectedIds.value = []
  editorTitle.value = ep.title || `第${ep.number}集`
  editorScript.value = ''
  editorOpen.value = true
}

function inferredTitle(script: string, fallback: string) {
  const line = script.split('\n').map(s => s.trim()).find(Boolean) || ''
  if (/^第\d+集/.test(line) && line.length < 80) return line
  return fallback.trim() || line.slice(0, 40)
}

async function saveEditor(close = true) {
  if (!activeEpisode.value) return false
  savingEditor.value = true
  try {
    const script = editorScript.value
    const title = inferredTitle(script, editorTitle.value)
    editorTitle.value = title
    await saveEpisodeMeta({ script, title })
    if (close) editorOpen.value = false
    return true
  } catch (e: any) {
    error.value = e.message || '保存剧本失败'
    return false
  } finally {
    savingEditor.value = false
  }
}

async function extractCurrent() {
  if (!activeEpisode.value) return
  const ok = await saveEditor(false)
  if (!ok) return
  const id = activeEpisode.value.id
  editorOpen.value = false
  await extractEpisodes([id])
}

async function extractSelected() {
  await extractEpisodes(selectedIds.value)
}

function deleteSelected() {
  const eps = episodes.value.filter(ep => selectedIds.value.includes(ep.id))
  removeEpisodes(eps)
  selectedIds.value = []
}

function goShots(ep: Episode) {
  editorOpen.value = false
  void goToEpisode(ep.number)
}
</script>

<template>
  <div class="scripts-panel">
    <div class="scripts-toolbar">
      <el-input
        v-model="query"
        class="scripts-search"
        placeholder="搜索剧本名称…"
        clearable
        :prefix-icon="Search"
      />
      <el-button type="primary" :icon="Plus" :loading="addingEpisode" @click="onCreate">新建剧本</el-button>
      <div class="scripts-toolbar-spacer" />
      <el-button @click="toggleSelectAll">{{ allVisibleSelected ? '取消全选' : '全选' }}</el-button>
      <el-button :icon="Upload" :loading="!!extractingEpisodeIds.length" @click="extractSelected">
        提取资产
      </el-button>
      <el-button type="danger" plain :icon="Delete" :disabled="selectedIds.length < 1 || episodes.length <= 1" @click="deleteSelected">
        批量删除剧本
      </el-button>
    </div>

    <p v-if="extractingHint" class="scripts-hint">
      {{ extractingHint }}
    </p>

    <el-empty v-if="!filtered.length" :description="query ? '没有匹配的剧本' : '还没有剧本'">
      <el-button type="primary" :icon="Plus" :loading="addingEpisode" @click="onCreate">新建剧本</el-button>
    </el-empty>

    <div v-else class="script-grid">
      <article
        v-for="ep in filtered"
        :key="ep.id"
        class="script-card"
        :class="{ selected: selectedIds.includes(ep.id), extracting: isExtracting(ep) }"
        @click="goShots(ep)"
      >
        <div class="script-card-head">
          <h4>{{ cardTitle(ep) }}</h4>
          <el-checkbox
            :model-value="selectedIds.includes(ep.id)"
            @click.stop
            @change="(on: boolean) => toggleSelect(ep.id, on)"
          />
        </div>
        <p class="script-card-summary">{{ cardSummary(ep) }}</p>
        <div v-if="(ep.assets || []).length" class="script-tags">
          <span
            v-for="(asset, i) in (ep.assets || []).slice(0, 16)"
            :key="asset.name + i"
            class="script-tag"
            :title="assetTypeLabel(asset.type)"
          >{{ asset.name }}</span>
          <span v-if="(ep.assets || []).length > 16" class="script-tag more">+{{ (ep.assets || []).length - 16 }}</span>
        </div>
        <p v-else-if="isExtracting(ep)" class="script-card-status">{{ extractingLabel(ep) }}</p>
        <p v-else-if="!(ep.script || '').trim()" class="script-card-status">点下方「编辑」填写本集剧本</p>
        <p v-else class="script-card-status">已填写，点「提取资产」写入资源库</p>
        <button
          type="button"
          class="script-card-edit"
          title="编辑这一集剧本"
          @click.stop="openEditor(ep)"
        >
          编辑
        </button>
        <button
          v-if="episodes.length > 1"
          type="button"
          class="script-card-del"
          title="删除这一集"
          @click.stop="removeEpisode(ep)"
        >
          删除
        </button>
      </article>
    </div>

    <el-dialog
      v-model="editorOpen"
      :title="activeEpisode ? `第${activeEpisode.number}集剧本` : '剧本'"
      width="720px"
      class="script-editor-dialog"
      align-center
      destroy-on-close
    >
      <el-form label-position="top">
        <el-form-item label="标题">
          <el-input v-model="editorTitle" placeholder="第1集：庆功夜的刀" />
        </el-form-item>
        <el-form-item label="剧本">
          <el-input
            v-model="editorScript"
            type="textarea"
            :rows="14"
            placeholder="粘贴本集剧本。可写标题、关键词、看点，再写正文。保存后勾选卡片，点「提取资产」。"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editorOpen = false">取消</el-button>
        <el-button v-if="activeEpisode" @click="goShots(activeEpisode)">进入分镜</el-button>
        <el-button :loading="!!extractingEpisodeIds.length" @click="extractCurrent">提取资产</el-button>
        <el-button type="primary" :loading="savingEditor" @click="() => saveEditor()">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.scripts-panel {
  min-width: 0;
}

.scripts-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
}

.scripts-search {
  width: min(280px, 100%);
}

.scripts-toolbar-spacer {
  flex: 1;
}

.scripts-hint {
  margin: 0 0 14px;
  font-size: 13px;
  color: #ff9d85;
}

.script-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
}

.script-card {
  position: relative;
  background: #211e1b;
  border: 1px solid #37312d;
  border-radius: 14px;
  padding: 16px 16px 40px;
  cursor: pointer;
  min-height: 168px;
  transition: border-color 0.15s ease, background 0.15s ease;
}

.script-card:hover,
.script-card.selected {
  border-color: #ff785a;
  background: #26221e;
}

.script-card.extracting {
  border-color: #c9a227;
}

.script-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.script-card-head h4 {
  margin: 0;
  font-size: 16px;
  font-weight: 750;
  color: #f2ebe3;
  line-height: 1.35;
}

.script-card-summary {
  margin: 10px 0 12px;
  font-size: 13px;
  line-height: 1.55;
  color: #b9afa5;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.script-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.script-tag {
  font-size: 11px;
  color: #d8d0c6;
  border: 1px solid #4a433c;
  border-radius: 999px;
  padding: 2px 8px;
  background: #1a1816;
}

.script-tag.more {
  color: #8f8880;
}

.script-card-status {
  margin: 8px 0 0;
  font-size: 12px;
  color: #8f8880;
}

.script-card-del {
  position: absolute;
  right: 12px;
  bottom: 10px;
  border: 0;
  background: transparent;
  color: #6a635c;
  font-size: 12px;
  cursor: pointer;
}

.script-card-edit {
  position: absolute;
  left: 12px;
  bottom: 10px;
  border: 0;
  background: transparent;
  color: #ff9b82;
  font-size: 12px;
  cursor: pointer;
}

.script-card-edit:hover {
  color: #ffd0c4;
}

.script-card-del:hover {
  color: #ffb6a6;
}
</style>

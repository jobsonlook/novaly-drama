<script setup lang="ts">
import { computed, ref } from 'vue'
import { Box, MoreFilled, Plus, Refresh, Setting } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import { useNovalyInject } from '@/composables/useNovalyInject'
import EpisodesPanel from '@/components/studio/EpisodesPanel.vue'
import ScriptsPanel from '@/components/studio/ScriptsPanel.vue'
import ResourcePanel from '@/components/studio/ResourcePanel.vue'
import ProjectMetaFields from '@/components/studio/ProjectMetaFields.vue'

const {
  active,
  activeEpisode,
  error,
  saving,
  refreshingStudio,
  projectLoading,
  studioTab,
  showAddForm,
  characterCandidates,
  visibleImageGenJobs,
  focusedImageJobId,
  hasReadyImageJob,
  openResourceGenerateJob,
  dismissImageJobFromPanel,
  stylizeJobs,
  dismissStylizeJob,
  focusStylizeJob,
  saveProject,
  refreshStudio,
  deleteActiveProject,
  resourceFormTypeLabel,
  addEpisode,
  addingEpisode,
  goToEpisode,
  crewJob,
  crewModalOpen,
  openCrewModal,
} = useNovalyInject()

const configOpen = ref(false)
const router = useRouter()

function onMenuCommand(cmd: string) {
  if (cmd === 'delete') deleteActiveProject()
}

function openScripts() {
  studioTab.value = 'scripts'
}

function openAssets() {
  studioTab.value = 'resources'
}

function openShots() {
  if (activeEpisode.value?.number) void goToEpisode(activeEpisode.value.number)
  else studioTab.value = 'episodes'
}

function openEditor() {
  if (!active.value?.id || !activeEpisode.value?.id) return
  void router.push(`/projects/${active.value.id}/editor/${activeEpisode.value.id}`)
}

function jobStatusLabel(status: string) {
  if (status === 'pending' || status === 'running') return '生成中'
  if (status === 'completed') return '已完成'
  if (status === 'failed') return '失败'
  return status
}

const runningJobCount = computed(
  () =>
    visibleImageGenJobs.value.filter(j => j.status === 'pending' || j.status === 'running').length
    + stylizeJobs.value.filter(j => j.status === 'running').length
    + (crewJob.value?.status === 'running' && !crewModalOpen.value ? 1 : 0),
)
const totalJobCount = computed(
  () => visibleImageGenJobs.value.length + stylizeJobs.value.length + (showCrewTask.value ? 1 : 0),
)

const showCrewTask = computed(() => crewJob.value?.status === 'running' && !crewModalOpen.value)
const crewProgress = computed(() => {
  const stage = crewJob.value?.stage
  if (stage === 'screenwriter') return 15
  if (stage === 'director' || stage === 'consistency') return 35
  if (stage === 'assets') return 55
  if (stage === 'storyboard') return 78
  if (stage === 'qc') return 92
  return 8
})
const crewStageLabel = computed(() => {
  const stage = crewJob.value?.stage
  if (stage === 'screenwriter') return '正在标准化剧本'
  if (stage === 'director' || stage === 'consistency') return '正在提取并核对资产'
  if (stage === 'assets') return '正在生成资产图片'
  if (stage === 'storyboard') return '正在拆分并校正分镜'
  if (stage === 'qc') return '正在修改与复检'
  return '剧组正在处理'
})

const showJobPanel = computed(
  () =>
    !!(showCrewTask.value || visibleImageGenJobs.value.length || hasReadyImageJob.value || stylizeJobs.value.length),
)

const projectBadge = computed(() => {
  const title = active.value?.title?.trim() || '未命名项目'
  if (studioTab.value === 'scripts') return title
  const ep = activeEpisode.value?.number
  return ep ? `${title} · 剧集 ${ep}` : title
})

const episodeOptions = computed(() =>
  (active.value?.episodes || []).map(ep => ({
    label: `剧集 ${ep.number}`,
    value: ep.number,
  })),
)

async function onSaveConfig() {
  await saveProject()
  configOpen.value = false
}

function fillTimeTravelStyle() {
  if (!active.value) return
  const template = '穿越剧双时空风格：现代时空采用当代都市真人电影质感，自然光、冷暖克制、现代服装与建筑；古代时空采用东方古装影视质感，传统服饰与建筑、暖色烛火和典雅电影光影。两个时空保持同一套人物面貌、镜头语言与写实度，仅用时代陈设、服装、色温和光线区分，禁止现代物件误入古代。'
  active.value.style = template
}
</script>

<template>
  <section
    v-if="active"
    class="studio-page"
    v-loading="projectLoading"
    element-loading-text="正在加载项目…"
    element-loading-background="rgba(17, 16, 15, 0.72)"
  >
    <div class="studio-stage-nav">
      <button
        type="button"
        class="stage-pill"
        :class="{ active: studioTab === 'scripts' }"
        @click="openScripts"
      >
        剧本
      </button>
      <button
        type="button"
        class="stage-pill"
        :class="{ active: studioTab === 'episodes' }"
        @click="openShots"
      >
        分镜
      </button>
      <button
        type="button"
        class="stage-pill"
        :class="{ active: studioTab === 'resources' }"
        @click="openAssets"
      >
        资源
      </button>
      <button
        type="button"
        class="stage-pill"
        @click="openEditor"
      >
        剪辑台
      </button>
    </div>

    <div class="studio-toolbar">
      <div class="project-badge" :title="projectBadge">
        <span class="badge-dot" />
        <span class="badge-text">{{ projectBadge }}</span>
        <el-select
          v-if="studioTab !== 'scripts'"
          :model-value="activeEpisode?.number"
          size="small"
          class="episode-switch"
          @change="(n: number) => goToEpisode(n)"
        >
          <el-option
            v-for="opt in episodeOptions"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
        <el-button
          v-if="studioTab !== 'scripts'"
          size="small"
          :icon="Plus"
          :loading="addingEpisode"
          title="添加一集剧本"
          @click="addEpisode"
        >
          添加一集
        </el-button>
      </div>

      <div class="studio-actions">
        <el-button :icon="Box" @click="openAssets">资产库</el-button>
        <el-button type="primary" :icon="Setting" @click="configOpen = true">项目配置</el-button>
        <el-button
          :icon="Refresh"
          circle
          :loading="refreshingStudio"
          title="从服务器重新拉取"
          @click="refreshStudio"
        />
        <el-dropdown trigger="click" @command="onMenuCommand">
          <el-button :icon="MoreFilled" circle />
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="delete">移入回收站</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </div>

    <el-alert
      v-if="error"
      :title="error"
      type="error"
      show-icon
      :closable="false"
      class="studio-error"
    />

    <div class="studio-body">
      <div class="studio-main">
        <ScriptsPanel v-show="studioTab === 'scripts'" />
        <EpisodesPanel v-show="studioTab === 'episodes'" />
        <ResourcePanel v-show="studioTab === 'resources'" />
      </div>
    </div>

    <Teleport to="body">
      <div v-if="showJobPanel" class="ai-job-panel">
        <div class="ai-job-panel-head">
          <strong>生成任务</strong>
          <span v-if="runningJobCount">进行中 {{ runningJobCount }} · 共 {{ totalJobCount }} 项</span>
          <span v-else-if="hasReadyImageJob">候选已就绪 · 共 {{ totalJobCount }} 项</span>
          <span v-else>已完成 · 共 {{ totalJobCount }} 项</span>
        </div>
        <div class="ai-job-list">
          <div
            v-if="showCrewTask"
            class="ai-job-row crew-task-row on"
            role="button"
            tabindex="0"
            @click="openCrewModal"
            @keydown.enter="openCrewModal"
          >
            <div class="ai-job-row-top">
              <span class="ai-job-type">AI 剧组</span>
              <b class="ai-job-name">{{ activeEpisode?.title || `第${activeEpisode?.number || ''}集` }}</b>
              <span class="ai-job-status">执行中</span>
            </div>
            <el-progress
              :percentage="crewProgress"
              :stroke-width="6"
              :show-text="true"
              :format="() => `${crewProgress}%`"
              :indeterminate="crewProgress < 10"
            />
            <span class="ai-job-msg">{{ crewStageLabel }} · 点击查看详情</span>
          </div>
          <div
            v-for="job in stylizeJobs"
            :key="'stylize-' + job.id"
            class="ai-job-row"
            :class="{ failed: job.status === 'failed', done: job.status === 'completed' }"
            role="button"
            tabindex="0"
            @click="focusStylizeJob(job)"
            @keydown.enter="focusStylizeJob(job)"
          >
            <div class="ai-job-row-top">
              <span class="ai-job-type">非真人</span>
              <b class="ai-job-name" :title="job.name">{{ job.name }}</b>
              <span class="ai-job-status">{{ jobStatusLabel(job.status) }}</span>
              <button
                v-if="job.status === 'completed' || job.status === 'failed'"
                type="button"
                class="ai-job-dismiss"
                title="关闭"
                @click="dismissStylizeJob(job.id, $event)"
              >×</button>
            </div>
            <el-progress
              v-if="job.status === 'running'"
              :percentage="30"
              :stroke-width="6"
              :show-text="false"
              indeterminate
            />
            <span class="ai-job-msg">{{ job.message }}</span>
          </div>
          <div
            v-for="job in visibleImageGenJobs"
            :key="job.id"
            class="ai-job-row"
            :class="{ on: focusedImageJobId === job.id, failed: job.status === 'failed', done: job.status === 'completed' }"
            role="button"
            tabindex="0"
            @click="openResourceGenerateJob(job.id)"
            @keydown.enter="openResourceGenerateJob(job.id)"
          >
            <div class="ai-job-row-top">
              <span class="ai-job-type">{{ resourceFormTypeLabel(job.type) }}</span>
              <b class="ai-job-name" :title="job.name">{{ job.name }}</b>
              <span class="ai-job-status">{{ jobStatusLabel(job.status) }}</span>
              <button
                v-if="job.status === 'completed' || job.status === 'failed'"
                type="button"
                class="ai-job-dismiss"
                title="关闭"
                @click="dismissImageJobFromPanel(job.id, $event)"
              >×</button>
            </div>
            <el-progress
              v-if="job.status === 'pending' || job.status === 'running'"
              :percentage="Math.max(job.progress || 0, 1)"
              :stroke-width="6"
              :show-text="true"
              :format="() => `${job.progress || 0}%`"
              :indeterminate="!job.progress || job.progress < 8"
            />
            <span class="ai-job-msg">
              {{ job.message || '生成中…' }}
              <template v-if="job.totalCount">（{{ job.doneCount ?? 0 }}/{{ job.totalCount }}）</template>
              {{ job.status === 'completed' ? ' · 点击查看' : '' }}
            </span>
          </div>
          <button
            v-if="!visibleImageGenJobs.length && !stylizeJobs.length && characterCandidates.length"
            type="button"
            class="ai-job-row done"
            @click="openResourceGenerateJob()"
          >
            <span class="ai-job-msg">候选图已就绪（{{ characterCandidates.length }} 张）· 点击选择并添加</span>
          </button>
        </div>
        <div v-if="totalJobCount > 3" class="ai-job-panel-foot">上下滚动查看全部 {{ totalJobCount }} 项任务</div>
      </div>
    </Teleport>

    <el-dialog
      v-model="configOpen"
      title="项目配置"
      width="980px"
      class="studio-config-dialog"
      align-center
      destroy-on-close
    >
      <el-form v-if="active" label-position="top">
        <ProjectMetaFields v-model="active" />
        <el-form-item class="style-override-field">
          <template #label>
            <div class="config-field-head">
              <span>风格补充说明</span>
              <el-button size="small" plain @click="fillTimeTravelStyle">填入穿越剧双时空模板</el-button>
            </div>
          </template>
          <el-input
            v-model="active.style"
            type="textarea"
            :rows="5"
            resize="vertical"
            placeholder="可补充或覆盖视觉手册。例如分别说明现代时空、古代时空的服装、场景、色调与光影；单个分镜还可在高级设置里填写本镜风格。"
          />
          <div class="config-field-hint">视觉手册负责基础画风；这里负责全项目的补充规则。穿越剧可分别定义现代段与古代段，保存后会用于素材绘制和视频提示词。</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="configOpen = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="onSaveConfig">保存</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<style scoped>
.studio-page {
  position: relative;
  max-width: 1200px;
  margin: 0 auto;
  padding: 20px 28px 56px;
  min-height: calc(100vh - 64px);
}

.studio-page :deep(.el-loading-mask) {
  border-radius: 12px;
}

.config-field-head {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.config-field-hint {
  margin-top: 7px;
  color: #8f877e;
  font-size: 12px;
  line-height: 1.55;
}

.studio-stage-nav {
  display: flex;
  justify-content: center;
  gap: 8px;
  margin-bottom: 22px;
}

.stage-pill {
  border: 0;
  background: transparent;
  color: #9a9288;
  font-size: 14px;
  font-weight: 650;
  padding: 10px 22px;
  border-radius: 999px;
  cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease;
}

.stage-pill:hover {
  color: #eee9e1;
  background: rgba(255, 255, 255, 0.04);
}

.stage-pill.active {
  color: #25120e;
  background: #ff785a;
}

.studio-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 20px;
}

.project-badge {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  max-width: min(720px, 100%);
  min-height: 40px;
  padding: 6px 10px 6px 12px;
  border-radius: 999px;
  border: 1px solid #3c3731;
  background: linear-gradient(90deg, rgba(255, 120, 90, 0.16), rgba(26, 24, 22, 0.9));
  color: #f2ebe3;
}

.badge-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #ff785a;
  flex-shrink: 0;
}

.badge-text {
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.episode-switch {
  width: 96px;
}

.studio-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-left: auto;
}

.studio-error {
  margin: 0 0 16px;
}

.studio-body {
  min-width: 0;
}

.studio-main {
  flex: 1;
  min-width: 0;
}

@media (max-width: 720px) {
  .studio-page {
    padding: 16px 14px 48px;
  }

  .studio-toolbar {
    align-items: stretch;
  }

  .project-badge {
    max-width: 100%;
  }

  .studio-actions {
    margin-left: 0;
    width: 100%;
  }
}
</style>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'
import type { CrewAsset, CrewStage } from '@/types'
import CrewChatPanel from '@/components/studio/CrewChatPanel.vue'

const {
  crewModalOpen,
  crewJob,
  crewBusy,
  crewShotConflict,
  shotTotal,
  closeCrewModal,
  continueCrewPipeline,
  retryCrewPipeline,
  rewindCrewPipeline,
  updateCrewScript,
  updateCrewPlan,
  updateCrewAssets,
  studioTab,
} = useNovalyInject()

const steps: { key: CrewStage; title: string; hint: string }[] = [
  { key: 'screenwriter', title: '编剧', hint: '标准化剧本' },
  { key: 'consistency', title: '资产', hint: '提取并写提示词' },
  { key: 'assets', title: '生图', hint: '批量角色/场景/道具' },
  { key: 'storyboard', title: '分镜', hint: '对话拆镜并绑定参考' },
  { key: 'qc', title: '质检', hint: '对话质检并按建议改稿' },
]

function stageIndex(stage: string | undefined) {
  if (stage === 'director' || stage === 'consistency') return 1
  const i = steps.findIndex(s => s.key === stage)
  return i < 0 ? 0 : i
}

const pipelineIndex = computed(() => stageIndex(crewJob.value?.stage))
const viewIndex = ref(0)
const running = computed(() => crewJob.value?.status === 'running')
const waiting = computed(() => crewJob.value?.status === 'waiting_review')
const failed = computed(() => crewJob.value?.status === 'failed')
const completed = computed(() => crewJob.value?.status === 'completed')
const maxReachedIndex = computed(() => (completed.value ? 4 : pipelineIndex.value))
const viewingCurrent = computed(() => viewIndex.value === pipelineIndex.value)
const canEdit = computed(() => waiting.value && viewingCurrent.value && !running.value)

watch(crewModalOpen, (open) => {
  if (open) viewIndex.value = pipelineIndex.value
})
watch(
  () => crewJob.value?.id,
  () => { viewIndex.value = pipelineIndex.value },
)
watch(
  () => crewJob.value?.stage,
  () => { viewIndex.value = pipelineIndex.value },
)

const existingShotCount = computed(() => crewShotConflict.value || shotTotal.value || 0)
const nextStepIndex = computed(() => pipelineIndex.value + 1)

function canViewStep(i: number) {
  if (i <= maxReachedIndex.value) return true
  return waiting.value && viewingCurrent.value && i === nextStepIndex.value
}

function selectStep(i: number) {
  if (i <= maxReachedIndex.value) {
    viewIndex.value = i
    return
  }
  if (!waiting.value || i !== nextStepIndex.value) return
  if (crewShotConflict.value) return
  if (i >= 4) {
    viewIndex.value = i
    return
  }
  continueCrewPipeline()
}

const statusLabel = computed(() => {
  if (!crewJob.value) return '等待启动'
  if (running.value) return '执行中'
  if (waiting.value) return '待确认'
  if (failed.value) return '失败'
  if (completed.value) return '已完成'
  return crewJob.value.status
})

const canContinue = computed(() => {
  if (!waiting.value || !viewingCurrent.value || crewBusy.value) return false
  return viewIndex.value < 3
})
const canComplete = computed(() => {
  if (!waiting.value || !viewingCurrent.value || crewBusy.value) return false
  return viewIndex.value === 4 && crewJob.value?.stage === 'qc'
})
const canRetry = computed(() => {
  if (!viewingCurrent.value || crewBusy.value) return false
  if (viewIndex.value >= 3) return false
  if (failed.value) return true
  return waiting.value && !completed.value
})
const canRewind = computed(() => {
  if (!crewJob.value || running.value || crewBusy.value) return false
  if (viewIndex.value > maxReachedIndex.value || viewIndex.value >= 4) return false
  if (completed.value) return true
  return viewIndex.value < pipelineIndex.value
})

const continueLabel = computed(() => {
  const stage = crewJob.value?.stage
  if (stage === 'screenwriter') return '确认剧本，提取资产'
  if (stage === 'director' || stage === 'consistency') {
    return allAssetsReusable.value ? '复用已有图，直接拆镜' : '确认资产，开始生图'
  }
  if (stage === 'assets') return '确认图片，进入拆镜'
  if (stage === 'qc') return '完成'
  return '完成'
})

const assetDraft = computed({
  get: () => crewJob.value?.assets || [],
  set: (v: CrewAsset[]) => updateCrewAssets(v),
})

const allAssetsReusable = computed(() => {
  const list = assetDraft.value.filter(a => (a.name || '').trim())
  if (!list.length) return true
  return list.every(a => !!(a.skipped || a.reused))
})

function assetReuseHint(asset: CrewAsset) {
  if (asset.reused) return '已有图，将复用'
  if (asset.skipped) return '已跳过生图'
  return ''
}

function assetTypeLabel(type: string) {
  if (type === 'character') return '角色'
  if (type === 'scene') return '场景'
  if (type === 'prop') return '道具'
  return type
}

function addAsset(type: CrewAsset['type']) {
  updateCrewAssets([
    ...assetDraft.value,
    { name: '', type, description: '', prompt: '', priority: 2 },
  ])
}

function removeAsset(index: number) {
  updateCrewAssets(assetDraft.value.filter((_, i) => i !== index))
}

function patchAsset(index: number, patch: Partial<CrewAsset>) {
  updateCrewAssets(assetDraft.value.map((a, i) => (i === index ? { ...a, ...patch } : a)))
}

async function goResources() {
  closeCrewModal()
  studioTab.value = 'resources'
}

function rewindViewedStep() {
  const key = steps[viewIndex.value]?.key
  if (key) rewindCrewPipeline(key)
}
</script>

<template>
  <el-dialog
    :model-value="crewModalOpen"
    title="AI 剧组制作"
    width="920px"
    class="modal-wide crew-dialog"
    align-center
    :close-on-click-modal="false"
    @close="closeCrewModal"
  >
    <div class="crew-body">
      <div class="crew-pipeline-fixed">
        <div class="crew-steps">
          <div
            v-for="(step, i) in steps"
            :key="step.key"
            class="crew-step"
            :class="{
              active: i === viewIndex,
              done: i < pipelineIndex || completed,
              clickable: canViewStep(i),
              next: waiting && viewingCurrent && i === nextStepIndex,
              locked: !canViewStep(i),
            }"
            role="button"
            :tabindex="canViewStep(i) ? 0 : -1"
            :aria-disabled="!canViewStep(i)"
            @click="selectStep(i)"
            @keydown.enter.prevent="selectStep(i)"
            @keydown.space.prevent="selectStep(i)"
          >
            <span class="crew-step-index">{{ i + 1 }}</span>
            <div>
              <b>{{ step.title }}</b>
              <p>{{ step.hint }}</p>
            </div>
          </div>
        </div>

        <div class="crew-status-row">
          <el-tag :type="failed ? 'danger' : running ? 'warning' : completed ? 'success' : 'info'" effect="dark" size="small">
            {{ statusLabel }}
          </el-tag>
          <span v-if="running" class="crew-running">Agent 正在工作，请稍候…</span>
          <span v-else-if="crewShotConflict" class="crew-running">本集已有 {{ existingShotCount }} 个分镜，请先选择替换还是追加</span>
          <span v-else-if="viewIndex >= 3" class="crew-running">用对话拆镜、质检和改稿。监制只审核，改稿需你确认。</span>
          <span v-else-if="waiting && viewingCurrent" class="crew-running">请检查下方产物，确认后进入下一阶段。也可直接点下一步。</span>
          <span v-else-if="canRewind" class="crew-running">正在查看已完成步骤，可点「从这步重做」再往下走</span>
        </div>
      </div>

      <div class="crew-pipeline-scroll">
        <el-alert
          v-if="crewJob?.errorMessage"
          :title="crewJob.errorMessage"
          type="error"
          show-icon
          :closable="false"
          class="crew-alert"
        />

      <div v-if="crewShotConflict" class="crew-conflict">
        <p>本集已有 {{ existingShotCount || crewShotConflict }} 个分镜。重新拆镜会先删除旧分镜；追加则接到现有分镜后面。</p>
        <el-button type="primary" :loading="crewBusy" @click="continueCrewPipeline({ shotMode: 'replace', skipImages: viewIndex === 1 })">替换并重新拆镜</el-button>
        <el-button :loading="crewBusy" @click="continueCrewPipeline({ shotMode: 'append', skipImages: viewIndex === 1 })">保留旧分镜，追加到末尾</el-button>
      </div>

        <template v-if="crewJob">
        <section v-if="viewIndex === 0" class="crew-section">
          <h4>编剧剧本</h4>
          <el-input
            :model-value="crewJob.scriptDraft"
            type="textarea"
            :rows="14"
            :disabled="!canEdit"
            placeholder="标准化后的分集剧本"
            @update:model-value="updateCrewScript"
          />
        </section>

        <section v-if="viewIndex === 1" class="crew-section">
          <h4>导演规划</h4>
          <el-input
            :model-value="crewJob.directorPlan"
            type="textarea"
            :rows="5"
            :disabled="!canEdit"
            placeholder="节奏、场次与优先级"
            @update:model-value="updateCrewPlan"
          />
          <div class="crew-asset-head">
            <h4>角色 / 场景 / 道具</h4>
            <div class="crew-asset-add">
              <el-button size="small" :disabled="!canEdit" @click="addAsset('character')">加角色</el-button>
              <el-button size="small" :disabled="!canEdit" @click="addAsset('scene')">加场景</el-button>
              <el-button size="small" :disabled="!canEdit" @click="addAsset('prop')">加道具</el-button>
            </div>
          </div>
          <p class="crew-hint">可改名称和提示词；已有同名成图会自动勾选「跳过生图」并复用。想先测分镜，也可点「跳过生图，直接拆镜」。</p>
          <div v-if="!assetDraft.length" class="crew-empty">没有提取到资产。可手动添加，或直接进入分镜。</div>
          <div v-for="(asset, i) in assetDraft" :key="i" class="crew-asset">
            <div class="crew-asset-row">
              <el-select :model-value="asset.type" size="small" :disabled="!canEdit" style="width: 92px" @change="(v: string) => patchAsset(i, { type: v })">
                <el-option label="角色" value="character" />
                <el-option label="场景" value="scene" />
                <el-option label="道具" value="prop" />
              </el-select>
              <el-input :model-value="asset.name" size="small" placeholder="名称" :disabled="!canEdit" @update:model-value="(v: string) => patchAsset(i, { name: v })" />
              <el-checkbox :model-value="!!asset.skipped || !!asset.reused" :disabled="!canEdit" @change="(v: boolean) => patchAsset(i, { skipped: v, reused: v ? asset.reused : false })">跳过生图</el-checkbox>
              <el-button text type="danger" size="small" :disabled="!canEdit" @click="removeAsset(i)">删除</el-button>
            </div>
            <p v-if="assetReuseHint(asset)" class="crew-asset-reuse">{{ assetReuseHint(asset) }}</p>
            <el-input :model-value="asset.description" size="small" placeholder="剧情描述" :disabled="!canEdit" @update:model-value="(v: string) => patchAsset(i, { description: v })" />
            <el-input
              v-if="asset.type === 'character'"
              :model-value="asset.voicePrompt"
              size="small"
              placeholder="音色提示词，各分镜视频共用同一句"
              :disabled="!canEdit"
              @update:model-value="(v: string) => patchAsset(i, { voicePrompt: v })"
            />
            <el-input
              :model-value="asset.prompt"
              type="textarea"
              :rows="2"
              size="small"
              placeholder="绘图提示词"
              :disabled="!canEdit"
              @update:model-value="(v: string) => patchAsset(i, { prompt: v })"
            />
          </div>
        </section>

        <section v-if="viewIndex === 2" class="crew-section">
          <h4>批量生图</h4>
          <p class="crew-hint">任务会进入右下角生成面板。完成后可去资源库换主图，再继续分镜。</p>
          <div v-for="asset in assetDraft" :key="asset.name + asset.type" class="crew-asset-status">
            <el-tag size="small" effect="plain">{{ assetTypeLabel(asset.type) }}</el-tag>
            <b>{{ asset.name || '未命名' }}</b>
            <span v-if="asset.skipped">已跳过</span>
            <span v-else-if="asset.reused">复用已有资源</span>
            <span v-else-if="asset.error" class="crew-err">{{ asset.error }}</span>
            <span v-else-if="running && viewingCurrent">生成中…</span>
            <span v-else-if="asset.jobId || asset.resourceId">已生成</span>
            <span v-else>等待</span>
          </div>
          <el-button size="small" @click="goResources">打开资源库</el-button>
        </section>

        <section v-if="viewIndex >= 3" class="crew-section crew-section-chat">
          <CrewChatPanel embedded />
        </section>
        </template>
      </div>
    </div>

    <template #footer>
      <el-button @click="closeCrewModal">关闭</el-button>
      <el-button v-if="canRetry" :disabled="crewBusy" @click="retryCrewPipeline">重试本阶段</el-button>
      <el-button v-if="canRewind" :disabled="crewBusy" @click="rewindViewedStep">从这步重做</el-button>
      <el-button
        v-if="canContinue && viewIndex === 1 && !crewShotConflict && !allAssetsReusable"
        :disabled="crewBusy"
        @click="continueCrewPipeline({ skipImages: true })"
      >
        跳过生图，直接拆镜
      </el-button>
      <el-button
        v-if="crewShotConflict"
        type="primary"
        :loading="crewBusy"
        @click="continueCrewPipeline({ shotMode: 'replace', skipImages: viewIndex === 1 })"
      >
        替换旧分镜并继续
      </el-button>
      <el-button
        v-if="crewShotConflict"
        :loading="crewBusy"
        @click="continueCrewPipeline({ shotMode: 'append', skipImages: viewIndex === 1 })"
      >
        保留旧分镜，追加
      </el-button>
      <el-button
        v-if="canContinue && !crewShotConflict"
        type="primary"
        :loading="crewBusy"
        @click="continueCrewPipeline(viewIndex === 1 && allAssetsReusable ? { skipImages: true } : undefined)"
      >
        {{ continueLabel }}
      </el-button>
      <el-button
        v-if="canComplete"
        type="primary"
        :loading="crewBusy"
        @click="continueCrewPipeline()"
      >
        完成
      </el-button>
    </template>
  </el-dialog>
</template>

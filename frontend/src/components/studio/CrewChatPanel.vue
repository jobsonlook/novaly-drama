<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useNovalyInject } from '@/composables/useNovalyInject'

defineProps<{
  embedded?: boolean
}>()

const {
  crewJob,
  crewBusy,
  crewChatBusy,
  sendCrewChat,
  reconnectCrewChat,
  clearCrewMemory,
  jumpCrewShot,
  fixCrewPipeline,
} = useNovalyInject()

const draft = ref('')
const scroller = ref<HTMLElement | null>(null)
const selectedIssueIdx = ref<number[]>([])
const thinkingOpen = ref(true)
type ThinkingLevel = 'off' | 'light' | 'deep' | 'extreme'
const storedThinkingLevel = localStorage.getItem('novaly-crew-thinking-level')
const thinkingLevel = ref<ThinkingLevel>(
  storedThinkingLevel === 'light' || storedThinkingLevel === 'deep' || storedThinkingLevel === 'extreme'
    ? storedThinkingLevel
    : 'off',
)
const thinkingLabels: Record<ThinkingLevel, string> = {
  off: '关闭思考',
  light: '轻度思考',
  deep: '深度思考',
  extreme: '极致思考',
}

const messages = computed(() => crewJob.value?.chat || [])
const qcIssues = computed(() => crewJob.value?.qc?.issues || [])
const qc = computed(() => crewJob.value?.qc || null)
const busy = computed(() => crewChatBusy.value || crewBusy.value)
const fixing = computed(() => {
  if (!busy.value) return false
  return crewJob.value?.status === 'running' && crewJob.value?.stage === 'qc'
})
const busyText = computed(() => {
  if (fixing.value) return '执行导演正在按建议改稿，监制随后复检，请稍候…'
  if (crewChatBusy.value) return '剧组正在对照当前分镜处理…'
  if (busy.value) return '剧组正在处理，请稍候…'
  return ''
})
type ThinkingStep = { label: string; detail: string; state: 'done' | 'active' | 'pending' }

const liveThinkingSteps = computed<ThinkingStep[]>(() => {
  const labels = fixing.value
    ? [
        ['读取修改范围', '只处理已确认的问题，其余分镜保持不动'],
        ['修正分镜', '调整文案、时长与缺失的资产引用'],
        ['规则收敛', '恢复完整台词并执行确定性校正'],
        ['监制复检', '重新检查修改后的分镜'],
      ]
    : crewChatBusy.value
      ? [
          ['理解指令', '识别操作意图和涉及的镜头'],
          ['读取上下文', '核对当前剧本、分镜和资产引用'],
          ['执行处理', '按当前指令更新剧组数据'],
          ['返回结果', '整理执行结果并写入对话'],
        ]
      : [
          ['读取剧本', '分析当前集的场景、人物和对白'],
          ['规划镜头', '安排镜头节奏与场景连续性'],
          ['生成分镜', '拆分镜头并自动绑定已有资产'],
          ['自动校正', '检查台词时长和引用完整性'],
          ['监制质检', '复核结果并给出可处理的问题'],
        ]

  // The server currently exposes the active operation, not token-level model
  // reasoning. Keep the first real stage complete and the current work active.
  return labels.map(([label, detail], index) => ({
    label,
    detail,
    state: index === 0 ? 'done' : index === 1 ? 'active' : 'pending',
  }))
})

function completedSteps(action?: string) {
  if (action === 'split') return ['读取当前剧本与资产', '规划并拆分镜头', '自动绑定参考图', '校正台词与时长']
  if (action === 'fix') return ['读取确认的质检项', '修正对应分镜', '执行规则校正', '提交监制复检']
  if (action === 'qc') return ['读取当前分镜', '检查资产与空间连续性', '检查台词和时长', '生成质检报告']
  return []
}

function showCompletedSteps(msg: { role?: string; name?: string; action?: string }) {
  if (msg.role === 'user') return false
  if (msg.action === 'qc') return msg.name === '监制'
  if (msg.action === 'split' || msg.action === 'fix') return msg.name === '执行导演'
  return false
}
const lastSupervisorId = computed(() => {
  for (let i = messages.value.length - 1; i >= 0; i--) {
    if (messages.value[i].name === '监制') return messages.value[i].id
  }
  return ''
})

const chips = [
  { label: '开始拆镜', text: '开始拆镜' },
  { label: '重新拆镜', text: '替换分镜' },
  { label: '质检本集', text: '质检本集' },
  { label: '按上次建议修改', text: '按上次建议修改' },
]

watch(
  () => qcIssues.value.map(issue => `${issue.shotId || 0}:${issue.code}:${issue.message}`).join('|'),
  () => {
    selectedIssueIdx.value = qcIssues.value
      .map((issue, i) => (issue.severity === 'low' ? -1 : i))
      .filter(i => i >= 0)
  },
  { immediate: true },
)

watch(busy, value => {
  if (value) thinkingOpen.value = true
})

const selectedIssues = computed(() => selectedIssueIdx.value.map(i => qcIssues.value[i]).filter(Boolean))

function agentClass(name?: string) {
  if (name === '监制') return 'supervisor'
  if (name === '执行导演') return 'director'
  if (name === '视频策划') return 'planner'
  return ''
}

function severityType(sev: string) {
  if (sev === 'high') return 'danger'
  if (sev === 'medium') return 'warning'
  return 'info'
}

function toggleIssue(i: number) {
  if (selectedIssueIdx.value.includes(i)) {
    selectedIssueIdx.value = selectedIssueIdx.value.filter(x => x !== i)
  } else {
    selectedIssueIdx.value = [...selectedIssueIdx.value, i]
  }
}

async function send(text: string) {
  const value = text.trim()
  if (!value || busy.value) return
  draft.value = ''
  await sendCrewChat(value, thinkingLevel.value)
}

function selectThinking(level: ThinkingLevel) {
  thinkingLevel.value = level
  localStorage.setItem('novaly-crew-thinking-level', level)
}

async function reconnect() {
  if (busy.value) return
  try {
    await reconnectCrewChat()
    ElMessage.success('已重新连接并同步服务器状态')
  } catch (e: any) {
    ElMessage.error(e?.message || '重新连接失败')
  }
}

async function clearMemory(scope: 'messages' | 'summary' | 'all') {
  if (busy.value) return
  const label = scope === 'messages' ? '消息记忆' : scope === 'summary' ? '摘要记忆（上次质检）' : '消息与摘要记忆'
  try {
    await ElMessageBox.confirm(
      `确定清除${label}？剧本、分镜、资产和成片不会被删除。`,
      '清除记忆',
      { type: scope === 'all' ? 'warning' : 'info', confirmButtonText: '确认清除', cancelButtonText: '取消' },
    )
    await clearCrewMemory(scope)
    ElMessage.success(`已清除${label}`)
  } catch (e: any) {
    if (e !== 'cancel' && e !== 'close') ElMessage.error(e?.message || '清除失败')
  }
}

function onSubmit() {
  void send(draft.value)
}

function applySelectedFixes() {
  if (!selectedIssues.value.length) return
  void fixCrewPipeline(selectedIssues.value)
}

watch(
  () => [messages.value.length, busy.value, busyText.value],
  async () => {
    await nextTick()
    const log = scroller.value
    if (!log) return
    const pending = log.querySelector<HTMLElement>('.crew-chat-pending')
    const lastMsg = log.querySelector<HTMLElement>('.crew-chat-msg:last-of-type')
    ;(pending || lastMsg)?.scrollIntoView({ block: 'nearest' })
  },
)
</script>

<template>
  <aside class="crew-chat" :class="{ embedded }">
    <div class="crew-chat-head">
      <span class="crew-chat-dot" />
      <strong>剧组</strong>
      <span class="crew-chat-sub">拆镜 · 质检</span>
    </div>

    <div ref="scroller" class="crew-chat-log">
      <div v-if="!messages.length" class="crew-chat-welcome">
        <p>执行导演负责拆镜，监制只审核、不直接改稿。对照<strong>当前分镜</strong>说话。</p>
        <div class="crew-chat-chips">
          <button
            v-for="chip in chips"
            :key="chip.text"
            type="button"
            :disabled="busy"
            @click="send(chip.text)"
          >{{ chip.label }}</button>
        </div>
      </div>

      <div
        v-for="msg in messages"
        :key="msg.id"
        class="crew-chat-msg"
        :class="[msg.role, agentClass(msg.name)]"
      >
        <span v-if="msg.role !== 'user'" class="crew-chat-name">{{ msg.name || '剧组' }}</span>
        <details v-if="showCompletedSteps(msg)" class="crew-thinking crew-thinking-complete">
          <summary>
            <span class="crew-thinking-check">✓</span>
            <span>处理步骤 · 已完成</span>
          </summary>
          <ol>
            <li v-for="step in completedSteps(msg.action)" :key="step">
              <span class="crew-thinking-step-icon">✓</span>
              <span>{{ step }}</span>
            </li>
          </ol>
        </details>
        <p class="crew-chat-text">{{ msg.content }}</p>
      </div>

      <div v-if="qc && lastSupervisorId" class="crew-qc crew-chat-qc">
        <p>
          <el-tag :type="qc.score === 'D' || qc.score === 'C' ? 'danger' : 'success'" effect="dark">
            {{ qc.score }}
          </el-tag>
          <span class="crew-qc-summary">{{ qc.summary }}</span>
        </p>
        <div v-if="!qcIssues.length" class="crew-empty">未发现需要处理的问题。</div>
        <template v-else>
          <div class="crew-qc-toolbar">
            <el-button size="small" text @click="selectedIssueIdx = qcIssues.map((_, i) => i)">全选</el-button>
            <el-button size="small" text @click="selectedIssueIdx = []">取消全选</el-button>
            <span class="crew-hint">已选 {{ selectedIssueIdx.length }}/{{ qcIssues.length }}</span>
            <el-button
              size="small"
              type="primary"
              :loading="busy"
              :disabled="!selectedIssues.length"
              @click="applySelectedFixes"
            >
              按建议修改选中项
            </el-button>
          </div>
          <div
            v-for="(issue, i) in qcIssues"
            :key="i"
            class="crew-issue"
            :class="{ selected: selectedIssueIdx.includes(i) }"
          >
            <el-checkbox
              :model-value="selectedIssueIdx.includes(i)"
              @change="toggleIssue(i)"
              @click.stop
            />
            <div class="crew-issue-body">
              <div class="crew-issue-head">
                <el-tag size="small" :type="severityType(issue.severity)" effect="dark">{{ issue.severity }}</el-tag>
                <el-tag size="small" effect="plain">{{ issue.code || 'QC' }}</el-tag>
                <button
                  v-if="issue.shotId || issue.shotIndex"
                  type="button"
                  class="crew-jump"
                  @click.stop.prevent="jumpCrewShot(issue.shotId, issue.shotIndex)"
                >
                  跳转到分镜
                </button>
              </div>
              <b>{{ issue.message }}</b>
              <p v-if="issue.suggestion">{{ issue.suggestion }}</p>
            </div>
          </div>
        </template>
      </div>

      <!-- Running state is the newest event in the conversation. Keep it after
           the previous QC report/issues so progress reads chronologically. -->
      <div v-if="busyText" class="crew-chat-msg assistant director pending crew-chat-pending">
        <span class="crew-chat-name">{{ fixing ? '执行导演' : '剧组' }}</span>
        <div class="crew-thinking crew-thinking-live" :class="{ open: thinkingOpen }">
          <button type="button" class="crew-thinking-summary" @click="thinkingOpen = !thinkingOpen">
            <span class="crew-thinking-spinner" />
            <span class="crew-thinking-title">思考中 · {{ busyText }}</span>
            <span class="crew-thinking-chevron">{{ thinkingOpen ? '⌃' : '⌄' }}</span>
          </button>
          <ol v-if="thinkingOpen">
            <li v-for="step in liveThinkingSteps" :key="step.label" :class="step.state">
              <span class="crew-thinking-step-icon">{{ step.state === 'done' ? '✓' : step.state === 'active' ? '●' : '○' }}</span>
              <span>
                <b>{{ step.label }}</b>
                <small>{{ step.detail }}</small>
              </span>
            </li>
          </ol>
        </div>
      </div>
    </div>

    <div v-if="messages.length" class="crew-chat-chips compact">
      <button
        v-for="chip in chips"
        :key="chip.text"
        type="button"
        :disabled="busy"
        @click="send(chip.text)"
      >{{ chip.label }}</button>
    </div>

    <form class="crew-chat-input" @submit.prevent="onSubmit">
      <textarea
        v-model="draft"
        rows="2"
        :disabled="busy"
        placeholder="例如：开始拆镜 / 质检本集 / 把第2镜台词合回去"
        @keydown.enter.exact.prevent="onSubmit"
      />
      <button type="submit" :disabled="busy || !draft.trim()">发送</button>
      <div class="crew-chat-controls">
        <el-dropdown trigger="click" placement="top-start" :disabled="busy">
          <button type="button" class="crew-control-btn" title="连接与记忆管理">☷</button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="reconnect">↻　重新连接</el-dropdown-item>
              <el-dropdown-item divided @click="clearMemory('messages')">清除消息记忆</el-dropdown-item>
              <el-dropdown-item @click="clearMemory('summary')">清除摘要记忆</el-dropdown-item>
              <el-dropdown-item divided class="crew-danger-item" @click="clearMemory('all')">清除所有记忆</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-dropdown trigger="click" placement="top-start" :disabled="busy">
          <button type="button" class="crew-control-btn crew-thinking-level">◉ {{ thinkingLabels[thinkingLevel] }}</button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="selectThinking('off')">关闭思考</el-dropdown-item>
              <el-dropdown-item @click="selectThinking('light')">轻度思考</el-dropdown-item>
              <el-dropdown-item @click="selectThinking('deep')">深度思考</el-dropdown-item>
              <el-dropdown-item @click="selectThinking('extreme')">极致思考</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </form>
  </aside>
</template>

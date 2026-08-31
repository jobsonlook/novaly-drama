<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useNovalyInject } from '@/composables/useNovalyInject'
import type { AIModel, Provider } from '@/types'
import LocalDoubaoService from '@/components/LocalDoubaoService.vue'
import TrashView from '@/views/TrashView.vue'
import {
  clearDownloadDirectory,
  getDownloadDirName,
  isDownloadDirSupported,
  pickDownloadDirectory,
} from '@/utils/downloadDir'

const {
  settingsTab,
  providers,
  providerKeys,
  shownKeys,
  testing,
  error,
  trashProjects,
  textModels,
  imageModels,
  imageModelsByProvider,
  videoModels,
  saveProvider,
  toggleKey,
  openAddModel,
  openEditModel,
  toggleModel,
  setDefaultModel,
  settingsModelLabel,
  testProvider,
  providersLoading,
} = useNovalyInject()

type Capability = 'text' | 'image' | 'video'

const capabilityTabs: { value: Capability; label: string }[] = [
  { value: 'text', label: '文本' },
  { value: 'image', label: '图像' },
  { value: 'video', label: '视频' },
]

const capabilityTabByProvider = reactive<Record<number, Capability>>({})
const editingConnection = reactive<Record<number, boolean>>({})

const downloadDirSupported = isDownloadDirSupported()
const downloadDirName = ref<string | null>(null)
const downloadDirBusy = ref(false)

const defaultTextId = computed({
  get: () => textModels.value.find(m => m.isDefault)?.id ?? null,
  set: (id: number | null) => { void setDefaultModel('text', id) },
})
const defaultImageId = computed({
  get: () => imageModels.value.find(m => m.isDefault)?.id ?? null,
  set: (id: number | null) => { void setDefaultModel('image', id) },
})
const defaultVideoId = computed({
  get: () => videoModels.value.find(m => m.isDefault)?.id ?? null,
  set: (id: number | null) => { void setDefaultModel('video', id) },
})
const allVideoModels = computed(() =>
  providers.value.flatMap(p => p.models.filter(m => m.capability === 'video')),
)
const enabledVideoIds = computed({
  get: () => allVideoModels.value.filter(m => m.enabled).map(m => m.id),
  set: (ids: number[]) => { void updateEnabledVideoModels(ids) },
})
const savingVideoSelection = ref(false)

async function updateEnabledVideoModels(ids: number[]) {
  if (!ids.length) {
    ElMessage.warning('至少保留一个启用的视频模型')
    return
  }
  savingVideoSelection.value = true
  try {
    const wanted = new Set(ids)
    for (const model of allVideoModels.value) {
      if (!!model.enabled !== wanted.has(model.id)) {
        await toggleModel(model, 'enabled')
      }
    }
    const currentDefault = allVideoModels.value.find(m => m.isDefault)
    if (!currentDefault || !wanted.has(currentDefault.id)) {
      await setDefaultModel('video', ids[0])
    }
    ElMessage.success(`已启用 ${ids.length} 个视频模型，分镜内可单独选择`)
  } finally {
    savingVideoSelection.value = false
  }
}

function providerCapTab(providerId: number): Capability {
  return capabilityTabByProvider[providerId] || 'image'
}

function setProviderCapTab(providerId: number, cap: Capability) {
  capabilityTabByProvider[providerId] = cap
}

function modelsFor(provider: Provider, capability: Capability) {
  return provider.models.filter(m => m.capability === capability)
}

function isConnectionOpen(provider: Provider) {
  return editingConnection[provider.id] || !provider.hasApiKey
}

function openConnection(provider: Provider) {
  editingConnection[provider.id] = true
}

function closeConnection(provider: Provider) {
  editingConnection[provider.id] = false
  shownKeys.value[provider.id] = false
  providerKeys.value[provider.id] = ''
}

async function onSaveProvider(provider: Provider) {
  await saveProvider(provider)
  editingConnection[provider.id] = false
}

function capabilityLabel(capability: string) {
  if (capability === 'text') return '文本'
  if (capability === 'image') return '图像'
  return '视频'
}

function apiKeyPlaceholder(provider: Provider) {
  if (provider.slug === 'doubao-web-api') return '可选，与 doubao-web-api 的 DOUBAO_API_KEY 一致'
  return provider.apiKeyMasked || '填写 API Key'
}

function baseUrlPlaceholder(provider: Provider) {
  if (provider.slug === 'doubao-web-api') return 'http://127.0.0.1:8080/api/v3'
  if (provider.slug === 'deepseek') return 'https://api.deepseek.com/v1'
  return 'https://…'
}

function providerHint(provider: Provider) {
  if (provider.slug === 'doubao-web-api') {
    return '本地 doubao-web-api：需 Chrome（已登录豆包）与 go run ./cmd/server。视频走网页自动化。'
  }
  if (provider.slug === 'pixapi') {
    return 'PixAPI 图像默认 gpt-image-2。国内部署请把 PIXAPI_BASE_URL 指到东京中继。'
  }
  if (provider.slug === 'xais') {
    return 'Xais 图像（GPT Image / Nano Banana），直连 sg2.dchai.cn，工作室内再选分辨率。'
  }
  if (provider.slug === 'deepseek') {
    return 'DeepSeek 官方文本。V4 Pro 用于剧组/分镜，V4 Flash 用于轻量任务。申请 Key：https://platform.deepseek.com/'
  }
  return ''
}

function connectionStatus(provider: Provider) {
  if (provider.slug === 'doubao-web-api') return provider.baseUrl ? 'ready' : 'missing'
  return provider.hasApiKey ? 'ready' : 'missing'
}

async function refreshDownloadDir() {
  downloadDirName.value = await getDownloadDirName()
}

async function chooseDownloadDir() {
  downloadDirBusy.value = true
  try {
    downloadDirName.value = await pickDownloadDirectory()
    ElMessage.success(`已设置下载目录：${downloadDirName.value}`)
  } catch (e: any) {
    if (e?.name !== 'AbortError') {
      ElMessage.error(e?.message || '选择目录失败')
    }
  } finally {
    downloadDirBusy.value = false
  }
}

async function resetDownloadDir() {
  downloadDirBusy.value = true
  try {
    await clearDownloadDirectory()
    downloadDirName.value = null
    ElMessage.success('已恢复为浏览器默认下载目录')
  } catch (e: any) {
    ElMessage.error(e?.message || '清除失败')
  } finally {
    downloadDirBusy.value = false
  }
}

onMounted(() => {
  if (downloadDirSupported) refreshDownloadDir()
})

watch(settingsTab, tab => {
  if (tab === 'download' && downloadDirSupported) refreshDownloadDir()
})

function onSetDefaultClick(model: AIModel) {
  if (model.isDefault) return
  void toggleModel(model, 'isDefault')
}
</script>

<template>
  <section
    class="workspace settings"
    v-loading="providersLoading && settingsTab === 'providers'"
    element-loading-text="加载设置…"
    element-loading-background="rgba(17, 16, 15, 0.55)"
  >
    <LocalDoubaoService />
    <header class="settings-header">
      <div>
        <p class="eyebrow">SETTINGS</p>
        <h1>设置中心</h1>
        <p class="premise">配置默认模型与服务商；管理下载目录与回收站。</p>
      </div>
      <el-radio-group v-model="settingsTab" size="default" class="settings-tabs">
        <el-radio-button value="providers">模型配置</el-radio-button>
        <el-radio-button value="download">下载目录</el-radio-button>
        <el-radio-button value="trash">
          回收站
          <span v-if="trashProjects.length" class="tab-count">{{ trashProjects.length }}</span>
        </el-radio-button>
      </el-radio-group>
    </header>

    <el-alert v-if="error" :title="error" type="error" show-icon :closable="false" class="error-alert" />

    <template v-if="settingsTab === 'providers'">
      <section class="defaults-section">
        <div class="section-head">
          <div>
            <h2>默认模型配置</h2>
            <p>作用于新项目与未单独指定模型的流程；工作室内仍可临时覆盖。</p>
          </div>
        </div>
        <div class="defaults-grid">
          <div class="default-card">
            <div class="default-card-label">
              <span class="default-icon">文</span>
              <div>
                <strong>文本分析模型</strong>
                <small>分镜文案 / 站位提示词分析</small>
              </div>
            </div>
            <el-select
              v-model="defaultTextId"
              placeholder="未设置"
              class="default-select"
            >
              <el-option
                v-for="m in textModels"
                :key="m.id"
                :value="m.id"
                :label="settingsModelLabel(m)"
              />
            </el-select>
          </div>
          <div class="default-card">
            <div class="default-card-label">
              <span class="default-icon">图</span>
              <div>
                <strong>图像生成模型</strong>
                <small>角色 / 场景 / 道具 / 站位图</small>
              </div>
            </div>
            <el-select
              v-model="defaultImageId"
              placeholder="未设置"
              filterable
              class="default-select"
            >
              <el-option-group
                v-for="group in imageModelsByProvider"
                :key="group.providerId"
                :label="group.providerName"
              >
                <el-option
                  v-for="m in group.models"
                  :key="m.id"
                  :value="m.id"
                  :label="settingsModelLabel(m)"
                />
              </el-option-group>
            </el-select>
          </div>
          <div class="default-card">
            <div class="default-card-label">
              <span class="default-icon">视</span>
              <div>
                <strong>视频生成模型</strong>
                <small>分镜成片默认通道</small>
              </div>
            </div>
            <el-select
              v-model="defaultVideoId"
              placeholder="未设置"
              class="default-select"
            >
              <el-option
                v-for="m in videoModels"
                :key="m.id"
                :value="m.id"
                :label="settingsModelLabel(m)"
              />
            </el-select>
            <div class="video-enabled-picker">
              <span>同时启用的视频模型</span>
              <el-select
                v-model="enabledVideoIds"
                multiple
                collapse-tags
                collapse-tags-tooltip
                :max-collapse-tags="2"
                :loading="savingVideoSelection"
                placeholder="选择可用视频模型"
                class="default-select"
              >
                <el-option
                  v-for="m in allVideoModels"
                  :key="m.id"
                  :value="m.id"
                  :label="settingsModelLabel(m)"
                />
              </el-select>
              <small>可多选；每个分镜生成时仍只选择其中一个模型。</small>
            </div>
          </div>
        </div>
      </section>

      <section class="pool-section">
        <div class="section-head">
          <div>
            <h2>厂商资源池</h2>
            <p>管理各服务商的 API Key 与可用模型；按能力切换列表。</p>
          </div>
        </div>

        <div class="provider-grid">
          <article
            v-for="provider in providers"
            :key="provider.id"
            class="provider-card"
          >
            <header class="provider-card-head">
              <div class="provider-identity">
                <span
                  class="conn-dot"
                  :class="connectionStatus(provider)"
                  :title="connectionStatus(provider) === 'ready' ? '已配置' : '未连接'"
                />
                <div>
                  <h3>{{ provider.name }}</h3>
                  <code>{{ provider.slug }}</code>
                </div>
              </div>
              <el-button size="small" :loading="testing === provider.id" @click="testProvider(provider)">
                {{ testing === provider.id ? '测试中…' : '测试连接' }}
              </el-button>
            </header>

            <div class="provider-connect">
              <div class="connect-label">API Key</div>
              <div v-if="!isConnectionOpen(provider)" class="connect-summary">
                <span class="masked-key">{{ provider.apiKeyMasked || '••••••••' }}</span>
                <el-button size="small" text type="primary" @click="openConnection(provider)">编辑</el-button>
              </div>
              <div v-else class="connect-form">
                <el-input
                  v-model="providerKeys[provider.id]"
                  :type="shownKeys[provider.id] ? 'text' : 'password'"
                  :placeholder="apiKeyPlaceholder(provider)"
                  size="small"
                >
                  <template #append>
                    <el-button @click="toggleKey(provider)">
                      {{ shownKeys[provider.id] ? '隐藏' : '显示' }}
                    </el-button>
                  </template>
                </el-input>
                <el-input
                  v-model="provider.baseUrl"
                  size="small"
                  :placeholder="baseUrlPlaceholder(provider)"
                >
                  <template #prefix>
                    <span class="url-prefix">地址</span>
                  </template>
                </el-input>
                <div class="connect-actions">
                  <el-button type="primary" size="small" @click="onSaveProvider(provider)">
                    {{ provider.hasApiKey ? '保存配置' : '连接' }}
                  </el-button>
                  <el-button
                    v-if="provider.hasApiKey"
                    size="small"
                    text
                    @click="closeConnection(provider)"
                  >
                    取消
                  </el-button>
                </div>
              </div>
            </div>

            <p v-if="providerHint(provider)" class="provider-hint">{{ providerHint(provider) }}</p>

            <el-radio-group
              :model-value="providerCapTab(provider.id)"
              size="small"
              class="cap-tabs"
              @update:model-value="(v: string) => setProviderCapTab(provider.id, v as Capability)"
            >
              <el-radio-button
                v-for="tab in capabilityTabs"
                :key="tab.value"
                :value="tab.value"
              >
                {{ tab.label }}
                <span class="cap-count">{{ modelsFor(provider, tab.value).length }}</span>
              </el-radio-button>
            </el-radio-group>

            <div class="model-list-head">
              <strong>{{ capabilityLabel(providerCapTab(provider.id)) }}</strong>
              <el-button
                text
                type="primary"
                size="small"
                @click="openAddModel(provider, providerCapTab(provider.id))"
              >
                + 添加
              </el-button>
            </div>

            <div class="model-list">
              <div
                v-for="model in modelsFor(provider, providerCapTab(provider.id))"
                :key="model.id"
                class="model-row"
                :class="{ disabled: !model.enabled }"
              >
                <div class="model-meta">
                  <div class="model-name-line">
                    <b>{{ model.name }}</b>
                    <span v-if="model.isDefault" class="default-badge">默认</span>
                  </div>
                  <small>{{ model.modelId }}</small>
                </div>
                <div class="model-actions">
                  <el-button text size="small" @click="openEditModel(provider, model)">编辑</el-button>
                  <el-button
                    text
                    size="small"
                    class="default-btn"
                    :class="{ on: model.isDefault }"
                    @click="onSetDefaultClick(model)"
                  >
                    {{ model.isDefault ? '默认' : '设为默认' }}
                  </el-button>
                  <el-switch
                    :model-value="model.enabled"
                    size="small"
                    @change="toggleModel(model, 'enabled')"
                  />
                </div>
              </div>
              <el-empty
                v-if="!modelsFor(provider, providerCapTab(provider.id)).length"
                description="暂无模型"
                :image-size="48"
              />
            </div>
          </article>
        </div>
      </section>
    </template>

    <template v-else-if="settingsTab === 'download'">
      <el-card class="download-card" shadow="never">
        <h2>下载目录</h2>
        <p class="download-desc">
          视频导出与 TTS 语音下载会保存到此处。未配置时，少量视频可打成 zip；数量较多时优先逐个写入本目录。若当前为 HTTP 访问（无目录选择能力），会改为由服务器打包 zip，经浏览器下载栏保存。
        </p>
        <el-alert
          v-if="!downloadDirSupported"
          type="warning"
          :closable="false"
          show-icon
          title="当前浏览器不支持选择文件夹，请使用 Chrome 或 Edge。下载仍会保存到系统默认目录。"
          class="download-alert"
        />
        <template v-else>
          <div class="download-current">
            <span class="download-label">当前目录</span>
            <strong>{{ downloadDirName || '浏览器默认下载目录' }}</strong>
          </div>
          <div class="download-actions">
            <el-button type="primary" :loading="downloadDirBusy" @click="chooseDownloadDir">
              {{ downloadDirName ? '更换目录' : '选择目录' }}
            </el-button>
            <el-button :disabled="!downloadDirName || downloadDirBusy" @click="resetDownloadDir">
              恢复默认
            </el-button>
          </div>
          <p class="download-hint">
            出于浏览器安全限制，只能显示文件夹名称，不能填写绝对路径。首次下载时若提示权限，请允许访问该文件夹。
          </p>
        </template>
      </el-card>
    </template>

    <TrashView v-else embedded />
  </section>
</template>

<style scoped>
.settings-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.settings-tabs :deep(.el-radio-button__inner) {
  border-color: #3c3731;
  background: #1a1816;
  color: #b9afa5;
}

.settings-tabs :deep(.el-radio-button__original-radio:checked + .el-radio-button__inner) {
  background: rgba(255, 120, 90, 0.15);
  border-color: #ff785a;
  color: #ff9d85;
  box-shadow: none;
}

.tab-count {
  display: inline-grid;
  place-items: center;
  min-width: 18px;
  height: 18px;
  margin-left: 6px;
  padding: 0 5px;
  border-radius: 999px;
  background: #ff785a;
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  vertical-align: middle;
}

.error-alert {
  margin-bottom: 16px;
}

.section-head {
  margin: 28px 0 14px;
}

.section-head h2 {
  margin: 0 0 6px;
  font-size: 18px;
  font-weight: 650;
  color: #f2ebe3;
}

.section-head p {
  margin: 0;
  font-size: 13px;
  line-height: 1.5;
  color: #8f857c;
}

.defaults-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

@media (max-width: 960px) {
  .defaults-grid {
    grid-template-columns: 1fr;
  }
}

.default-card {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 16px;
  border: 1px solid #37312d;
  border-radius: 14px;
  background: #211e1b;
}

.default-card-label {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.default-icon {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: rgba(255, 120, 90, 0.12);
  color: #ff9d85;
  font-size: 13px;
  font-weight: 700;
  flex-shrink: 0;
}

.default-card-label strong {
  display: block;
  font-size: 14px;
  font-weight: 650;
  color: #f2ebe3;
}

.default-card-label small {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: #8f857c;
  line-height: 1.4;
}

.default-select {
  width: 100%;
}

.video-enabled-picker {
  display: grid;
  gap: 7px;
  padding-top: 12px;
  border-top: 1px solid #37312d;
}

.video-enabled-picker > span {
  color: #c8bdb3;
  font-size: 12px;
  font-weight: 600;
}

.video-enabled-picker > small {
  color: #8f857c;
  font-size: 11px;
  line-height: 1.45;
}

.provider-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

@media (max-width: 900px) {
  .provider-grid {
    grid-template-columns: 1fr;
  }
}

.provider-card {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 18px;
  border: 1px solid #37312d;
  border-radius: 14px;
  background: #211e1b;
  min-height: 320px;
}

.provider-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.provider-identity {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 0;
}

.conn-dot {
  width: 10px;
  height: 10px;
  margin-top: 7px;
  border-radius: 50%;
  flex-shrink: 0;
  background: #56514b;
}

.conn-dot.ready {
  background: #59dc89;
  box-shadow: 0 0 0 3px rgba(89, 220, 137, 0.18);
}

.conn-dot.missing {
  background: #ff785a;
  box-shadow: 0 0 0 3px rgba(255, 120, 90, 0.15);
}

.provider-identity h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 650;
  color: #f2ebe3;
}

.provider-identity code {
  display: block;
  margin-top: 3px;
  font: 11px/1.4 'DM Mono', ui-monospace, monospace;
  color: #8f857c;
}

.provider-connect {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  border-radius: 10px;
  background: #181614;
  border: 1px solid #2e2a26;
}

.connect-label {
  font-size: 12px;
  font-weight: 600;
  color: #9a9288;
}

.connect-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.masked-key {
  font: 13px/1.4 'DM Mono', ui-monospace, monospace;
  color: #cfc5ba;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.connect-form {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.url-prefix {
  color: #8f857c;
  font-size: 12px;
  padding-right: 4px;
}

.connect-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}

.provider-hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.55;
  color: #9a9288;
}

.cap-tabs {
  display: flex;
  width: 100%;
}

.cap-tabs :deep(.el-radio-button) {
  flex: 1;
}

.cap-tabs :deep(.el-radio-button__inner) {
  width: 100%;
  border-color: #3c3731;
  background: #181614;
  color: #b9afa5;
}

.cap-tabs :deep(.el-radio-button__original-radio:checked + .el-radio-button__inner) {
  background: #2a2622;
  border-color: #514a43;
  color: #f2ebe3;
  box-shadow: none;
}

.cap-count {
  display: inline-grid;
  place-items: center;
  min-width: 16px;
  height: 16px;
  margin-left: 4px;
  padding: 0 4px;
  border-radius: 999px;
  background: #3b3732;
  color: #bbb1a6;
  font-size: 10px;
  font-weight: 700;
}

.model-list-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.model-list-head strong {
  font-size: 13px;
  color: #eee9e1;
}

.model-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 280px;
  overflow: auto;
  margin: 0 -4px;
  padding: 0 4px;
}

.model-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 6px;
  border-radius: 8px;
}

.model-row:hover {
  background: rgba(255, 255, 255, 0.03);
}

.model-row.disabled .model-meta {
  opacity: 0.55;
}

.model-meta {
  min-width: 0;
}

.model-name-line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.model-meta b {
  font-size: 13px;
  font-weight: 650;
  color: #f2ebe3;
}

.model-meta small {
  display: block;
  margin-top: 3px;
  font: 11px/1.4 'DM Mono', ui-monospace, monospace;
  color: #938a80;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.default-badge {
  display: inline-flex;
  align-items: center;
  height: 18px;
  padding: 0 7px;
  border-radius: 999px;
  background: rgba(255, 120, 90, 0.18);
  color: #ff9d85;
  font-size: 11px;
  font-weight: 700;
}

.model-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}

.default-btn.on {
  color: #ff9d85;
}

.download-card {
  max-width: 640px;
}

.download-card h2 {
  margin: 0 0 8px;
  font-size: 18px;
  font-weight: 650;
  color: #f2ebe3;
}

.download-desc {
  margin: 0 0 20px;
  color: #b9afa5;
  line-height: 1.55;
}

.download-alert {
  margin-bottom: 8px;
}

.download-current {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 16px;
  padding: 14px 16px;
  border: 1px solid #3c3731;
  border-radius: 10px;
  background: #141210;
}

.download-label {
  font-size: 12px;
  color: #8f857c;
}

.download-current strong {
  font-size: 15px;
  font-weight: 600;
  color: #f2ebe3;
  word-break: break-all;
}

.download-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 14px;
}

.download-hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: #8f857c;
}
</style>

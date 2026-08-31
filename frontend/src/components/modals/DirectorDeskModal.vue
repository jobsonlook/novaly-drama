<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'

const {
  directorDeskModal,
  closeDirectorDesk,
  onDirectorDeskReady,
  onDirectorDeskCaptures,
} = useNovalyInject()

const iframeRef = ref<HTMLIFrameElement | null>(null)
const ready = ref(false)
const status = ref('正在加载 3D 导演台…')

const src = computed(() => {
  const modal = directorDeskModal.value
  if (!modal) return ''
  const q = new URLSearchParams({
    instanceId: modal.instanceId,
    theme: 'dark',
  })
  return `/director-desk/?${q.toString()}`
})

function postToDesk(type: string, payload?: Record<string, unknown>) {
  const win = iframeRef.value?.contentWindow
  if (!win) return
  win.postMessage({ type, payload }, window.location.origin)
}

function onMessage(event: MessageEvent) {
  if (event.origin !== window.location.origin) return
  const type = event.data?.type
  if (!type || typeof type !== 'string') return
  if (!type.startsWith('storyai:director-desk-')) return

  if (type === 'storyai:director-desk-ready') {
    ready.value = true
    status.value = '导演台已就绪，正在导入全景…'
    onDirectorDeskReady(postToDesk)
    return
  }
  if (type === 'storyai:director-desk-close') {
    closeDirectorDesk()
    return
  }
  if (type === 'storyai:director-desk-captures-sent') {
    const captures = Array.isArray(event.data?.payload?.captures)
      ? event.data.payload.captures as { dataUrl?: string; fileName?: string }[]
      : []
    onDirectorDeskCaptures(captures)
    status.value = captures.length ? `已接收 ${captures.length} 张截图` : '未收到截图'
  }
}

onMounted(() => {
  window.addEventListener('message', onMessage)
})

onBeforeUnmount(() => {
  window.removeEventListener('message', onMessage)
})

watch(directorDeskModal, (modal) => {
  ready.value = false
  status.value = modal ? '正在加载 3D 导演台…' : ''
})
</script>

<template>
  <el-dialog
    :model-value="!!directorDeskModal"
    title="3D 导演台 · 全景摆位"
    width="96%"
    top="2vh"
    class="director-desk-dialog"
    align-center
    destroy-on-close
    :close-on-click-modal="false"
    @close="closeDirectorDesk"
  >
    <p class="desk-hint">
      全景已作为 360° 背景导入。在导演台里摆角色与机位，切到机位视角后截图，再点「发送到画布」回填到站位骨架。
    </p>
    <p class="desk-status">{{ status }}</p>
    <iframe
      v-if="directorDeskModal"
      ref="iframeRef"
      class="director-desk-frame"
      :src="src"
      title="3D 导演台"
      allow="fullscreen"
    />
    <template #footer>
      <el-button @click="closeDirectorDesk">关闭</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.desk-hint {
  margin: 0 0 6px;
  font-size: 13px;
  line-height: 1.5;
  color: #b9afa5;
}
.desk-status {
  margin: 0 0 8px;
  font-size: 12px;
  color: #8a8078;
}
.director-desk-frame {
  display: block;
  width: 100%;
  height: min(78vh, 820px);
  border: 0;
  border-radius: 8px;
  background: #0b0d10;
}
</style>

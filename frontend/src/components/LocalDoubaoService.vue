<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { api } from '@/api/client'
const status = ref<any>({ state: 'loading' })
const busy = ref(false)
const error = ref('')
let timer: ReturnType<typeof setInterval> | undefined
const labels: Record<string, string> = { loading: '读取中', stopped: '未启动', starting: '启动中', running: '运行中', stopping: '停止中' }
async function refresh() { try { status.value = await api('/local/doubao') } catch (e) { error.value = String(e) } }
async function action(name: string) {
 busy.value = true; error.value = ''
 try { status.value = await api('/local/doubao/' + name, { method: 'POST', headers: { 'X-Novaly-Local': '1' } }) }
 catch (e) { error.value = String(e) } finally { busy.value = false }
}
onMounted(() => { void refresh(); timer = setInterval(refresh, 3000) })
onUnmounted(() => clearInterval(timer))
</script>
<template>
 <el-card style="margin-bottom: 20px">
  <h2>本地豆包服务 · {{ labels[status.state] || status.state }}</h2>
  <p>数据库、图片和视频保存在本机，不使用 COS。首次启动后，请在专用 Chrome 窗口登录豆包。</p>
  <el-space wrap>
   <el-button type="primary" :loading="busy" :disabled="status.managed || status.ready" @click="action('start')">启动 doubao-web-api</el-button>
   <el-button :disabled="busy || !status.managed" @click="action('stop')">停止服务</el-button>
   <el-button :disabled="!status.ready" tag="a" href="http://127.0.0.1:8086/admin" target="_blank" rel="noopener">豆包账号管理</el-button>
   <el-button @click="refresh">刷新状态</el-button>
  </el-space>
  <p v-if="status.ready && !status.managed">端口上已有服务运行，Novaly 不会停止外部进程。</p>
  <p v-if="status.logPath">日志：{{ status.logPath }}</p>
  <el-alert v-if="error || status.error" :title="error || status.error" type="error" :closable="false" />
 </el-card>
</template>

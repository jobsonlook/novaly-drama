<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{
  src: string
  title?: string
}>()

const root = ref<HTMLElement>()
const videoMain = ref<HTMLVideoElement>()
const videoBg = ref<HTMLVideoElement>()
const progressBar = ref<HTMLElement>()

const playing = ref(false)
const current = ref(0)
const total = ref(0)
const buffered = ref(0)
const volume = ref(0.8)
const muted = ref(false)
const speedIdx = ref(2)
const controlsVisible = ref(false)
const hovering = ref(false)
const seeking = ref(false)

const speeds = [0.5, 0.75, 1, 1.25, 1.5, 2]
const speed = computed(() => speeds[speedIdx.value])

let hideTimer: ReturnType<typeof setTimeout> | null = null

const playedPct = computed(() => (total.value ? (current.value / total.value) * 100 : 0))
const bufferPct = computed(() => (total.value ? (buffered.value / total.value) * 100 : 0))
const timeText = computed(() => `${formatTime(current.value)} / ${formatTime(total.value)}`)

function formatTime(sec: number) {
  if (!Number.isFinite(sec) || sec < 0) return '0:00'
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}

function syncBg() {
  const main = videoMain.value
  const bg = videoBg.value
  if (!main || !bg) return
  if (Math.abs(bg.currentTime - main.currentTime) > 0.3) {
    bg.currentTime = main.currentTime
  }
}

function showControls(autoHide = true) {
  controlsVisible.value = true
  if (hideTimer) clearTimeout(hideTimer)
  if (autoHide && playing.value) {
    hideTimer = setTimeout(() => {
      if (playing.value) controlsVisible.value = false
    }, 2800)
  }
}

function onMouseEnter() {
  hovering.value = true
  showControls(false)
}

function onMouseLeave() {
  hovering.value = false
  if (seeking.value) return
  controlsVisible.value = false
  if (hideTimer) clearTimeout(hideTimer)
}

function onMouseMove() {
  if (!hovering.value) return
  showControls(true)
}

async function togglePlay() {
  const main = videoMain.value
  const bg = videoBg.value
  if (!main) return
  if (main.paused) {
    await main.play()
    void bg?.play()
  } else {
    main.pause()
    bg?.pause()
  }
}

function onPlay() {
  playing.value = true
  if (hovering.value) showControls(true)
}

function onPause() {
  playing.value = false
  if (hideTimer) clearTimeout(hideTimer)
}

function onTimeUpdate() {
  const el = videoMain.value
  if (!el || seeking.value) return
  current.value = el.currentTime
  total.value = el.duration || total.value
  syncBg()
}

function onLoadedMetadata() {
  const el = videoMain.value
  if (!el) return
  total.value = el.duration || 0
  el.volume = volume.value
  el.playbackRate = speed.value
  if (videoBg.value) {
    videoBg.value.muted = true
    videoBg.value.volume = 0
  }
}

function onProgress() {
  const el = videoMain.value
  if (!el || !el.buffered.length) return
  buffered.value = el.buffered.end(el.buffered.length - 1)
}

function seekTo(clientX: number) {
  const el = videoMain.value
  const bar = progressBar.value
  if (!el || !bar || !total.value) return
  const rect = bar.getBoundingClientRect()
  const pct = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
  el.currentTime = pct * total.value
  current.value = el.currentTime
  syncBg()
}

function onProgressClick(e: MouseEvent) {
  seekTo(e.clientX)
}

function onThumbDown(e: MouseEvent) {
  e.preventDefault()
  seeking.value = true
  showControls(false)
  const move = (ev: MouseEvent) => seekTo(ev.clientX)
  const up = () => {
    seeking.value = false
    document.removeEventListener('mousemove', move)
    document.removeEventListener('mouseup', up)
    hideControlsSoon()
  }
  document.addEventListener('mousemove', move)
  document.addEventListener('mouseup', up)
}

function hideControlsSoon() {
  if (hovering.value) showControls(playing.value)
  else controlsVisible.value = false
}

function cycleSpeed() {
  speedIdx.value = (speedIdx.value + 1) % speeds.length
  if (videoMain.value) videoMain.value.playbackRate = speed.value
}

function toggleMute() {
  muted.value = !muted.value
  if (videoMain.value) videoMain.value.muted = muted.value
}

function onVolumeInput(e: Event) {
  const v = Number((e.target as HTMLInputElement).value)
  volume.value = v
  muted.value = v === 0
  if (videoMain.value) {
    videoMain.value.volume = v
    videoMain.value.muted = v === 0
  }
}

async function togglePiP() {
  const el = videoMain.value
  if (!el) return
  try {
    if (document.pictureInPictureElement) {
      await document.exitPictureInPicture()
    } else if (document.pictureInPictureEnabled) {
      await el.requestPictureInPicture()
    }
  } catch { /* ignore */ }
}

function toggleFullscreen() {
  const el = root.value
  if (!el) return
  if (document.fullscreenElement) {
    void document.exitFullscreen()
  } else {
    void el.requestFullscreen()
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.code === 'Space') {
    e.preventDefault()
    void togglePlay()
  }
}

watch(() => props.src, () => {
  current.value = 0
  total.value = 0
  buffered.value = 0
  playing.value = false
})

onMounted(() => {
  if (videoMain.value) videoMain.value.volume = volume.value
})

onBeforeUnmount(() => {
  if (hideTimer) clearTimeout(hideTimer)
})
</script>

<template>
  <div
    ref="root"
    class="bili-player"
    tabindex="0"
    @keydown="onKeydown"
    @mouseenter="onMouseEnter"
    @mouseleave="onMouseLeave"
    @mousemove="onMouseMove"
  >
    <div class="bili-stage">
      <video
        ref="videoBg"
        class="bili-video-bg"
        :src="src"
        muted
        playsinline
        preload="metadata"
        tabindex="-1"
      />
      <video
        ref="videoMain"
        class="bili-video"
        :src="src"
        playsinline
        preload="metadata"
        tabindex="-1"
        @play="onPlay"
        @pause="onPause"
        @timeupdate="onTimeUpdate"
        @loadedmetadata="onLoadedMetadata"
        @progress="onProgress"
        @click.stop="togglePlay"
      />

      <button
        v-show="controlsVisible && !playing"
        type="button"
        class="bili-corner-play"
        aria-label="播放"
        @click.stop="togglePlay"
      >
        <svg viewBox="0 0 24 24" width="28" height="28"><path fill="currentColor" d="M8 5v14l11-7z" /></svg>
      </button>

      <div class="bili-controls" :class="{ show: controlsVisible || seeking }" @click.stop>
        <div ref="progressBar" class="bili-progress" @click="onProgressClick">
          <div class="bili-progress-buffer" :style="{ width: `${bufferPct}%` }" />
          <div class="bili-progress-played" :style="{ width: `${playedPct}%` }" />
          <div
            class="bili-progress-thumb"
            :style="{ left: `${playedPct}%` }"
            @mousedown="onThumbDown"
          />
        </div>

        <div class="bili-toolbar">
          <div class="bili-left">
            <button type="button" class="bili-btn" :aria-label="playing ? '暂停' : '播放'" @click="togglePlay">
              <svg v-if="playing" viewBox="0 0 24 24" width="22" height="22"><path fill="currentColor" d="M6 5h4v14H6V5zm8 0h4v14h-4V5z" /></svg>
              <svg v-else viewBox="0 0 24 24" width="22" height="22"><path fill="currentColor" d="M8 5v14l11-7z" /></svg>
            </button>
            <span class="bili-time">{{ timeText }}</span>
            <span v-if="title" class="bili-title">{{ title }}</span>
          </div>

          <div class="bili-right">
            <button type="button" class="bili-text-btn" @click="cycleSpeed">
              {{ speed === 1 ? '倍速' : `${speed}x` }}
            </button>
            <div class="bili-volume">
              <button type="button" class="bili-btn" aria-label="音量" @click="toggleMute">
                <svg v-if="muted || volume === 0" viewBox="0 0 24 24" width="20" height="20"><path fill="currentColor" d="M16.5 12c0-1.77-1.02-3.29-2.5-4.03v2.21l2.45 2.45c.03-.2.05-.41.05-.63zm2.5 0c0 .94-.2 1.82-.54 2.64l1.51 1.51C20.63 14.91 21 13.5 21 12c0-4.28-2.99-7.86-7-8.77v2.06c2.89.86 5 3.54 5 6.71zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06c1.38-.31 2.63-.95 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z" /></svg>
                <svg v-else viewBox="0 0 24 24" width="20" height="20"><path fill="currentColor" d="M3 10v4h4l5 5V5L7 10H3zm13.5 2c0-1.77-1.02-3.29-2.5-4.03v8.06c1.48-.73 2.5-2.25 2.5-4.03z" /></svg>
              </button>
              <input
                class="bili-volume-slider"
                type="range"
                min="0"
                max="1"
                step="0.02"
                :value="muted ? 0 : volume"
                @input="onVolumeInput"
              />
            </div>
            <button type="button" class="bili-btn" aria-label="画中画" @click="togglePiP">
              <svg viewBox="0 0 24 24" width="20" height="20"><path fill="currentColor" d="M19 7h-8v8h8V7zm2-4H3c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h18c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm0 16H3V5h18v14z" /></svg>
            </button>
            <button type="button" class="bili-btn" aria-label="全屏" @click="toggleFullscreen">
              <svg viewBox="0 0 24 24" width="20" height="20"><path fill="currentColor" d="M7 14H5v5h5v-2H7v-3zm-2-4h2V7h3V5H5v5zm12 7h-3v2h5v-5h-2v3zM14 5v2h3v3h2V5h-5z" /></svg>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.bili-player {
  background: #000;
  outline: none;
}

.bili-player:focus-visible {
  box-shadow: inset 0 0 0 2px rgba(211, 204, 194, 0.45);
}

.bili-stage {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 9;
  max-height: min(520px, 72vh);
  background: #111;
  overflow: hidden;
  cursor: pointer;
}

.bili-player:fullscreen .bili-stage {
  max-height: none;
  height: 100%;
  aspect-ratio: auto;
}

.bili-video-bg {
  position: absolute;
  inset: -20px;
  width: calc(100% + 40px);
  height: calc(100% + 40px);
  object-fit: cover;
  filter: blur(28px) brightness(0.45) saturate(1.1);
  transform: scale(1.08);
  pointer-events: none;
}

.bili-video {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
  background: transparent;
}

.bili-corner-play {
  position: absolute;
  z-index: 3;
  right: 14px;
  bottom: 52px;
  width: 44px;
  height: 44px;
  border: none;
  border-radius: 10px;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(6px);
  color: #fff;
  display: grid;
  place-items: center;
  cursor: pointer;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
  transition: background 0.15s, transform 0.15s;
}

.bili-corner-play:hover {
  background: rgba(0, 0, 0, 0.72);
  transform: scale(1.05);
}

.bili-controls {
  position: absolute;
  z-index: 4;
  left: 0;
  right: 0;
  bottom: 0;
  padding: 28px 12px 10px;
  background: linear-gradient(180deg, transparent, rgba(0, 0, 0, 0.75));
  opacity: 0;
  transform: translateY(6px);
  transition: opacity 0.22s, transform 0.22s;
  pointer-events: none;
}

.bili-controls.show {
  opacity: 1;
  transform: translateY(0);
  pointer-events: auto;
}

.bili-progress {
  position: relative;
  height: 5px;
  margin: 0 4px 10px;
  background: rgba(255, 255, 255, 0.18);
  border-radius: 999px;
  cursor: pointer;
}

.bili-progress:hover {
  height: 6px;
}

.bili-progress-buffer,
.bili-progress-played {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  border-radius: inherit;
  pointer-events: none;
}

.bili-progress-buffer {
  background: rgba(255, 255, 255, 0.28);
}

.bili-progress-played {
  background: #d3ccc2;
}

.bili-progress-thumb {
  position: absolute;
  top: 50%;
  width: 14px;
  height: 14px;
  margin-left: -7px;
  margin-top: -7px;
  border-radius: 50%;
  background: #eee9e1;
  box-shadow: 0 0 0 2px rgba(17, 16, 15, 0.65);
  opacity: 0;
  transition: opacity 0.15s;
  cursor: grab;
}

.bili-progress:hover .bili-progress-thumb,
.bili-controls.show .bili-progress-thumb {
  opacity: 1;
}

.bili-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: #fff;
}

.bili-left,
.bili-right {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.bili-left {
  flex: 1;
}

.bili-btn {
  border: none;
  background: transparent;
  color: #fff;
  padding: 4px;
  border-radius: 4px;
  cursor: pointer;
  display: grid;
  place-items: center;
  opacity: 0.95;
}

.bili-btn:hover {
  background: rgba(255, 255, 255, 0.12);
}

.bili-text-btn {
  border: none;
  background: transparent;
  color: #fff;
  font-size: 13px;
  padding: 4px 6px;
  border-radius: 4px;
  cursor: pointer;
  white-space: nowrap;
}

.bili-text-btn:hover {
  background: rgba(255, 255, 255, 0.12);
}

.bili-time {
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  opacity: 0.95;
}

.bili-title {
  font-size: 12px;
  opacity: 0.75;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.bili-volume {
  display: flex;
  align-items: center;
  gap: 4px;
}

.bili-volume-slider {
  width: 0;
  opacity: 0;
  transition: width 0.18s, opacity 0.18s;
  accent-color: #d3ccc2;
  cursor: pointer;
}

.bili-volume:hover .bili-volume-slider {
  width: 72px;
  opacity: 1;
}
</style>

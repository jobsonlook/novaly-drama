<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  src: string
  /** Safe-seam panoramas put front at ~25% width → start yaw -90°. */
  initialYawDeg?: number
  initialPitchDeg?: number
  initialFovDeg?: number
}>(), {
  initialYawDeg: -90,
  initialPitchDeg: 0,
  initialFovDeg: 75,
})

const canvasRef = ref<HTMLCanvasElement | null>(null)
const hint = ref('拖拽旋转 · 滚轮缩放')
const ready = ref(false)
const error = ref('')

let gl: WebGLRenderingContext | null = null
let program: WebGLProgram | null = null
let tex: WebGLTexture | null = null
let raf = 0
let yaw = (props.initialYawDeg * Math.PI) / 180
let pitch = (props.initialPitchDeg * Math.PI) / 180
let fov = (props.initialFovDeg * Math.PI) / 180
let dragging = false
let lastX = 0
let lastY = 0
let uYaw: WebGLUniformLocation | null = null
let uPitch: WebGLUniformLocation | null = null
let uFov: WebGLUniformLocation | null = null
let uAspect: WebGLUniformLocation | null = null

const VERT = `
attribute vec2 aPos;
varying vec2 vUv;
void main() {
  vUv = aPos * 0.5 + 0.5;
  gl_Position = vec4(aPos, 0.0, 1.0);
}
`

const FRAG = `
precision mediump float;
uniform sampler2D uTex;
uniform float uYaw;
uniform float uPitch;
uniform float uFov;
uniform float uAspect;
varying vec2 vUv;

void main() {
  vec2 ndc = vUv * 2.0 - 1.0;
  float fovY = uFov;
  float fovX = 2.0 * atan(tan(fovY * 0.5) * uAspect);
  vec3 dir = normalize(vec3(
    ndc.x * tan(fovX * 0.5),
    ndc.y * tan(fovY * 0.5),
    1.0
  ));
  float cp = cos(uPitch);
  float sp = sin(uPitch);
  float cy = cos(uYaw);
  float sy = sin(uYaw);
  vec3 pitched = vec3(dir.x, dir.y * cp - dir.z * sp, dir.y * sp + dir.z * cp);
  vec3 world = vec3(
    pitched.x * cy + pitched.z * sy,
    pitched.y,
    -pitched.x * sy + pitched.z * cy
  );
  float lon = atan(world.x, world.z);
  float lat = asin(clamp(world.y, -1.0, 1.0));
  vec2 uv = vec2(lon / (2.0 * 3.14159265) + 0.5, 0.5 - lat / 3.14159265);
  gl_FragColor = texture2D(uTex, uv);
}
`

function compile(type: number, src: string) {
  if (!gl) return null
  const s = gl.createShader(type)
  if (!s) return null
  gl.shaderSource(s, src)
  gl.compileShader(s)
  if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
    const msg = gl.getShaderInfoLog(s) || 'shader compile failed'
    gl.deleteShader(s)
    throw new Error(msg)
  }
  return s
}

function initGL() {
  const canvas = canvasRef.value
  if (!canvas) return
  gl = canvas.getContext('webgl', { antialias: true, alpha: false })
  if (!gl) {
    error.value = '当前浏览器不支持 WebGL，无法环视全景'
    return
  }
  const vs = compile(gl.VERTEX_SHADER, VERT)
  const fs = compile(gl.FRAGMENT_SHADER, FRAG)
  if (!vs || !fs) return
  program = gl.createProgram()
  if (!program) return
  gl.attachShader(program, vs)
  gl.attachShader(program, fs)
  gl.linkProgram(program)
  if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
    throw new Error(gl.getProgramInfoLog(program) || 'program link failed')
  }
  gl.useProgram(program)
  const buf = gl.createBuffer()
  gl.bindBuffer(gl.ARRAY_BUFFER, buf)
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([
    -1, -1, 1, -1, -1, 1,
    -1, 1, 1, -1, 1, 1,
  ]), gl.STATIC_DRAW)
  const aPos = gl.getAttribLocation(program, 'aPos')
  gl.enableVertexAttribArray(aPos)
  gl.vertexAttribPointer(aPos, 2, gl.FLOAT, false, 0, 0)
  uYaw = gl.getUniformLocation(program, 'uYaw')
  uPitch = gl.getUniformLocation(program, 'uPitch')
  uFov = gl.getUniformLocation(program, 'uFov')
  uAspect = gl.getUniformLocation(program, 'uAspect')
  gl.uniform1i(gl.getUniformLocation(program, 'uTex'), 0)
  tex = gl.createTexture()
  gl.bindTexture(gl.TEXTURE_2D, tex)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
}

function resize() {
  const canvas = canvasRef.value
  if (!canvas || !gl) return
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  const w = Math.max(1, Math.floor(canvas.clientWidth * dpr))
  const h = Math.max(1, Math.floor(canvas.clientHeight * dpr))
  if (canvas.width !== w || canvas.height !== h) {
    canvas.width = w
    canvas.height = h
    gl.viewport(0, 0, w, h)
  }
}

function draw() {
  if (!gl || !program || !ready.value) return
  resize()
  gl.useProgram(program)
  gl.uniform1f(uYaw, yaw)
  gl.uniform1f(uPitch, pitch)
  gl.uniform1f(uFov, fov)
  gl.uniform1f(uAspect, canvasRef.value ? canvasRef.value.width / Math.max(1, canvasRef.value.height) : 16 / 9)
  gl.drawArrays(gl.TRIANGLES, 0, 6)
}

function loop() {
  draw()
  raf = requestAnimationFrame(loop)
}

function loadTexture(url: string) {
  if (!gl || !tex) return
  error.value = ''
  ready.value = false
  const img = new Image()
  img.crossOrigin = 'anonymous'
  img.onload = () => {
    if (!gl || !tex) return
    gl.bindTexture(gl.TEXTURE_2D, tex)
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, 0)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, img)
    ready.value = true
    hint.value = '拖拽旋转 · 滚轮缩放'
  }
  img.onerror = () => {
    error.value = '全景图加载失败'
  }
  img.src = url
}

function onPointerDown(e: PointerEvent) {
  dragging = true
  lastX = e.clientX
  lastY = e.clientY
  ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
}

function onPointerMove(e: PointerEvent) {
  if (!dragging) return
  const dx = e.clientX - lastX
  const dy = e.clientY - lastY
  lastX = e.clientX
  lastY = e.clientY
  const sens = fov * 0.0022
  yaw -= dx * sens
  pitch += dy * sens
  const lim = Math.PI / 2 - 0.05
  if (pitch > lim) pitch = lim
  if (pitch < -lim) pitch = -lim
}

function onPointerUp() {
  dragging = false
}

function onWheel(e: WheelEvent) {
  e.preventDefault()
  const next = fov * (e.deltaY > 0 ? 1.06 : 0.94)
  fov = Math.min(Math.max(next, (40 * Math.PI) / 180), (110 * Math.PI) / 180)
}

function lookAt(yawDeg: number, pitchDeg = 0) {
  yaw = (yawDeg * Math.PI) / 180
  pitch = (pitchDeg * Math.PI) / 180
}

function resetView() {
  yaw = (props.initialYawDeg * Math.PI) / 180
  pitch = (props.initialPitchDeg * Math.PI) / 180
  fov = (props.initialFovDeg * Math.PI) / 180
}

defineExpose({ lookAt, resetView })

onMounted(() => {
  try {
    initGL()
    if (props.src) loadTexture(props.src)
    raf = requestAnimationFrame(loop)
  } catch (e: any) {
    error.value = e?.message || '全景查看器初始化失败'
  }
})

watch(() => props.src, (url) => {
  if (url) loadTexture(url)
})

onBeforeUnmount(() => {
  cancelAnimationFrame(raf)
  if (gl && tex) gl.deleteTexture(tex)
  if (gl && program) gl.deleteProgram(program)
  gl = null
})
</script>

<template>
  <div class="pano-viewer">
    <canvas
      ref="canvasRef"
      class="pano-viewer-canvas"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerUp"
      @wheel="onWheel"
    />
    <div v-if="error" class="pano-viewer-error">{{ error }}</div>
    <div v-else-if="!ready" class="pano-viewer-loading">加载全景中…</div>
    <div class="pano-viewer-hint">{{ hint }}</div>
    <div class="pano-viewer-presets">
      <button type="button" @click="lookAt(initialYawDeg + 0)">正面</button>
      <button type="button" @click="lookAt(initialYawDeg + 90)">右侧</button>
      <button type="button" @click="lookAt(initialYawDeg + 180)">背面</button>
      <button type="button" @click="lookAt(initialYawDeg - 90)">左侧</button>
      <button type="button" @click="lookAt(initialYawDeg, -35)">仰视</button>
      <button type="button" @click="lookAt(initialYawDeg, 35)">俯视</button>
      <button type="button" class="muted" @click="resetView">复位</button>
    </div>
  </div>
</template>

<style scoped>
.pano-viewer {
  position: relative;
  width: 100%;
  height: min(70vh, 640px);
  background: #0b0d10;
  border-radius: 8px;
  overflow: hidden;
  touch-action: none;
  user-select: none;
}
.pano-viewer-canvas {
  width: 100%;
  height: 100%;
  display: block;
  cursor: grab;
}
.pano-viewer-canvas:active {
  cursor: grabbing;
}
.pano-viewer-loading,
.pano-viewer-error {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: #e8eaed;
  background: rgba(0, 0, 0, 0.45);
  pointer-events: none;
}
.pano-viewer-error {
  color: #ffb4a8;
}
.pano-viewer-hint {
  position: absolute;
  left: 12px;
  bottom: 48px;
  padding: 4px 8px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.45);
  color: rgba(255, 255, 255, 0.85);
  font-size: 12px;
  pointer-events: none;
}
.pano-viewer-presets {
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: 10px;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.pano-viewer-presets button {
  border: 0;
  border-radius: 4px;
  padding: 4px 10px;
  background: rgba(255, 255, 255, 0.14);
  color: #fff;
  font-size: 12px;
  cursor: pointer;
}
.pano-viewer-presets button:hover {
  background: rgba(255, 255, 255, 0.24);
}
.pano-viewer-presets button.muted {
  background: rgba(255, 255, 255, 0.08);
}
</style>

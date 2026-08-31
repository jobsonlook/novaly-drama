<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useNovalyInject } from '@/composables/useNovalyInject'
import type { Resource, ShotRef } from '@/types'

type PickerTab = 'recent' | 'all' | 'character' | 'scene' | 'prop' | 'other'

const {
  sceneRefPickerOpen,
  sceneRefPickerTitle,
  sceneRefPickerHint,
  refPickerReferences,
  refPickerMax,
  pickerPrimaryCharacters,
  pickerPrimaryScenes,
  pickerFlatScenes,
  sceneGridsMap,
  gridCellsFor,
  loadGridCells,
  gridIsSplit,
  sceneGridAngleOf,
  splitGridResource,
  splittingGridIds,
  pickerPrimaryProps,
  pickerPrimaryOthers,
  recentShotRefs,
  closeSceneRefPicker,
  isSceneRefSelected,
  isSceneRefDisabled,
  pickSceneCharacterRef,
  pickSceneSceneRef,
  pickScenePropRef,
  pickSceneOtherRef,
  pickSceneRecentRef,
  characterImage,
  sceneImage,
  otherImage,
  pickerResourceName,
  refThumb,
  refLabel,
  refPickerReplaceHint,
} = useNovalyInject()

const search = ref('')
const activeTab = ref<PickerTab>('recent')
const drillScene = ref<Resource | null>(null)

function gridsOf(s: Resource): Resource[] {
  return sceneGridsMap.value.get(s.id) ?? []
}

const drillGrids = computed(() => (drillScene.value ? gridsOf(drillScene.value) : []))

function gridCellLabel(cell: Resource): string {
  return sceneGridAngleOf(cell) || (cell.gridCell ? `格${cell.gridCell}` : pickerResourceName(cell))
}

const positioningReplaceHint = computed(() => refPickerReplaceHint.value)

watch(sceneRefPickerOpen, (v) => {
  if (v) {
    search.value = ''
    activeTab.value = 'recent'
    drillScene.value = null
  }
})

watch(drillScene, (scene) => {
  if (!scene) return
  for (const grid of gridsOf(scene)) void loadGridCells(grid.id)
})

function matchesQuery(r: Resource, q: string) {
  if (!q) return true
  const name = pickerResourceName(r).toLowerCase()
  const desc = (r.description || '').toLowerCase()
  return name.includes(q) || desc.includes(q)
}

function matchesRecentQuery(ref: ShotRef, q: string) {
  if (!q) return true
  return refLabel(ref).toLowerCase().includes(q)
}

const query = computed(() => search.value.trim().toLowerCase())

const filteredRecent = computed(() =>
  recentShotRefs.value.filter(r => matchesRecentQuery(r, query.value)),
)
const filteredCharacters = computed(() =>
  pickerPrimaryCharacters.value.filter(c => matchesQuery(c, query.value)),
)
const filteredScenes = computed(() =>
  pickerFlatScenes.value.filter(s => matchesQuery(s, query.value)),
)
const filteredProps = computed(() =>
  pickerPrimaryProps.value.filter(p => matchesQuery(p, query.value)),
)
const filteredOthers = computed(() =>
  pickerPrimaryOthers.value.filter(o => matchesQuery(o, query.value)),
)

const tabs = computed(() => {
  const list: { id: PickerTab; label: string; count: number }[] = [
    {
      id: 'recent',
      label: '最近使用',
      count: filteredRecent.value.length,
    },
    {
      id: 'all',
      label: '全部',
      count:
        filteredCharacters.value.length +
        filteredScenes.value.length +
        filteredProps.value.length +
        filteredOthers.value.length,
    },
  ]
  if (pickerPrimaryCharacters.value.length) {
    list.push({ id: 'character', label: '角色', count: filteredCharacters.value.length })
  }
  if (pickerFlatScenes.value.length) {
    list.push({ id: 'scene', label: '场景', count: filteredScenes.value.length })
  }
  if (pickerPrimaryProps.value.length) {
    list.push({ id: 'prop', label: '道具', count: filteredProps.value.length })
  }
  if (pickerPrimaryOthers.value.length) {
    list.push({ id: 'other', label: '其他', count: filteredOthers.value.length })
  }
  return list
})

const showRecent = computed(() => activeTab.value === 'recent')
const showCharacters = computed(() => activeTab.value === 'all' || activeTab.value === 'character')
const showScenes = computed(() => activeTab.value === 'all' || activeTab.value === 'scene')
const showProps = computed(() => activeTab.value === 'all' || activeTab.value === 'prop')
const showOthers = computed(() => activeTab.value === 'all' || activeTab.value === 'other')

const hasAnySource = computed(() =>
  !!(
    recentShotRefs.value.length ||
    pickerPrimaryCharacters.value.length ||
    pickerPrimaryScenes.value.length ||
    pickerPrimaryProps.value.length ||
    pickerPrimaryOthers.value.length
  ),
)

const hasFiltered = computed(() =>
  !!(
    (showRecent.value && filteredRecent.value.length) ||
    (showCharacters.value && filteredCharacters.value.length) ||
    (showScenes.value && filteredScenes.value.length) ||
    (showProps.value && filteredProps.value.length) ||
    (showOthers.value && filteredOthers.value.length)
  ),
)

function recentEmptyHint() {
  if (query.value) return '没有匹配的最近使用'
  return '暂无最近使用，从其他分类添加后会出现在这里'
}
</script>

<template>
  <el-dialog
    :model-value="sceneRefPickerOpen"
    :title="sceneRefPickerTitle"
    width="720px"
    class="modal-wide"
    align-center
    @close="closeSceneRefPicker"
  >
    <p class="modal-hint">{{ sceneRefPickerHint }}</p>

    <div v-if="hasAnySource && !drillScene" class="picker-toolbar">
      <div class="picker-tabs" role="tablist">
        <button
          v-for="tab in tabs"
          :key="tab.id"
          type="button"
          role="tab"
          class="picker-tab"
          :class="{ on: activeTab === tab.id }"
          :aria-selected="activeTab === tab.id"
          @click="activeTab = tab.id"
        >
          {{ tab.label }}
          <span class="picker-tab-count">{{ tab.count }}</span>
        </button>
      </div>
      <el-input
        v-model="search"
        clearable
        placeholder="搜索名称或描述…"
        class="picker-search"
      />
    </div>

    <div v-if="drillScene" class="modal-body-scroll">
      <div class="grid-drill-head">
        <el-button size="small" text @click="drillScene = null">← 返回场景列表</el-button>
        <b>{{ pickerResourceName(drillScene) }} · 9宫格</b>
        <span class="hint">点选整图，或切分后按格挑选</span>
      </div>
      <section v-for="g in drillGrids" :key="g.id" class="ref-section grid-drill-block">
        <div class="grid-drill-grid-head">
          <button
            type="button"
            class="modal-card grid-whole-pick"
            :class="{ on: isSceneRefSelected({ kind: 'scene', id: g.id, variant: 'original' }), disabled: isSceneRefDisabled({ kind: 'scene', id: g.id, variant: 'original' }) }"
            :disabled="isSceneRefDisabled({ kind: 'scene', id: g.id, variant: 'original' })"
            @click="pickSceneSceneRef(g.id, 'original')"
          >
            <img :src="g.imageUrl" :alt="pickerResourceName(g)" />
            <b>整图</b>
          </button>
          <div class="grid-drill-meta">
            <b>{{ pickerResourceName(g) }}</b>
            <div class="grid-drill-meta-row">
              <span v-if="gridIsSplit(g.id)" class="tag grid-tag">已切分</span>
              <el-button
                v-else
                size="small"
                :loading="splittingGridIds.has(g.id)"
                @click="splitGridResource(g)"
              >
                切分9格
              </el-button>
            </div>
          </div>
        </div>
        <div v-if="gridCellsFor(g.id).length" class="modal-grid grid-cell-pick-grid">
          <button
            v-for="cell in gridCellsFor(g.id)"
            :key="cell.id"
            type="button"
            class="modal-card"
            :class="{ on: isSceneRefSelected({ kind: 'scene', id: cell.id, variant: 'original' }), disabled: isSceneRefDisabled({ kind: 'scene', id: cell.id, variant: 'original' }) }"
            :disabled="isSceneRefDisabled({ kind: 'scene', id: cell.id, variant: 'original' })"
            @click="pickSceneSceneRef(cell.id, 'original')"
          >
            <img :src="cell.imageUrl" :alt="gridCellLabel(cell)" />
            <b>{{ gridCellLabel(cell) }}</b>
          </button>
        </div>
        <p v-else class="hint">该9宫格尚未切分；切分后可按格挑选，也可直接选用左侧整图</p>
      </section>
      <p v-if="!drillGrids.length" class="hint">该场景还没有9宫格，可先在资源管理中生成</p>
    </div>
    <div v-else class="modal-body-scroll">
      <section v-if="showRecent" class="ref-section">
        <div v-if="filteredRecent.length" class="modal-grid recent-pick-grid">
          <button
            v-for="item in filteredRecent"
            :key="`${item.kind}-${item.id}-${item.variant || ''}`"
            type="button"
            class="modal-card"
            :class="{ on: isSceneRefSelected(item), disabled: isSceneRefDisabled(item) }"
            :disabled="isSceneRefDisabled(item)"
            @click="pickSceneRecentRef(item)"
          >
            <img v-if="refThumb(item)" :src="refThumb(item)" :alt="refLabel(item)" />
            <b>{{ refLabel(item) }}</b>
          </button>
        </div>
        <p v-else class="hint">{{ recentEmptyHint() }}</p>
      </section>
      <section v-if="showCharacters && filteredCharacters.length" class="ref-section">
        <h4 v-if="activeTab === 'all'">角色</h4>
        <div class="modal-grid character-pick-grid">
          <div v-for="c in filteredCharacters" :key="c.id" class="modal-character">
            <b class="modal-character-name">{{ pickerResourceName(c) }}</b>
            <p class="modal-character-desc">{{ c.description || '暂无描述' }}</p>
            <div class="variant-picks">
              <button
                v-if="c.stylizedImageUrl"
                type="button"
                class="variant-pick"
                :class="{ on: isSceneRefSelected({ kind: 'character', id: c.id, variant: 'stylized' }), disabled: isSceneRefDisabled({ kind: 'character', id: c.id, variant: 'stylized' }) }"
                :disabled="isSceneRefDisabled({ kind: 'character', id: c.id, variant: 'stylized' })"
                @click="pickSceneCharacterRef(c.id, 'stylized')"
              >
                <img :src="c.stylizedImageUrl" :alt="c.name + '非真人'" />
                <span>非真人</span>
              </button>
              <button
                v-if="characterImage(c, 'original')"
                type="button"
                class="variant-pick"
                :class="{ on: isSceneRefSelected({ kind: 'character', id: c.id, variant: 'original' }), disabled: isSceneRefDisabled({ kind: 'character', id: c.id, variant: 'original' }) }"
                :disabled="isSceneRefDisabled({ kind: 'character', id: c.id, variant: 'original' })"
                @click="pickSceneCharacterRef(c.id, 'original')"
              >
                <img :src="characterImage(c, 'original')" :alt="c.name + '真人'" />
                <span>真人</span>
              </button>
            </div>
          </div>
        </div>
      </section>
      <section v-if="showScenes && filteredScenes.length" class="ref-section">
        <h4 v-if="activeTab === 'all'">场景</h4>
        <div class="modal-grid character-pick-grid">
          <div v-for="s in filteredScenes" :key="s.id" class="modal-character">
            <b class="modal-character-name">{{ pickerResourceName(s) }}</b>
            <p class="modal-character-desc">{{ s.description || '暂无描述' }}</p>
            <div class="variant-picks">
              <button
                v-if="s.stylizedImageUrl"
                type="button"
                class="variant-pick"
                :class="{ on: isSceneRefSelected({ kind: 'scene', id: s.id, variant: 'stylized' }), disabled: isSceneRefDisabled({ kind: 'scene', id: s.id, variant: 'stylized' }) }"
                :disabled="isSceneRefDisabled({ kind: 'scene', id: s.id, variant: 'stylized' })"
                @click="pickSceneSceneRef(s.id, 'stylized')"
              >
                <img :src="s.stylizedImageUrl" :alt="s.name + '非真人'" />
                <span>非真人</span>
              </button>
              <button
                v-if="gridsOf(s).length"
                type="button"
                class="variant-pick variant-pick-grid"
                @click.stop="drillScene = s"
              >
                <img :src="gridsOf(s)[0].imageUrl" :alt="s.name + '9宫格'" />
                <span>9宫格 ›</span>
              </button>
              <button
                v-if="sceneImage(s, 'original')"
                type="button"
                class="variant-pick"
                :class="{ on: isSceneRefSelected({ kind: 'scene', id: s.id, variant: 'original' }), disabled: isSceneRefDisabled({ kind: 'scene', id: s.id, variant: 'original' }) }"
                :disabled="isSceneRefDisabled({ kind: 'scene', id: s.id, variant: 'original' })"
                @click="pickSceneSceneRef(s.id, 'original')"
              >
                <img :src="sceneImage(s, 'original')" :alt="s.name + '原图'" />
                <span>原图</span>
              </button>
            </div>
          </div>
        </div>
      </section>
      <section v-if="showProps && filteredProps.length" class="ref-section">
        <h4 v-if="activeTab === 'all'">道具</h4>
        <div class="modal-grid">
          <button
            v-for="p in filteredProps"
            :key="p.id"
            type="button"
            class="modal-card"
            :class="{ on: isSceneRefSelected({ kind: 'prop', id: p.id }), disabled: isSceneRefDisabled({ kind: 'prop', id: p.id }) }"
            :disabled="isSceneRefDisabled({ kind: 'prop', id: p.id })"
            @click="pickScenePropRef(p.id)"
          >
            <img v-if="p.imageUrl" :src="p.imageUrl" :alt="pickerResourceName(p)" />
            <b>{{ pickerResourceName(p) }}</b>
            <p>{{ p.description || '暂无描述' }}</p>
          </button>
        </div>
      </section>
      <section v-if="showOthers && filteredOthers.length" class="ref-section">
        <h4 v-if="activeTab === 'all'">其他</h4>
        <div class="modal-grid character-pick-grid">
          <div v-for="o in filteredOthers" :key="o.id" class="modal-character">
            <b class="modal-character-name">{{ pickerResourceName(o) }}</b>
            <p class="modal-character-desc">{{ o.description || '暂无描述' }}</p>
            <div class="variant-picks">
              <button
                v-if="o.stylizedImageUrl"
                type="button"
                class="variant-pick"
                :class="{ on: isSceneRefSelected({ kind: 'other', id: o.id, variant: 'stylized' }), disabled: isSceneRefDisabled({ kind: 'other', id: o.id, variant: 'stylized' }) }"
                :disabled="isSceneRefDisabled({ kind: 'other', id: o.id, variant: 'stylized' })"
                @click="pickSceneOtherRef(o.id, 'stylized')"
              >
                <img :src="o.stylizedImageUrl" :alt="o.name + '非真人'" />
                <span>非真人</span>
              </button>
              <button
                v-if="otherImage(o, 'original')"
                type="button"
                class="variant-pick"
                :class="{ on: isSceneRefSelected({ kind: 'other', id: o.id, variant: 'original' }), disabled: isSceneRefDisabled({ kind: 'other', id: o.id, variant: 'original' }) }"
                :disabled="isSceneRefDisabled({ kind: 'other', id: o.id, variant: 'original' })"
                @click="pickSceneOtherRef(o.id, 'original')"
              >
                <img :src="otherImage(o, 'original')" :alt="o.name + '原图'" />
                <span>原图</span>
              </button>
            </div>
          </div>
        </div>
      </section>
      <p v-if="!hasAnySource" class="hint">请先在资源管理中心添加资源</p>
      <p v-else-if="!hasFiltered && activeTab !== 'recent'" class="hint">没有匹配的资源</p>
    </div>
    <template #footer>
      <span class="hint">
        <template v-if="positioningReplaceHint">{{ positioningReplaceHint }}</template>
        <template v-else>已选 {{ refPickerReferences.length }}/{{ refPickerMax }} 张</template>
      </span>
      <el-button type="primary" @click="closeSceneRefPicker">
        {{ positioningReplaceHint ? '取消替换' : '完成' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.picker-toolbar {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 0 20px 12px;
  background: #1a1816;
}

.picker-search {
  width: 100%;
}

.picker-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.picker-tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border: 1px solid #3a352f;
  border-radius: 6px;
  background: #221f1c;
  color: #b8b0a6;
  font-size: 13px;
  cursor: pointer;
  transition: border-color 0.15s, color 0.15s, background 0.15s;
}

.picker-tab:hover {
  border-color: #5a5248;
  color: #ebe4db;
}

.picker-tab.on {
  border-color: #c45c3e;
  background: rgba(196, 92, 62, 0.14);
  color: #f0e8df;
}

.picker-tab-count {
  min-width: 1.25em;
  padding: 0 5px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.06);
  font-size: 11px;
  line-height: 1.5;
  color: #9a9288;
}

.picker-tab.on .picker-tab-count {
  background: rgba(196, 92, 62, 0.25);
  color: #e8c4b8;
}

.recent-pick-grid {
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
}

.variant-pick-grid {
  border-style: dashed;
}

.grid-drill-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 0 20px 12px;
}

.grid-drill-block {
  border-top: 1px solid #322d28;
  padding-top: 12px;
}

.grid-drill-grid-head {
  display: flex;
  gap: 14px;
  align-items: flex-start;
  margin-bottom: 12px;
}

.grid-whole-pick {
  width: 180px;
  flex-shrink: 0;
}

.grid-drill-meta {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}

.grid-drill-meta-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.grid-cell-pick-grid {
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
}
</style>

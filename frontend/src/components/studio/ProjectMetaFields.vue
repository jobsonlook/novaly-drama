<script setup lang="ts">
import { directorManuals, visualManuals } from '@/data/projectManuals'

export type ProjectMetaModel = {
  kind?: string
  title: string
  genre?: string
  videoRatio: string
  synopsis?: string
  visualManual?: string
  directorManual?: string
  storyboardPace?: string
  style: string
}

const model = defineModel<ProjectMetaModel>({ required: true })

function pickVisual(id: string) {
  const item = visualManuals.find(v => v.id === id)
  model.value.visualManual = id
  if (item?.stylePrompt) model.value.style = item.stylePrompt
}

function pickDirector(id: string) {
  model.value.directorManual = id
}
</script>

<template>
  <div class="project-meta">
    <div class="project-meta-left">
      <el-form-item label="项目类型">
        <el-select v-model="model.kind" style="width: 100%">
          <el-option label="基于剧本" value="script" />
          <el-option label="基于小说" value="novel" />
        </el-select>
      </el-form-item>
      <el-form-item label="项目名称">
        <el-input v-model="model.title" placeholder="请输入项目名称" />
      </el-form-item>
      <el-form-item label="小说类型">
        <el-input v-model="model.genre" placeholder="例如：玄幻、科幻、言情" />
      </el-form-item>
      <el-form-item label="影片比例">
        <el-select v-model="model.videoRatio" style="width: 100%">
          <el-option label="16:9 横屏" value="16:9" />
          <el-option label="9:16 竖屏" value="9:16" />
          <el-option label="4:3 传统" value="4:3" />
          <el-option label="1:1 方形" value="1:1" />
        </el-select>
      </el-form-item>
      <el-form-item label="拆镜节奏">
        <el-select v-model="model.storyboardPace" style="width: 100%">
          <el-option label="细切（一句一对白一镜，接近第1集）" value="fine" />
          <el-option label="打包（同场打进 10 秒大镜）" value="packed" />
        </el-select>
      </el-form-item>
      <el-form-item :label="model.kind === 'novel' ? '小说简介' : '故事简介'">
        <el-input
          v-model="model.synopsis"
          type="textarea"
          :rows="5"
          :placeholder="model.kind === 'novel' ? '请输入小说简介' : '一句话故事、人物关系或本集看点'"
        />
      </el-form-item>
    </div>

    <div class="project-meta-right">
      <el-form-item label="视觉手册">
        <div class="manual-grid">
          <button
            v-for="item in visualManuals"
            :key="item.id"
            type="button"
            class="manual-card visual"
            :class="{ selected: model.visualManual === item.id }"
            @click="pickVisual(item.id)"
          >
            <span class="manual-swatch" :style="{ background: item.tone }" />
            <strong>{{ item.name }}</strong>
            <span>{{ item.desc }}</span>
          </button>
        </div>
      </el-form-item>
      <el-form-item label="导演手册">
        <div class="manual-grid director">
          <button
            v-for="item in directorManuals"
            :key="item.id"
            type="button"
            class="manual-card"
            :class="{ selected: model.directorManual === item.id }"
            @click="pickDirector(item.id)"
          >
            <strong>{{ item.name }}</strong>
            <span>{{ item.desc }}</span>
          </button>
        </div>
      </el-form-item>
    </div>
  </div>
</template>

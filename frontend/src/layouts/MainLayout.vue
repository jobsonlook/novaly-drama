<script setup lang="ts">
defineProps<{
  showSidebar?: boolean
}>()
</script>

<template>
  <el-container class="novaly-layout" :class="{ 'no-aside': !showSidebar }">
    <el-aside v-if="showSidebar" width="260px" class="novaly-aside">
      <slot name="sidebar" />
    </el-aside>
    <el-container class="novaly-body">
      <slot name="header" />
      <el-main class="novaly-main">
        <slot />
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.novaly-layout {
  min-height: 100vh;
  background: #11100f;
}

.novaly-aside {
  padding: 0;
  overflow: hidden;
  border-right: 1px solid #2a2622;
  background: #141210;
}

.novaly-body {
  flex-direction: column;
  min-width: 0;
}

.novaly-main {
  padding: 0;
  overflow: auto;
  background: #11100f;
}

.novaly-layout.no-aside .novaly-main {
  background:
    radial-gradient(ellipse 80% 50% at 50% -10%, rgba(255, 120, 90, 0.08), transparent 55%),
    #11100f;
}
</style>

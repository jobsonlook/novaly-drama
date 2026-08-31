<script setup lang="ts">
import { provide } from 'vue'
import { useNovaly, NovalyKey } from '@/composables/useNovaly'
import MainLayout from '@/layouts/MainLayout.vue'
import AppTopBar from '@/components/layout/AppTopBar.vue'
import AppModals from '@/components/modals/AppModals.vue'

const novaly = useNovaly()
provide(NovalyKey, novaly)
</script>

<template>
  <MainLayout :show-sidebar="false">
    <template #header>
      <AppTopBar mode="full" />
    </template>
    <RouterView />
  </MainLayout>
  <AppModals />
</template>

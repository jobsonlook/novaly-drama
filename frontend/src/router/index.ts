import { createRouter, createWebHistory } from 'vue-router'
import HomeView from '@/views/HomeView.vue'
import StudioView from '@/views/StudioView.vue'
import SettingsView from '@/views/SettingsView.vue'
import TtsView from '@/views/TtsView.vue'
import EditorView from '@/views/EditorView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'home', component: HomeView },
    { path: '/projects/:id', name: 'project', component: StudioView },
    { path: '/projects/:id/episodes/:episodeNumber', name: 'project-episode', component: StudioView },
    { path: '/projects/:id/resources', name: 'project-resources', component: StudioView },
    { path: '/settings', name: 'settings', component: SettingsView },
    { path: '/settings/download', name: 'settings-download', component: SettingsView },
    { path: '/settings/trash', name: 'settings-trash', component: SettingsView },
    { path: '/tts', name: 'tts', component: TtsView },
    { path: '/projects/:id/editor/:episodeId', name: 'editor', component: EditorView },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

export default router

import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import LoginView from '../views/LoginView.vue'
import MainLayout from '../layouts/MainLayout.vue'
import DevicesView from '../views/DevicesView.vue'
import MonitorView from '../views/MonitorView.vue'
import MappingView from '../views/MappingView.vue'
import DiagnosticsView from '../views/DiagnosticsView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginView },
    {
      path: '/',
      component: MainLayout,
      meta: { auth: true },
      children: [
        { path: '', redirect: '/devices' },
        { path: 'devices', name: 'devices', component: DevicesView },
        { path: 'devices/:id/monitor', name: 'monitor', component: MonitorView },
        { path: 'mapping', name: 'mapping', component: MappingView },
        { path: 'diagnostics', name: 'diagnostics', component: DiagnosticsView }
      ]
    }
  ]
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.meta.auth && !auth.isLoggedIn) return { name: 'login' }
  if (to.name === 'login' && auth.isLoggedIn) return { name: 'devices' }
})

export default router

import { createRouter, createWebHistory } from 'vue-router'
import { bootstrap, state } from '@/lib/store'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/Login.vue'), meta: { public: true } },
    { path: '/', name: 'dashboard', component: () => import('@/views/Dashboard.vue') },
    { path: '/accounts', name: 'accounts', component: () => import('@/views/Accounts.vue') },
    { path: '/budget', name: 'budget', component: () => import('@/views/Budget.vue') },
    { path: '/plan', name: 'plan', component: () => import('@/views/Plan.vue') },
    { path: '/withdrawal', name: 'withdrawal', component: () => import('@/views/Withdrawal.vue') },
    { path: '/freedom', name: 'freedom', component: () => import('@/views/Freedom.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach(async (to) => {
  if (!state.ready) await bootstrap()
  if (!to.meta.public && !state.user) return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : undefined }
  if (to.name === 'login' && state.user) return { name: 'dashboard' }
})

export default router

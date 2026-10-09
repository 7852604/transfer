// 速传 Service Worker：静态资源缓存，离线可打开外壳
// 策略：/api 一律走网络；带 hash 的 /assets 走 cache-first；HTML 走 network-first
const CACHE = 'sucheng-v1'

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== location.origin) return
  // API 实时性优先，绝不缓存
  if (url.pathname.startsWith('/api/')) return

  // 静态资产（内容寻址，文件名带 hash）：cache-first
  if (url.pathname.startsWith('/assets/') || url.pathname.startsWith('/icon-')) {
    event.respondWith(
      caches.open(CACHE).then(async (cache) => {
        const hit = await cache.match(req)
        if (hit) return hit
        const res = await fetch(req)
        if (res.ok) cache.put(req, res.clone())
        return res
      })
    )
    return
  }

  // HTML 外壳：network-first，离线回退缓存
  event.respondWith(
    (async () => {
      try {
        const res = await fetch(req)
        if (res.ok && req.mode === 'navigate') {
          const cache = await caches.open(CACHE)
          cache.put('/', res.clone())
        }
        return res
      } catch {
        const cache = await caches.open(CACHE)
        const hit = await cache.match('/')
        if (hit) return hit
        throw new Error('offline')
      }
    })()
  )
})

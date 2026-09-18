/* Uniminute background notifications. Firebase's compat build is used here
   because this file runs directly as a service worker without a bundler. */
importScripts('https://www.gstatic.com/firebasejs/12.19.0/firebase-app-compat.js');
importScripts('https://www.gstatic.com/firebasejs/12.19.0/firebase-messaging-compat.js');

const ready = fetch('/api/push/config', { headers: { Accept: 'application/json' } })
  .then((res) => {
    if (!res.ok) throw new Error('push config unavailable');
    return res.json();
  })
  .then(({ firebase: config }) => {
    if (!firebase.apps.length) firebase.initializeApp(config);
    const messaging = firebase.messaging();
    messaging.onBackgroundMessage((payload) => {
      const data = payload?.data || {};
      return self.registration.showNotification(data.title || 'Uniminute order update', {
        body: data.body || 'Open Uniminute to see what changed.',
        icon: '/static/images/logo.png',
        badge: '/static/images/logo.png',
        tag: data.tag || 'uniminute-order',
        data: { url: data.url || '/orders' },
      });
    });
  })
  .catch((err) => console.error('Uniminute push setup:', err));

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = new URL(event.notification.data?.url || '/orders', self.location.origin).href;
  event.waitUntil((async () => {
    const windows = await clients.matchAll({ type: 'window', includeUncontrolled: true });
    for (const client of windows) {
      if (new URL(client.url).origin === self.location.origin) {
        await client.navigate(target);
        return client.focus();
      }
    }
    return clients.openWindow(target);
  })());
});

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));

void ready;

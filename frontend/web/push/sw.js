// Service worker for Firebase Cloud Messaging web push. It stays separate from
// Flutter's own service worker, which handles caching and knows nothing about
// notifications.

importScripts('https://www.gstatic.com/firebasejs/10.14.1/firebase-app-compat.js');
importScripts('https://www.gstatic.com/firebasejs/10.14.1/firebase-messaging-compat.js');

// The same one config index.html reads, and the same project the API's
// service account sends from. A second hardcoded copy used to live here; a
// worker registering against a different project than the server pushes to is
// a silent failure nobody can see from either end.
self.importScripts('/firebase-env.js');

firebase.initializeApp(self.lamazonFirebaseConfig);

const messaging = firebase.messaging();

messaging.onBackgroundMessage((payload) => {
  console.info('[Unimiunte push] background FCM payload', payload);
  showLamazonNotification({ ...(payload.notification || {}), ...(payload.data || {}) });
});

// FCM data values arrive as strings, so "false" would be truthy — every flag
// has to be compared, never coerced.
function isTrue(value) {
  return value === true || value === 'true';
}

function showLamazonNotification(payload) {
  const confirm = isTrue(payload.confirm);
  const options = {
    body: payload.body,
    icon: '/icons/Icon-192.png',
    badge: '/icons/Icon-192.png',
    // Same tag replaces the previous notification instead of stacking a
    // dozen of them when several orders land together.
    tag: payload.tag || 'lamazon',
    data: { url: payload.url || '/', confirm },
  };
  // The test notification carries a button, because the only real proof it
  // arrived is the person answering it.
  if (confirm) {
    options.actions = [{ action: 'confirm', title: 'Yes, got it' }];
    options.requireInteraction = true; // do not vanish before it is answered
  }

  // Telling the page it was drawn separates "push is broken" from "you did
  // not tap it" — two very different problems that looked identical before.
  if (confirm) tellClients({ type: 'push-delivered' });
  return self.registration.showNotification(payload.title || 'Unimiunte', options);
}

function tellClients(message) {
  return self.clients
    .matchAll({ type: 'window', includeUncontrolled: true })
    .then((tabs) => tabs.forEach((tab) => tab.postMessage(message)));
}

// Tapping the notification should land in the app, reusing the tab that is
// already open rather than piling up new ones. Tapping "Yes, got it" also
// tells that tab, so the screen can stop waiting and say it worked.
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const data = event.notification.data || {};
  const confirmed = event.action === 'confirm' || isTrue(data.confirm);

  event.waitUntil(
    self.clients
      .matchAll({ type: 'window', includeUncontrolled: true })
      .then((tabs) => {
        for (const tab of tabs) {
          if (confirmed) tab.postMessage({ type: 'push-confirmed' });
          if ('focus' in tab) return tab.focus();
        }
        // No tab open: carry the answer in the URL instead of losing it.
        return self.clients.openWindow(
          confirmed ? '/?push=confirmed' : data.url || '/',
        );
      }),
  );
});

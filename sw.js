/* Boxdeck service worker: shows pushed alerts and runs their buttons.
   The deck decides what to send (rules, quiet hours, disarm). This file only renders what
   arrives and calls the deck back with the per alert action token from the payload. */
'use strict';

self.addEventListener('install', function () { self.skipWaiting(); });
self.addEventListener('activate', function (event) { event.waitUntil(self.clients.claim()); });

function parsePayload(event) {
  if (!event.data) return null;
  try { return event.data.json(); } catch (error) { return {title: 'Boxdeck', body: event.data.text()}; }
}

function safeLink(link) {
  var value = String(link || '#/alerts').trim();
  return (/^#\//.test(value) || /^\/(?!\/)/.test(value)) ? value : '#/alerts';
}

function safePath(path) {
  var value = String(path || '').trim();
  return /^\/api\/[A-Za-z0-9_\-\/.]+$/.test(value) ? value : '';
}

function notificationActions(actions) {
  var list = [];
  (actions || []).forEach(function (action, index) {
    if (!action || !action.label || !safePath(action.path)) return;
    list.push({action: 'act:' + index, title: String(action.label)});
  });
  return list;
}

self.addEventListener('push', function (event) {
  var payload = parsePayload(event);
  if (!payload) return;
  var severity = payload.severity || 'info';
  var options = {
    body: payload.body || '',
    tag: payload.id || undefined,
    renotify: Boolean(payload.id),
    requireInteraction: severity === 'critical',
    silent: severity === 'info',
    icon: '/icon.svg',
    badge: '/icon.svg',
    timestamp: payload.at ? new Date(payload.at).getTime() : Date.now(),
    data: payload,
    actions: notificationActions(payload.actions)
  };
  var title = payload.title || 'Boxdeck';
  if (payload.box) title = title + ' / ' + payload.box;
  event.waitUntil(self.registration.showNotification(title, options));
});

function deckOrigin(payload) {
  var base = payload && payload.deckURL ? String(payload.deckURL) : '';
  if (!base) base = self.location.origin;
  return base.replace(/\/+$/, '');
}

function runAction(payload, action) {
  var path = safePath(action.path);
  if (!path) return Promise.resolve();
  var headers = {'Content-Type': 'application/json'};
  if (payload.actionToken) headers.Authorization = 'Bearer ' + payload.actionToken;
  return fetch(deckOrigin(payload) + path, {
    method: action.method || 'POST',
    headers: headers,
    body: JSON.stringify(action.body || {}),
    credentials: 'include'
  }).then(function (response) {
    if (!response.ok) throw new Error('HTTP ' + response.status);
  }).catch(function (error) {
    return self.registration.showNotification('Boxdeck action failed', {body: action.label + ': ' + error.message, icon: '/icon.svg', tag: 'boxdeck-action-error'});
  });
}

function openDeck(payload) {
  var target = deckOrigin(payload) + '/' + safeLink(payload.link).replace(/^\//, '');
  return self.clients.matchAll({type: 'window', includeUncontrolled: true}).then(function (windows) {
    for (var i = 0; i < windows.length; i++) {
      var client = windows[i];
      if (client.url.indexOf(deckOrigin(payload)) === 0 && 'focus' in client) {
        if ('navigate' in client) client.navigate(target).catch(function () {});
        return client.focus();
      }
    }
    return self.clients.openWindow(target);
  });
}

self.addEventListener('notificationclick', function (event) {
  var payload = event.notification.data || {};
  event.notification.close();
  if (event.action && event.action.indexOf('act:') === 0) {
    var index = Number(event.action.slice(4));
    var action = (payload.actions || [])[index];
    if (action) { event.waitUntil(runAction(payload, action)); return; }
  }
  event.waitUntil(openDeck(payload));
});

self.addEventListener('pushsubscriptionchange', function (event) {
  var keyPromise = fetch('/api/push/web', {credentials: 'include'}).then(function (response) { return response.json(); });
  event.waitUntil(keyPromise.then(function (status) {
    return self.registration.pushManager.subscribe({userVisibleOnly: true, applicationServerKey: status.publicKey});
  }).then(function (subscription) {
    return fetch('/api/push/web/subscribe', {method: 'POST', credentials: 'include', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({subscription: subscription.toJSON()})});
  }).catch(function () {}));
});

/* No fetch handler on purpose: the deck is live data and must never be served from a cache. */

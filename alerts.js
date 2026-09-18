(function () {
  'use strict';
  const addView = window.boxdeck && window.boxdeck.addView;
  if (!addView) return;
  const esc = (value) => String(value == null ? '' : value).replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  // Alert links and action paths come from the server, but the inbox lets any local process file an
  // alert, so only deck hash links and same origin paths are ever rendered.
  var safeLink = function (u) { var s = String(u || '#/overview').trim(); return (/^#\//.test(s) || /^\/(?!\/)/.test(s)) ? s : '#/overview'; };
  var safePath = function (p) { var s = String(p || '').trim(); return /^\/api\/[A-Za-z0-9_\-\/.]+$/.test(s) ? s : ''; };
  const api = async (path, options) => {
    const response = await fetch(path, Object.assign({cache: 'no-store'}, options || {}));
    const value = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(value.error || 'Request failed');
    return value;
  };
  let mode = 'open';
  let alerts = [];
  let config = null;
  let container = null;

  function age(at) {
    const seconds = Math.max(0, (Date.now() - new Date(at).getTime()) / 1000);
    if (seconds < 60) return Math.floor(seconds) + 's';
    if (seconds < 3600) return Math.floor(seconds / 60) + 'm';
    if (seconds < 86400) return Math.floor(seconds / 3600) + 'h';
    return Math.floor(seconds / 86400) + 'd';
  }
  function updateCount() {
    const open = alerts.filter((item) => item.state === 'open' || item.state === 'acked').length;
    const critical = alerts.some((item) => item.state === 'open' && item.severity === 'critical');
    const rail = document.querySelector('[data-nav="alerts"]');
    if (rail) {
      let badge = rail.querySelector('.alert-badge');
      if (!badge) { badge = document.createElement('span'); badge.className = 'alert-badge'; rail.append(badge); }
      badge.textContent = open ? String(open) : '';
      badge.hidden = !open;
      rail.classList.toggle('alert-critical', critical);
    }
    const mast = document.getElementById('alert-masthead');
    if (mast) {
      mast.hidden = !open;
      mast.textContent = open + ' need you';
      mast.classList.toggle('critical', critical);
    }
    const line = document.getElementById('overview-alert-line');
    if (line) {
      line.hidden = !open;
      line.innerHTML = open ? '<a href="#/alerts"><span class="lamp"></span>' + open + ' need you</a>' : '';
    }
  }
  function actionButton(action, alert) {
    const body = encodeURIComponent(JSON.stringify(action.body || {}));
    return '<button class="alert-action" data-alert-path="' + esc(safePath(action.path)) + '" data-alert-method="' + esc(action.method || 'POST') + '" data-alert-body="' + body + '">' + esc(action.label) + '</button>';
  }
  function card(alert) {
    const actions = (alert.actions || []).map((action) => actionButton(action, alert)).join('');
    return '<article class="alert-card ' + esc(alert.severity) + '" data-alert-id="' + esc(alert.id) + '"><div class="alert-mark" aria-label="' + esc(alert.severity) + '"></div><div class="alert-main"><div class="alert-top"><strong>' + esc(alert.title) + '</strong><span class="alert-age">' + age(alert.at) + (alert.count > 1 ? ' / ' + alert.count : '') + '</span></div><p>' + esc(alert.body) + '</p><div class="alert-meta"><span>' + esc(alert.box || 'local') + '</span><span>' + esc(alert.rule) + '</span><a href="' + esc(safeLink(alert.link)) + '">Open</a></div><div class="alert-actions">' + actions + '<button data-alert-command="ack">Ack</button><button data-alert-command="snooze">Snooze</button><button data-alert-command="resolve">Resolve</button></div></div></article>';
  }
  function renderList() {
    if (!container) return;
    const box = container.querySelector('#alerts-box-filter').value;
    const rule = container.querySelector('#alerts-rule-filter').value;
    const filtered = alerts.filter((item) => (!box || item.box === box) && (!rule || item.rule === rule));
    const list = container.querySelector('#alerts-list');
    list.innerHTML = filtered.length ? filtered.map(card).join('') : '<div class="alert-empty"><strong>Inbox clear</strong><span>Rules are active. New agent, quota, box, pressure, process, or probe issues will appear here.</span></div>';
    container.querySelector('#alerts-summary').textContent = filtered.length + ' alerts';
    container.querySelectorAll('[data-alert-command]').forEach((button) => button.addEventListener('click', () => command(button.closest('[data-alert-id]').dataset.alertId, button.dataset.alertCommand)));
    container.querySelectorAll('[data-alert-path]').forEach((button) => button.addEventListener('click', async () => {
      try {
        await api(button.dataset.alertPath, {method: button.dataset.alertMethod, headers: {'Content-Type': 'application/json'}, body: decodeURIComponent(button.dataset.alertBody)});
        await load();
      } catch (error) { container.querySelector('#alerts-status').textContent = error.message; }
    }));
  }
  async function command(id, command) {
    try {
      const body = command === 'snooze' ? JSON.stringify({until: '2h'}) : '{}';
      await api('/api/alerts/' + encodeURIComponent(id) + '/' + command, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: body});
      await load();
    } catch (error) { container.querySelector('#alerts-status').textContent = error.message; }
  }
  function filterOptions() {
    const boxes = [...new Set(alerts.map((item) => item.box).filter(Boolean))].sort();
    const rules = [...new Set(alerts.map((item) => item.rule).filter(Boolean))].sort();
    container.querySelector('#alerts-box-filter').innerHTML = '<option value="">All boxes</option>' + boxes.map((item) => '<option>' + esc(item) + '</option>').join('');
    container.querySelector('#alerts-rule-filter').innerHTML = '<option value="">All rules</option>' + rules.map((item) => '<option>' + esc(item) + '</option>').join('');
  }
  function renderRules() {
    const rules = config && config.rules || {};
    const names = ['agent_needs_you','agent_stuck','agent_done','agent_limit','quota_high','box_unreachable','box_pressure','process_died','probe_failed'];
    container.querySelector('#alerts-panel').innerHTML = '<div class="alert-rule-list">' + names.map((name) => '<label class="alert-rule-row"><span><strong>' + name + '</strong><small>Built in alert rule</small></span><input type="checkbox" data-rule="' + name + '"' + (rules[name] ? ' checked' : '') + '></label>').join('') + '</div><div class="alert-form-row"><label>Stuck minutes <input id="rule-stuck" type="number" min="1" value="' + esc(rules.stuckMinutes || 10) + '"></label><label>Quota percent <input id="rule-quota" type="number" min="1" max="100" value="' + esc(rules.quotaPercent || 90) + '"></label></div><button id="rules-save">Save rules</button><p id="alerts-status" class="alert-status"></p>';
    container.querySelector('#rules-save').onclick = async () => {
      const next = Object.assign({}, rules);
      container.querySelectorAll('[data-rule]').forEach((input) => { next[input.dataset.rule] = input.checked; });
      next.stuckMinutes = Number(container.querySelector('#rule-stuck').value);
      next.quotaPercent = Number(container.querySelector('#rule-quota').value);
      try { await api('/api/alerts/rules', {method: 'PUT', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({rules: next, quiet: config.quiet, quietAllowCritical: config.quietAllowCritical})}); await loadRules(); container.querySelector('#alerts-status').textContent = 'Rules saved'; } catch (error) { container.querySelector('#alerts-status').textContent = error.message; }
    };
  }
  function sinkLabel(sink) {
    if (sink.type === 'apns') return 'Native push';
    if (sink.type === 'webpush') return 'Browser push';
    if (sink.type === 'ntfy') return 'ntfy, optional if you do not want to run the iOS app';
    return sink.type;
  }
  function renderDelivery() {
    const order = {apns: 0, webpush: 1};
    const sinks = (config && config.sinks || []).slice().sort((left, right) => (order[left.type] ?? 9) - (order[right.type] ?? 9));
    const delivery = config && config.delivery || {};
    const rows = sinks.length ? sinks.map((sink) => { const name = sink.name || sink.type; return '<div class="alert-delivery-row"><span><strong>' + esc(sinkLabel(sink)) + '</strong><small>' + esc(name) + '</small></span><span class="alert-delivery-result">' + esc(delivery[name]?.status || 'not tested') + '</span><button data-test-sink="' + esc(name) + '">Test</button></div>'; }).join('') : '<div class="alert-empty">No delivery sinks are enabled. Turn on browser push below, configure native push in Settings, or ntfy if you do not want to run the iOS app.</div>';
    container.querySelector('#alerts-panel').innerHTML = '<div class="alert-delivery-list">' + rows + '</div><section id="webpush-block" class="alert-webpush" aria-live="polite"></section><div class="alert-quiet"><strong>Quiet hours</strong><span>' + esc((config.quiet && config.quiet.from) || 'not set') + ' to ' + esc((config.quiet && config.quiet.to) || 'not set') + '</span><button id="disarm-alerts">' + (config.disarmed ? 'Turn alerts on' : 'Disarm alerts') + '</button></div><p id="alerts-status" class="alert-status"></p>';
    container.querySelectorAll('[data-test-sink]').forEach((button) => button.onclick = async () => { try { await api('/api/alerts/sinks/test?name=' + encodeURIComponent(button.dataset.testSink)); await loadRules(); } catch (error) { container.querySelector('#alerts-status').textContent = error.message; } });
    container.querySelector('#disarm-alerts').onclick = async () => { try { await api('/api/alerts/disarm', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({on: !config.disarmed})}); await loadRules(); } catch (error) { container.querySelector('#alerts-status').textContent = error.message; } };
    renderWebPush();
  }

  // Browser push. The deck signs with its own VAPID key, so no third party account is needed.
  // Push only works from a secure context: localhost, or https behind Tailscale or a proxy.
  const webPush = {
    supported() { return Boolean(window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window); },
    async registration() { return navigator.serviceWorker.register('/sw.js', {scope: '/'}); },
    async current() {
      if (!this.supported()) return null;
      const registration = await navigator.serviceWorker.getRegistration('/');
      return registration ? registration.pushManager.getSubscription() : null;
    },
    async subscribe(publicKey) {
      const permission = await Notification.requestPermission();
      if (permission !== 'granted') throw new Error('Notifications are blocked for this site. Allow them in the browser site settings and try again.');
      const registration = await this.registration();
      await navigator.serviceWorker.ready;
      const subscription = await registration.pushManager.subscribe({userVisibleOnly: true, applicationServerKey: publicKey});
      await api('/api/push/web/subscribe', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({subscription: subscription.toJSON(), name: ''})});
      return subscription;
    },
    async unsubscribe() {
      const subscription = await this.current();
      if (!subscription) return;
      await api('/api/push/web/subscribe', {method: 'DELETE', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({endpoint: subscription.endpoint})}).catch(() => {});
      await subscription.unsubscribe();
    }
  };
  // Same id the server derives: the first 16 characters of base64url(sha256(endpoint)).
  async function endpointID(endpoint) {
    if (!window.crypto || !crypto.subtle) return '';
    const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(endpoint));
    return btoa(String.fromCharCode.apply(null, new Uint8Array(digest))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '').slice(0, 16);
  }
  function webPushStatusText() {
    if (!window.isSecureContext) return 'Browser push needs a secure page. Open the deck at http://localhost:' + (location.port || '8100') + ' on the box, or over https behind Tailscale or a proxy.';
    if (!('serviceWorker' in navigator) || !('PushManager' in window)) return 'This browser does not support Web Push.';
    if (window.Notification && Notification.permission === 'denied') return 'Notifications are blocked for this site. Allow them in the browser site settings to get alerts here.';
    return '';
  }
  async function renderWebPush() {
    const block = container && container.querySelector('#webpush-block');
    if (!block) return;
    let status = null;
    let mine = null;
    try { status = await api('/api/push/web'); } catch (error) { block.innerHTML = '<div class="alert-empty">' + esc(error.message) + '</div>'; return; }
    try { mine = await webPush.current(); } catch (error) { mine = null; }
    if (!block.isConnected) return;
    const subscriptions = status.subscriptions || [];
    const mineID = mine ? await endpointID(mine.endpoint) : '';
    const blocked = webPushStatusText();
    const head = '<div class="alert-webpush-head"><span><strong>Browser push</strong><small>' + (mine ? 'This device gets alerts, even with the deck closed.' : 'Get alerts on this device without the iOS app or ntfy.') + '</small></span>' + (blocked ? '' : mine ? '<span class="alert-webpush-buttons"><button id="webpush-test">Send test</button><button id="webpush-stop" class="button-quiet">Stop</button></span>' : '<button id="webpush-start" class="button-primary">Get alerts on this device</button>') + '</div>';
    const note = blocked ? '<p class="alert-webpush-note">' + esc(blocked) + '</p>' : '';
    const rows = subscriptions.length ? '<div class="alert-webpush-list">' + subscriptions.map((item) => '<div class="alert-delivery-row"><span><strong>' + esc(item.name || 'Browser') + (mineID && item.id === mineID ? ' <em>this device</em>' : '') + '</strong><small>' + esc(item.host || '') + '</small></span><span class="alert-delivery-result">' + esc(item.lastDelivery && item.lastDelivery.status ? item.lastDelivery.status + (item.lastDelivery.message ? ', ' + item.lastDelivery.message : '') : 'no delivery yet') + '</span><button data-webpush-remove="' + esc(item.id) + '" class="button-quiet">Forget</button></div>').join('') + '</div>' : '<p class="alert-webpush-note">No browser is subscribed yet.</p>';
    block.innerHTML = head + note + rows;
    const say = (text) => { const line = container.querySelector('#alerts-status'); if (line) line.textContent = text; };
    const start = block.querySelector('#webpush-start');
    if (start) start.onclick = async () => { start.disabled = true; try { await webPush.subscribe(status.publicKey); say('This device now gets alerts'); await loadRules(); } catch (error) { say(error.message); start.disabled = false; } };
    const stop = block.querySelector('#webpush-stop');
    if (stop) stop.onclick = async () => { stop.disabled = true; try { await webPush.unsubscribe(); say('This device no longer gets alerts'); await loadRules(); } catch (error) { say(error.message); stop.disabled = false; } };
    const test = block.querySelector('#webpush-test');
    if (test) test.onclick = async () => { test.disabled = true; try { const result = await api('/api/push/web/test', {method: 'POST'}); const failed = (result.results || []).filter((item) => item.status !== 'delivered'); say(failed.length ? failed.map((item) => item.message).join('; ') : 'Test sent'); await loadRules(); } catch (error) { say(error.message); } test.disabled = false; };
    block.querySelectorAll('[data-webpush-remove]').forEach((button) => button.onclick = async () => { try { await api('/api/push/web/subscribe', {method: 'DELETE', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({id: button.dataset.webpushRemove})}); if (mine && mineID && button.dataset.webpushRemove === mineID) await mine.unsubscribe().catch(() => {}); await loadRules(); } catch (error) { say(error.message); } });
  }
  async function loadRules() { config = await api('/api/alerts/rules'); if (mode === 'rules') renderRules(); if (mode === 'delivery') renderDelivery(); }
  async function load() {
    try {
      if (!container.querySelector('#alerts-list')) container.querySelector('#alerts-panel').innerHTML = '<div id="alerts-list"></div>';
      alerts = await api('/api/alerts?state=' + encodeURIComponent(mode === 'open' ? 'open' : mode));
      filterOptions(); renderList(); updateCount();
    } catch (error) {
      const list = container && container.querySelector('#alerts-list');
      if (list) list.innerHTML = '<div class="alert-empty">' + esc(error.message) + '</div>';
    }
  }
  function render(target) {
    container = target;
    mode = 'open';
    target.innerHTML = '<section class="alerts-view"><div class="alerts-head"><div><p class="eyebrow">SENTRY</p><p class="view-subtitle">Quiet inbox for agents, boxes, quotas, and probes.</p></div><span id="alerts-summary" class="mono"></span></div><nav class="alert-tabs" aria-label="Alert sections"><button data-alert-tab="open" class="button-primary">Open</button><button data-alert-tab="snoozed">Snoozed</button><button data-alert-tab="resolved">Resolved</button><button data-alert-tab="rules">Rules</button><button data-alert-tab="delivery">Delivery</button></nav><div id="alerts-filters" class="alert-filters"><select id="alerts-box-filter" aria-label="Filter alerts by box"></select><select id="alerts-rule-filter" aria-label="Filter alerts by rule"></select><button id="alerts-snooze-all" class="button-quiet">Snooze all 2h</button></div><div id="alerts-panel"><div id="alerts-list"></div></div></section>';
    target.querySelectorAll('[data-alert-tab]').forEach((button) => button.onclick = async () => { mode = button.dataset.alertTab; target.querySelectorAll('[data-alert-tab]').forEach((item) => item.classList.toggle('selected', item === button)); target.querySelector('#alerts-filters').hidden = mode === 'rules' || mode === 'delivery'; if (mode === 'rules' || mode === 'delivery') { await loadRules(); } else { await load(); } });
    target.querySelector('#alerts-box-filter').onchange = renderList;
    target.querySelector('#alerts-rule-filter').onchange = renderList;
    target.querySelector('#alerts-snooze-all').onclick = async () => { await api('/api/alerts/snooze-all', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({until: '2h'})}); await load(); };
    target.querySelector('[data-alert-tab="open"]').classList.add('selected');
    load();
  }
  addView('alerts', 'Alerts', render);
  setInterval(() => { if (mode === 'open' && location.hash.indexOf('#/alerts') === 0) load(); else { fetch('/api/alerts?state=open', {cache: 'no-store'}).then((response) => response.json()).then((value) => { alerts = value; updateCount(); }).catch(() => {}); } }, 5000);
  if (location.hash.indexOf('#/alerts') === 0) window.dispatchEvent(new Event('hashchange'));
}());

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
    list.innerHTML = filtered.length ? filtered.map(card).join('') : '<div class="alert-empty">Quiet. Nothing needs you. Rules are on: agent needs you, stuck, quota, box down.</div>';
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
  function renderDelivery() {
    const sinks = config && config.sinks || [];
    const delivery = config && config.delivery || {};
    container.querySelector('#alerts-panel').innerHTML = '<div class="alert-delivery-list">' + (sinks.length ? sinks.map((sink) => '<div class="alert-delivery-row"><span><strong>' + esc(sink.name || sink.type) + '</strong><small>' + esc(sink.type) + '</small></span><span class="alert-delivery-result">' + esc(delivery[sink.name || sink.type]?.status || 'not tested') + '</span><button data-test-sink="' + esc(sink.name || sink.type) + '">Test</button></div>').join('') : '<div class="alert-empty">No delivery sinks are enabled. Notifications are off by default.</div>') + '</div><div class="alert-quiet"><strong>Quiet hours</strong><span>' + esc((config.quiet && config.quiet.from) || 'not set') + ' to ' + esc((config.quiet && config.quiet.to) || 'not set') + '</span><button id="disarm-alerts">' + (config.disarmed ? 'Turn alerts on' : 'Disarm alerts') + '</button></div><p id="alerts-status" class="alert-status"></p>';
    container.querySelectorAll('[data-test-sink]').forEach((button) => button.onclick = async () => { try { await api('/api/alerts/sinks/test?name=' + encodeURIComponent(button.dataset.testSink)); await loadRules(); } catch (error) { container.querySelector('#alerts-status').textContent = error.message; } });
    container.querySelector('#disarm-alerts').onclick = async () => { try { await api('/api/alerts/disarm', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({on: !config.disarmed})}); await loadRules(); } catch (error) { container.querySelector('#alerts-status').textContent = error.message; } };
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
    target.innerHTML = '<section class="alerts-view"><div class="alerts-head"><div><p class="eyebrow">SENTRY</p><h2>Alerts</h2><p class="alert-lede">A quiet inbox for agents, boxes, quotas and probes.</p></div><span id="alerts-summary" class="mono"></span></div><nav class="alert-tabs" aria-label="Alert sections"><button data-alert-tab="open">Open</button><button data-alert-tab="snoozed">Snoozed</button><button data-alert-tab="resolved">Resolved</button><button data-alert-tab="rules">Rules</button><button data-alert-tab="delivery">Delivery</button></nav><div id="alerts-filters" class="alert-filters"><select id="alerts-box-filter" aria-label="Filter alerts by box"></select><select id="alerts-rule-filter" aria-label="Filter alerts by rule"></select><button id="alerts-snooze-all">Snooze all 2h</button></div><div id="alerts-panel"><div id="alerts-list"></div></div></section>';
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

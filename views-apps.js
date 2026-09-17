(function () {
  'use strict';

  var safe = function (value) { return String(value == null ? '' : value).replace(/[\u00b7\u2013\u2014]/g, ' / '); };
  var esc = function (value) { return safe(value).replace(/[&<>"']/g, function (char) { return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]; }); };
  var node = function (id) { return document.getElementById(id); };
  var apps = [];
  var currentContainer = null;
  var refreshTimer = 0;

  async function json(path, options) {
    var response = await fetch(path, Object.assign({ cache: 'no-store' }, options || {}));
    if (response.status === 401) {
      location.href = '/login?next=' + encodeURIComponent('/' + location.hash);
      throw new Error('Sign in to continue');
    }
    var value = await response.json();
    if (!response.ok) throw new Error(value.error || 'Request failed');
    return value;
  }

  function setStatus(message, attention) {
    var status = (currentContainer && currentContainer.querySelector('[data-app-status]')) || node('action-status');
    if (!status) return;
    status.textContent = safe(message);
    status.classList.toggle('attention', !!attention);
  }

  function openTarget(app) { return app.openPath || app.url || ''; }

  function targetHTML(app, label) {
    var target = openTarget(app);
    if (!target) return '';
    if (app.openPath || !app.embed) {
      return '<a class="apps-primary" href="' + esc(target) + '"' + (app.openPath ? '' : ' target="_blank" rel="noopener"') + '>' + label + '</a>';
    }
    return '<button class="apps-primary" data-app-open="' + esc(target) + '">' + label + '</button>';
  }

  function primaryHTML(app) {
    if (app.running) return targetHTML(app, 'Open');
    if (app.detected && app.canStart) return '<button class="apps-primary" data-app-action="start" data-app-id="' + esc(app.id) + '">Start</button>';
    if (app.detected && openTarget(app)) return targetHTML(app, 'Open');
    if (!app.detected && app.installHint) return '<button class="apps-primary apps-install" data-copy-install="' + esc(app.installHint) + '">Install hint</button>';
    return '<span class="apps-primary apps-disabled">Unavailable</span>';
  }

  function statusHTML(app) {
    var label = app.status === 'running' ? 'running' : app.status === 'detected' ? 'detected' : 'missing';
    return '<span class="apps-status apps-status-' + esc(app.status) + '"><i></i>' + label + '</span>';
  }

  function cardHTML(app) {
    var badge = safe(app.name).replace(/[^a-zA-Z0-9]/g, '').slice(0, 2).toUpperCase() || 'AP';
    var details = app.message ? '<p class="apps-message">' + esc(app.message) + '</p>' : '';
    if (app.id === 'herdr') details += '<p class="apps-message">Reach herdr through <a href="#/terminal">Terminal</a> and <a href="#/agents">Agents</a>.</p>';
    var install = !app.detected && app.installHint ? '<p class="apps-install-copy">' + esc(app.installHint) + '</p>' : '';
    var link = app.docs ? '<a href="' + esc(app.docs) + '" target="_blank" rel="noopener">Docs</a>' : '';
    var meta = app.running && app.pid ? '<span class="mono">PID ' + app.pid + '</span>' : app.port ? '<span class="mono">port ' + app.port + '</span>' : '<span>ready on this box</span>';
    return '<article class="apps-card" data-app-card="' + esc(app.id) + '">' +
      '<header class="apps-card-head"><span class="apps-badge" aria-hidden="true">' + esc(badge) + '</span><div class="apps-card-title"><h3>' + esc(app.name) + '</h3><p>' + esc(app.tag || 'box tool') + '</p></div>' + statusHTML(app) + '</header>' +
      '<div class="apps-card-meta">' + meta + (app.health ? '<span class="apps-health">healthy</span>' : '') + '</div>' +
      details + install +
      '<div class="apps-card-actions">' + primaryHTML(app) + '<details class="apps-menu"><summary>More</summary><div class="apps-menu-pop"><button data-app-action="stop" data-app-id="' + esc(app.id) + '"' + (app.canStop ? '' : ' disabled') + '>Stop</button><button data-app-action="restart" data-app-id="' + esc(app.id) + '"' + (app.canStart ? '' : ' disabled') + '>Restart</button><button data-app-log="' + esc(app.id) + '">Log</button>' + (app.installHint ? '<button data-copy-install="' + esc(app.installHint) + '">Copy install hint</button>' : '') + link + '</div></details></div>' +
      '<div class="apps-log" data-app-log-drawer="' + esc(app.id) + '" hidden><div class="apps-log-head"><span>Last log lines</span><button data-close-log="' + esc(app.id) + '">Close</button></div><pre data-app-log-body></pre></div>' +
      '</article>';
  }

  function renderStrip() {
    var overview = node('overview-view');
    var berth = overview && overview.querySelector('section');
    if (!berth) return;
    var strip = node('overview-apps');
    if (!strip) {
      strip = document.createElement('section');
      strip.id = 'overview-apps';
      strip.className = 'apps-overview-strip';
      berth.insertAdjacentElement('afterend', strip);
    }
    var running = apps.filter(function (app) { return app.running; });
    var pills = running.map(function (app) {
      var target = openTarget(app);
      return target ? '<a href="' + esc(target) + '"' + (app.openPath ? '' : ' target="_blank" rel="noopener"') + '>' + esc(app.name) + '</a>' : '';
    }).join('');
    strip.innerHTML = '<div class="section-top"><h2>Apps <small>' + running.length + ' running</small></h2><a href="#/apps">All apps</a></div><div class="apps-pills">' + (pills || '<span class="apps-strip-empty">No managed apps running</span>') + '</div>';
  }

  function renderCards(container) {
    var grid = container.querySelector('[data-app-grid]');
    if (!grid) return;
    var rank = { running: 0, detected: 1, missing: 2 };
    var ordered = apps.slice().sort(function (a, b) { return (rank[a.status] - rank[b.status]) || a.name.localeCompare(b.name); });
    grid.innerHTML = ordered.map(cardHTML).join('');
    container.querySelector('[data-app-count]').textContent = apps.length + ' recipes';
    var builtin = ['terminal', 'files', 'browser', 't3code', 'herdr', 'jev', 'code-server', 'filebrowser', 'ollama', 'syncthing'];
    var custom = apps.filter(function (app) { return builtin.indexOf(app.id) < 0; });
    container.querySelector('[data-app-custom]').hidden = custom.length > 0;
    renderStrip();
  }

  async function loadApps(container) {
    try {
      apps = await json('/api/apps');
      renderCards(container);
      setStatus('');
    } catch (error) {
      setStatus(error.message || 'Could not read apps', true);
    }
  }

  async function doAction(id, action) {
    var app = apps.find(function (item) { return item.id === id; });
    setStatus(action.charAt(0).toUpperCase() + action.slice(1) + ' ' + (app ? app.name : id) + '...');
    try {
      if (id === 'browser' && (action === 'start' || action === 'stop')) {
        await json('/api/browser/' + action, { method: 'POST' });
      } else {
        await json('/api/apps/' + encodeURIComponent(id) + '/' + action, { method: 'POST' });
      }
      await loadApps(currentContainer);
      setStatus((app ? app.name : id) + ' ' + action + ' complete.');
    } catch (error) {
      setStatus(error.message || 'App action failed', true);
    }
  }

  async function showLog(id) {
    var card = currentContainer && currentContainer.querySelector('[data-app-card="' + CSS.escape(id) + '"]');
    var drawer = card && card.querySelector('[data-app-log-drawer="' + CSS.escape(id) + '"]');
    if (!drawer) return;
    drawer.hidden = false;
    var body = drawer.querySelector('[data-app-log-body]');
    body.textContent = 'Reading log...';
    try {
      var value = await json('/api/apps/' + encodeURIComponent(id) + '/log?lines=200');
      body.textContent = (value.lines || []).join('\n') || 'No log output yet.';
    } catch (error) {
      body.textContent = error.message || 'Could not read log.';
    }
  }

  async function reloadRecipes() {
    setStatus('Reloading recipes...');
    try {
      var value = await json('/api/apps/reload', { method: 'POST' });
      apps = value.apps || [];
      renderCards(currentContainer);
      setStatus(value.error || 'Recipes reloaded.');
    } catch (error) {
      setStatus(error.message || 'Could not reload recipes.', true);
    }
  }

  function installEvents(container) {
    container.addEventListener('click', function (event) {
      var action = event.target.closest('[data-app-action]');
      if (action) {
        event.preventDefault();
        doAction(action.dataset.appId, action.dataset.appAction);
        return;
      }
      var log = event.target.closest('[data-app-log]');
      if (log) {
        event.preventDefault();
        showLog(log.dataset.appLog);
        return;
      }
      var close = event.target.closest('[data-close-log]');
      if (close) {
        var drawer = close.closest('.apps-log');
        if (drawer) drawer.hidden = true;
        return;
      }
      var copy = event.target.closest('[data-copy-install]');
      if (copy) {
        event.preventDefault();
        var value = copy.dataset.copyInstall;
        if (navigator.clipboard) navigator.clipboard.writeText(value).then(function () { setStatus('Install hint copied.'); }).catch(function () { setStatus(value); });
        else setStatus(value);
        return;
      }
      var open = event.target.closest('[data-app-open]');
      if (open) {
        event.preventDefault();
        if (typeof window.openViewer === 'function') window.openViewer(open.dataset.appOpen, true);
        else window.open(open.dataset.appOpen, '_blank', 'noopener');
      }
    });
    var reload = container.querySelector('[data-app-reload]');
    if (reload) reload.addEventListener('click', reloadRecipes);
  }

  function renderApps(container) {
    currentContainer = container;
    if (!container.dataset.ready) {
      container.dataset.ready = '1';
      container.innerHTML = '<div class="apps-view-head"><div><h2>Apps <small>tools on this box</small></h2><p>Start a tool here, open it on any device, or add another recipe to your box.</p></div><div class="apps-view-actions"><button data-app-reload>Reload recipes</button><span class="view-summary" data-app-count></span><span class="notice" data-app-status role="status"></span></div></div><div class="apps-grid" data-app-grid></div><section class="apps-custom" data-app-custom><h2>Your recipes</h2><p>Drop a JSON recipe into <code>~/.config/boxdeck/apps</code>, then reload recipes.</p></section>';
      installEvents(container);
    }
    loadApps(container);
    clearInterval(refreshTimer);
    refreshTimer = setInterval(function () { if (!document.hidden) loadApps(container); }, 10000);
  }

  function genericIcon() {
    return '<svg class="nav-icon" viewBox="0 0 22 24" aria-hidden="true"><path d="M4 4h14v16H4z M8 8h6 M8 12h6 M8 16h4"></path></svg>';
  }

  function fallbackAddView() {
    var registry = new Map();
    var addView = function (id, label, render) {
      if (registry.has(id)) return;
      registry.set(id, { id: id, label: label, render: render });
      if (typeof nav !== 'undefined' && !nav.some(function (item) { return item[0] === id; })) nav.push([id, label, 'M4 4h14v16H4z']);
      var rail = node('rail-nav');
      if (rail && !rail.querySelector('[data-nav="' + CSS.escape(id) + '"]')) {
        var link = document.createElement('a');
        link.href = '#/' + id;
        link.dataset.nav = id;
        link.title = label;
        link.innerHTML = genericIcon() + '<span class="nav-label">' + esc(label) + '</span>';
        var files = rail.querySelector('[data-nav="files"]');
        rail.insertBefore(link, files || null);
      }
      var more = node('more-nav');
      if (more && !more.querySelector('a[href="#/' + CSS.escape(id) + '"]')) {
        var moreLink = document.createElement('a');
        moreLink.href = '#/' + id;
        moreLink.textContent = label;
        more.append(moreLink);
      }
      var shell = document.createElement('div');
      shell.id = id + '-view';
      shell.className = 'view apps-view';
      shell.hidden = true;
      var actionStatus = node('action-status');
      if (actionStatus && actionStatus.parentElement) actionStatus.parentElement.insertBefore(shell, actionStatus);
      render(shell);
    };
    window.boxdeck = { addView: addView };
    window.addEventListener('hashchange', function () {
      var id = location.hash.replace(/^#\/?/, '').split('?')[0];
      var entry = registry.get(id);
      if (!entry) return;
      document.querySelectorAll('.view').forEach(function (view) { view.hidden = view.id !== id + '-view'; });
      document.querySelectorAll('[data-nav]').forEach(function (link) { link.toggleAttribute('aria-current', link.dataset.nav === id); });
      node('view-title').textContent = entry.label;
      ['overview-quick', 'live-band', 'toolbar'].forEach(function (part) { if (node(part)) node(part).hidden = true; });
      entry.render(node(id + '-view'));
    });
    return addView;
  }

  var addView = (window.boxdeck && window.boxdeck.addView) || fallbackAddView();
  addView('apps', 'Apps', renderApps);
  var appsLink = node('rail-nav') && node('rail-nav').querySelector('[data-nav="apps"]');
  var filesLink = node('rail-nav') && node('rail-nav').querySelector('[data-nav="files"]');
  if (appsLink && filesLink) filesLink.before(appsLink);
  if (location.hash === '#/apps') window.dispatchEvent(new HashChangeEvent('hashchange'));
}());

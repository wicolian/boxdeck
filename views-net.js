/*
 * Shared view hook for independent workers. addView inserts a rail item after
 * Docker, adds the view shell and lets each view own its fetch and rendering.
 */
(function () {
  'use strict';

  const registry = new Map();
  const expanded = new Set();
  const tails = new Map();
  const shots = new Map();
  let herdAgents = [];
  let herdTimer = 0;
  let herdLoading = false;
  let current = '';
  let browserScreenSocket = null;
  let browserScreenPage = '';
  let browserScreenCanvas = null;
  let browserScreenLastImage = 0;
  let browserStopArmed = false;

  const $n = (id) => document.getElementById(id);
  const safe = (value) => String(value ?? '').replace(/[\u00b7\u2013\u2014]/g, ' / ');
  const escn = (value) => safe(value).replace(/[&<>"']/g, (char) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
  }[char]));
  const paneOf = (agent) => agent.pane_id || agent.pane || '';
  const needsYou = (agent, tail) => agent.agent_status === 'waiting' || /\(\s*y\s*\/\s*n\s*\)|press\s+(enter|return)|\btrust\b/i.test(tail || '');

  async function json(path, options) {
    const response = await fetch(path, Object.assign({ cache: 'no-store' }, options || {}));
    if (response.status === 401) {
      location.href = '/login?next=' + encodeURIComponent('/' + location.hash);
      throw new Error('Sign in to continue');
    }
    const value = await response.json();
    if (!response.ok) throw new Error(value.error || 'Request failed');
    return value;
  }

  function netNavIcon(id) {
    const paths = {
      herd: 'M4 7h16M4 12h16M4 17h16 M8 4v16',
      browser: 'M3 5h18v14H3z M3 9h18 M7 7h.01 M10 7h.01',
      network: 'M12 4v5 M5 18h14 M7 18v-4h10v4 M5 18l-2 2 M19 18l2 2 M12 9l-7 5 M12 9l7 5'
    };
    return `<svg class="nav-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="${paths[id] || paths.network}"></path></svg>`;
  }

  function addView(id, label, render) {
    if (!id || registry.has(id)) return;
    const entry = { id, label, render };
    registry.set(id, entry);
    const rail = document.getElementById('rail-nav');
    const existingRail = rail && rail.querySelector('[data-nav="' + id + '"]');
    if (id !== 'herd' && rail && !existingRail) {
      const link = document.createElement('a');
      link.href = '#/' + id;
      link.dataset.nav = id;
      link.title = label;
      link.innerHTML = netNavIcon(id) + `<span class="nav-label">${escn(label)}</span>`;
      const group = id === 'alerts' ? 'watch' : id === 'herd' ? 'watch' : id === 'browser' || id === 'network' ? 'fleet' : 'fleet';
      const target = rail.querySelector('[data-nav-group-links="' + group + '"]');
      if (target) target.append(link);
    }
    const more = document.getElementById('more-nav');
    if (id !== 'herd' && more && !more.querySelector('a[href="#/' + id + '"]')) {
      const moreLink = document.createElement('a');
      moreLink.href = '#/' + id;
      moreLink.textContent = label;
      const group = id === 'alerts' ? 'watch' : id === 'herd' ? 'watch' : id === 'browser' || id === 'network' ? 'fleet' : 'fleet';
      const target = more.querySelector('[data-nav-group-links="' + group + '"]');
      if (target) target.append(moreLink);
    }
    const shell = document.createElement('div');
    shell.id = id + '-view';
    shell.className = 'view net-view';
    shell.hidden = true;
    const action = document.getElementById('action-status');
    action?.parentElement?.insertBefore(shell, action);
    render(shell);
  }

  // Other independent view workers can register without editing index.html.
  window.boxdeck = { addView };

  function setStatus(message, attention) {
    const node = $n('action-status');
    if (!node) return;
    node.textContent = safe(message);
    node.classList.toggle('attention', !!attention);
  }

  function showOnly(id) {
    document.querySelectorAll('.view').forEach((view) => { view.hidden = view.id !== id + '-view'; });
    document.querySelectorAll('[data-nav]').forEach((link) => {
      if (link.dataset.nav === id) link.setAttribute('aria-current', 'page');
      else link.removeAttribute('aria-current');
    });
    ['overview-focus', 'overview-quick', 'live-band', 'toolbar'].forEach((name) => { if ($n(name)) $n(name).hidden = true; });
    if ($n('view-title')) $n('view-title').textContent = registry.get(id).label;
    document.title = registry.get(id).label + ' / boxdeck';
  }

  function route() {
    const id = location.hash.replace(/^#\/?/, '').split('?')[0];
    if (!registry.has(id)) {
      closeBrowserScreen();
      clearInterval(herdTimer);
      return;
    }
    current = id;
    showOnly(id);
    registry.get(id).render($n(id + '-view'));
  }

  function closeBrowserScreen() {
    if (browserScreenSocket) {
      browserScreenSocket.close();
      browserScreenSocket = null;
    }
    browserScreenPage = '';
    browserScreenCanvas = null;
  }

  // CDP ids must fit a 32 bit int; Date.now() does not, and Chrome answers with an error instead of acting.
  let browserScreenSeq = 100;
  function browserScreenSend(message) {
    if (message && (message.id === undefined || message.id > 2147483647)) message.id = ++browserScreenSeq;
    if (browserScreenSocket?.readyState === WebSocket.OPEN) browserScreenSocket.send(JSON.stringify(message));
  }

  function connectBrowserScreen(page) {
    if (!page || browserScreenPage === page && browserScreenSocket) return;
    closeBrowserScreen();
    browserScreenPage = page;
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    browserScreenSocket = new WebSocket(protocol + '//' + location.host + '/api/browser/screen?page=' + encodeURIComponent(page));
    const canvas = $n('browser-canvas');
    browserScreenCanvas = canvas;
    if (!canvas) return;
    const context = canvas.getContext('2d');
    browserScreenSocket.onopen = () => {
      $n('browser-screen-status').textContent = 'Watching live';
      browserScreenSend({ method: 'Page.enable', params: {} });
    };
    // Chrome drops the screencast on a cross-process navigation (new tab page to a site, for
    // example), so restart it whenever the frame navigates or frames go quiet.
    const restartScreencast = () => browserScreenSend({ method: 'Page.startScreencast', params: { format: 'jpeg', quality: 60, maxWidth: 1280, everyNthFrame: 1 } });
    const quietTimer = setInterval(() => {
      if (browserScreenSocket?.readyState !== WebSocket.OPEN) { clearInterval(quietTimer); return; }
      if (Date.now() - browserScreenLastImage > 3000) restartScreencast();
    }, 2000);
    browserScreenSocket.onclose = () => {
      const wanted = browserScreenPage;
      browserScreenSocket = null;
      if ($n('browser-screen-status')) $n('browser-screen-status').textContent = 'Reconnecting screen';
      if (current === 'browser' && wanted) setTimeout(() => { if (current === 'browser' && browserScreenPage === wanted) connectBrowserScreen(wanted); }, 500);
    };
    browserScreenSocket.onerror = () => { if ($n('browser-screen-status')) $n('browser-screen-status').textContent = 'Screen unavailable'; };
    browserScreenSocket.onmessage = async (event) => {
      let message;
      try { message = JSON.parse(event.data); } catch { return; }
      if (message.method === 'Page.frameNavigated' && !message.params?.frame?.parentId) { restartScreencast(); return; }
      if (message.method !== 'Page.screencastFrame' || !message.params?.data) return;
      const now = Date.now();
      if (now - browserScreenLastImage < 100) return;
      browserScreenLastImage = now;
      const image = new Image();
      image.onload = () => {
        if (!browserScreenCanvas || !context) return;
        browserScreenCanvas.width = image.naturalWidth;
        browserScreenCanvas.height = image.naturalHeight;
        context.drawImage(image, 0, 0);
      };
      image.src = 'data:image/jpeg;base64,' + message.params.data;
    };
  }

  function screenPoint(event) {
    const canvas = browserScreenCanvas;
    if (!canvas) return null;
    const box = canvas.getBoundingClientRect();
    return { x: Math.round((event.clientX - box.left) * canvas.width / Math.max(1, box.width)), y: Math.round((event.clientY - box.top) * canvas.height / Math.max(1, box.height)) };
  }

  function sendMouse(event, type, button) {
    const point = screenPoint(event);
    if (!point) return;
    browserScreenSend({ id: Date.now(), method: 'Input.dispatchMouseEvent', params: { type, x: point.x, y: point.y, button: button || 'none', clickCount: type === 'mousePressed' ? 1 : 0, buttons: event.buttons || 0 } });
  }

  function installBrowserInput(container) {
    const canvas = $n('browser-canvas');
    if (!canvas || canvas.dataset.inputReady) return;
    canvas.dataset.inputReady = '1';
    canvas.tabIndex = 0;
    canvas.addEventListener('pointerdown', (event) => { canvas.setPointerCapture?.(event.pointerId); sendMouse(event, 'mousePressed', event.button === 2 ? 'right' : 'left'); });
    canvas.addEventListener('pointerup', (event) => sendMouse(event, 'mouseReleased', event.button === 2 ? 'right' : 'left'));
    canvas.addEventListener('pointermove', (event) => sendMouse(event, 'mouseMoved'));
    canvas.addEventListener('wheel', (event) => { event.preventDefault(); browserScreenSend({ id: Date.now(), method: 'Input.dispatchMouseEvent', params: { type: 'mouseWheel', x: screenPoint(event)?.x || 0, y: screenPoint(event)?.y || 0, deltaX: event.deltaX, deltaY: event.deltaY } }); }, { passive: false });
    canvas.addEventListener('keydown', (event) => {
      event.preventDefault();
      const text = event.key.length === 1 && !event.ctrlKey && !event.metaKey ? event.key : undefined;
      browserScreenSend({ id: Date.now(), method: 'Input.dispatchKeyEvent', params: { type: 'keyDown', key: event.key, code: event.code, text, modifiers: (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) | (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0) } });
    });
    canvas.addEventListener('keyup', (event) => { event.preventDefault(); browserScreenSend({ id: Date.now(), method: 'Input.dispatchKeyEvent', params: { type: 'keyUp', key: event.key, code: event.code } }); });
    const keyboard = $n('browser-keyboard');
    keyboard?.addEventListener('input', () => { if (keyboard.value) browserScreenSend({ id: Date.now(), method: 'Input.insertText', params: { text: keyboard.value } }); keyboard.value = ''; });
    container.addEventListener('paste', (event) => { const text = event.clipboardData?.getData('text'); if (text) { event.preventDefault(); browserScreenSend({ id: Date.now(), method: 'Input.insertText', params: { text } }); } });
  }

  function actionButton(pane, action, label) {
    return `<button class="net-action" data-herd-action="${action}" data-pane="${escn(pane)}">${label}</button>`;
  }

  function herdRows() {
    const rows = herdAgents.map((agent) => {
      const pane = paneOf(agent);
      const tail = tails.get(pane)?.text || '';
      return { agent, pane, tail, needs: needsYou(agent, tail) };
    }).filter((row) => row.pane);
    rows.sort((a, b) => Number(b.needs) - Number(a.needs) || String(a.agent.terminal_title_stripped || a.agent.cwd).localeCompare(String(b.agent.terminal_title_stripped || b.agent.cwd)));
    return rows;
  }

  function renderHerdRows(container) {
    const rows = herdRows();
    if (!rows.length) {
      container.innerHTML = '<div class="net-empty">No agent panes are visible. Start herdr or tmux on the box.</div>';
      return;
    }
    container.innerHTML = rows.map(({ agent, pane, tail, needs }) => {
      const open = expanded.has(pane);
      const title = agent.terminal_title_stripped || agent.cwd || pane;
      const status = agent.agent_status || 'running';
      return `<article class="net-agent ${needs ? 'needs-you' : ''}" data-pane="${escn(pane)}">
        <button class="net-agent-head" data-herd-toggle="${escn(pane)}" aria-expanded="${open}">
          <span class="net-agent-kind">${escn(agent.kind || 'agent')}</span>
          <span class="net-agent-title"><strong>${escn(title)}</strong><small>${escn(pane)}${agent.cwd ? ' / ' + escn(agent.cwd) : ''}</small></span>
          <span class="net-status ${needs ? 'waiting' : escn(status)}">${needs ? 'needs you' : escn(status)}</span>
        </button>
        <div class="net-agent-body" ${open ? '' : 'hidden'}>
          <pre class="net-tail">${escn(tail || 'Reading the pane...')}</pre>
          <div class="net-agent-tools">
            <form class="net-prompt" data-herd-prompt="${escn(pane)}"><input name="text" aria-label="Prompt ${escn(title)}" placeholder="Send a prompt to this pane"><button>Send</button></form>
            <div class="net-key-row">${actionButton(pane, 'interrupt', 'Interrupt')}${actionButton(pane, 'Enter', 'Enter')}${actionButton(pane, 'y', 'y')}${actionButton(pane, 'n', 'n')}</div>
          </div>
        </div>
      </article>`;
    }).join('');
  }

  async function refreshHerd(container) {
    try {
      const state = await json('/api/state');
      herdAgents = state.agents || [];
      renderHerdRows(container);
      await pollHerd(container);
    } catch (error) {
      container.innerHTML = `<div class="net-empty">${escn(error.message || 'Could not read agents')}</div>`;
    }
  }

  async function pollHerd(container) {
    if (herdLoading || !expanded.size || current !== 'herd') return;
    herdLoading = true;
    try {
      await Promise.all([...expanded].map(async (pane) => {
        try { tails.set(pane, await json('/api/herd/read?pane=' + encodeURIComponent(pane) + '&lines=80')); } catch (error) { tails.set(pane, { text: error.message }); }
      }));
      renderHerdRows(container);
    } finally {
      herdLoading = false;
    }
  }

  async function herdView(container) {
    if (!container.dataset.ready) {
      container.dataset.ready = '1';
      container.innerHTML = '<div class="net-view-head"><div><p class="view-subtitle">Live agent controls</p><p>Expand a pane to read its tail and send input. Waiting agents float to the top.</p></div><span class="net-cap">Agent controls</span></div><div id="herd-board" class="net-agent-list"></div>';
      container.addEventListener('click', async (event) => {
        const toggle = event.target.closest('[data-herd-toggle]');
        if (toggle) {
          const pane = toggle.dataset.herdToggle;
          if (expanded.has(pane)) expanded.delete(pane); else expanded.add(pane);
          renderHerdRows($n('herd-board'));
          await pollHerd($n('herd-board'));
          return;
        }
        const button = event.target.closest('[data-herd-action]');
        if (!button) return;
        const pane = button.dataset.pane;
        const action = button.dataset.herdAction;
        setStatus(action === 'interrupt' ? 'Interrupting ' + pane + '...' : 'Sending ' + action + ' to ' + pane + '...');
        try {
          const path = action === 'interrupt' ? '/api/herd/interrupt' : '/api/herd/keys';
          const body = action === 'interrupt' ? { pane } : { pane, keys: action };
          await json(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
          setStatus('Sent ' + action + ' to ' + pane + '.');
          await pollHerd($n('herd-board'));
        } catch (error) { setStatus(error.message || 'Could not send input', true); }
      });
      container.addEventListener('submit', async (event) => {
        const form = event.target.closest('[data-herd-prompt]');
        if (!form) return;
        event.preventDefault();
        const text = new FormData(form).get('text');
        if (!String(text || '').trim()) return;
        try {
          await json('/api/herd/prompt', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ pane: form.dataset.herdPrompt, text }) });
          form.reset();
          setStatus('Prompt sent to ' + form.dataset.herdPrompt + '.');
          await pollHerd($n('herd-board'));
        } catch (error) { setStatus(error.message || 'Could not send prompt', true); }
      });
    }
    await refreshHerd($n('herd-board'));
    clearInterval(herdTimer);
    herdTimer = setInterval(() => pollHerd($n('herd-board')), 2000);
  }

  function browserView(container) {
    if (!container.dataset.ready) {
      container.dataset.ready = '1';
      container.innerHTML = '<div class="net-view-head"><div><p class="view-subtitle">Remote Chromium</p><p>One managed browser per box. Agents can connect through the CDP tunnel and people can use it here.</p></div><span class="net-cap">1 browser</span></div><div class="net-controls"><button class="button-primary" data-browser="start">Start headless browser</button><button class="button-danger" data-browser="stop">Stop browser</button><span id="browser-status" class="net-inline-status"></span></div><div class="browser-interactive"><div class="browser-screen-head"><span id="browser-screen-status" class="net-status">Browser is ready when you start it.</span><button data-browser-keyboard>Keyboard</button><input id="browser-keyboard" class="browser-keyboard" aria-label="Browser keyboard input" autocomplete="off"></div><canvas id="browser-canvas" class="browser-canvas"></canvas><form class="browser-url" data-browser-url><button type="button" data-browser-nav="back" aria-label="Back">Back</button><button type="button" data-browser-nav="forward" aria-label="Forward">Forward</button><button type="button" data-browser-nav="reload" aria-label="Reload">Reload</button><input name="url" aria-label="Browser URL" placeholder="https://example.com"><button>Go</button><button type="button" data-tab-new>New tab</button></form></div><div id="browser-pages" class="net-pages"></div>';
      container.addEventListener('click', async (event) => {
        const control = event.target.closest('[data-browser]');
        const shot = event.target.closest('[data-shot]');
        const nav = event.target.closest('[data-browser-nav]');
        const newTab = event.target.closest('[data-tab-new]');
        const closeTab = event.target.closest('[data-tab-close]');
        try {
          if (control) {
            const action = control.dataset.browser;
            if (action === 'stop' && !browserStopArmed) {
              browserStopArmed = true;
              setStatus('Stop the managed browser? Press Stop again to confirm, or Escape to cancel.');
              await renderBrowserState(container);
              return;
            }
            browserStopArmed = false;
            setStatus(action === 'start' ? 'Starting browser...' : 'Stopping browser...');
            await json('/api/browser/' + action, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: action === 'start' ? JSON.stringify({ headless: true }) : '{}' });
            setStatus(action === 'start' ? 'Browser started.' : 'Browser stopped.');
            await renderBrowserState(container);
          }
          if (nav) {
            const commands = { back: 'Page.goBack', forward: 'Page.goForward', reload: 'Page.reload' };
            browserScreenSend({ id: Date.now(), method: commands[nav.dataset.browserNav], params: {} });
          }
          if (newTab) {
            await json('/api/browser/tabs', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url: 'about:blank' }) });
            await renderBrowserState(container);
          }
          if (closeTab) {
            await json('/api/browser/tabs?page=' + encodeURIComponent(closeTab.dataset.tabClose), { method: 'DELETE' });
            if (browserScreenPage === closeTab.dataset.tabClose) closeBrowserScreen();
            await renderBrowserState(container);
          }
          if (shot) {
            shot.disabled = true;
            const result = await json('/api/browser/shot?page=' + encodeURIComponent(shot.dataset.shot));
            shots.set(shot.dataset.shot, result.data);
            await renderBrowserState(container);
          }
        } catch (error) { setStatus(error.message || 'Browser action failed', true); }
        finally { if (shot) shot.disabled = false; }
      });
      container.addEventListener('submit', async (event) => {
        const form = event.target.closest('[data-browser-url]');
        if (!form) return;
        event.preventDefault();
        const url = String(new FormData(form).get('url') || '').trim();
        if (!url) return;
        browserScreenSend({ id: Date.now(), method: 'Page.navigate', params: { url } });
      });
      installBrowserInput(container);
      container.querySelector('[data-browser-keyboard]')?.addEventListener('click', () => $n('browser-keyboard')?.focus());
    }
    renderBrowserState(container);
  }

  async function renderBrowserState(container) {
    const pages = $n('browser-pages');
    try {
      const state = await json('/api/browser');
      const status = $n('browser-status');
      const stopButton = container.querySelector('[data-browser="stop"]');
      if (stopButton) { stopButton.textContent = browserStopArmed ? 'Confirm stop' : 'Stop browser'; stopButton.disabled = !state.running; stopButton.classList.toggle('button-danger', !browserStopArmed); stopButton.classList.toggle('confirm', browserStopArmed); }
      if (status) status.textContent = state.running ? `PID ${state.pid} / ${Math.round((state.memory || 0) / 1048576)} MB${state.agentDriving ? ' / an agent is driving this browser' : ''}` : (state.error || 'Browser is stopped');
      if (!state.running) { closeBrowserScreen(); const canvas = $n('browser-canvas'); if (canvas) { canvas.width = 1; canvas.height = 1; canvas.getContext('2d')?.clearRect(0, 0, 1, 1); } pages.innerHTML = '<div class="net-empty">Browser is ready when you start it. Open a page to begin.</div>'; return; }
      if (!state.pages?.length) { pages.innerHTML = '<div class="net-empty">Browser is running. Open a page through CDP to see it here.</div>'; return; }
      const selected = browserScreenPage || state.pages.find((page) => page.type === 'page')?.id;
      if (selected) connectBrowserScreen(selected);
      pages.innerHTML = state.pages.filter((page) => page.type === 'page').map((page) => `<article class="net-page ${page.id === selected ? 'selected' : ''}"><button class="net-page-copy" data-tab-select="${escn(page.id)}"><strong>${escn(page.title || 'Untitled page')}</strong><small>${escn(page.url || page.id)}</small></button><div class="net-page-actions"><button class="net-action" data-shot="${escn(page.id)}">Screenshot</button><button class="net-action" data-tab-close="${escn(page.id)}">Close</button></div>${shots.has(page.id) ? `<img class="net-shot" alt="Screenshot of ${escn(page.title || page.url)}" src="data:image/png;base64,${shots.get(page.id)}">` : ''}</article>`).join('');
      const url = state.pages.find((page) => page.id === selected)?.url || '';
      const urlInput = container.querySelector('input[name="url"]');
      if (urlInput && document.activeElement !== urlInput) urlInput.value = url;
      pages.querySelectorAll('[data-tab-select]').forEach((button) => button.addEventListener('click', () => { connectBrowserScreen(button.dataset.tabSelect); renderBrowserState(container); }));
    } catch (error) { pages.innerHTML = `<div class="net-empty">${escn(error.message || 'Could not read browser state')}</div>`; }
  }

  function networkView(container) {
    if (!container.dataset.ready) {
      container.dataset.ready = '1';
      container.innerHTML = '<div class="net-view-head"><div><p class="view-subtitle">Tailnet surface</p><p>See how this box is reachable and which local ports are exposed.</p></div><span class="net-cap">Private box</span></div><div id="network-board" class="net-network-board"></div>';
    }
    renderNetworkState(container);
  }

  function copyValue(value) {
    navigator.clipboard?.writeText(value).then(() => setStatus('Copied ' + value + '.')).catch(() => setStatus('Copy was blocked. Select the address manually.', true));
  }

  async function renderNetworkState(container) {
    const board = $n('network-board');
    try {
      const state = await json('/api/net');
      const ts = state.tailscale || {};
      const self = ts.self || {};
      const peerRows = (ts.peers || []).map((peer) => `<li><span><strong>${escn(peer.name || 'Unnamed peer')}</strong><small>${escn(peer.os || 'unknown OS')} / ${(peer.ips || []).map(escn).join(', ')}</small></span><span class="net-status ${peer.online ? 'online' : 'offline'}">${peer.online ? 'online' : 'offline'}</span></li>`).join('');
      const addressRows = [...(self.ips || []), ...(state.interfaces || []).flatMap((iface) => iface.ipv4 || [])].filter((value, index, all) => value && all.indexOf(value) === index).map((value) => `<button class="net-copy" data-copy="${escn(value)}"><span class="mono">${escn(value)}</span><span>Copy</span></button>`).join('');
      const interfaceRows = (state.interfaces || []).map((iface) => `<li><strong>${escn(iface.name)}</strong><span class="mono">${(iface.ipv4 || []).map(escn).join(', ')}</span></li>`).join('');
      const serveRows = (state.serve || []).map((entry) => `<li><span><strong>${escn(entry.protocol)} ${escn(entry.address || '')}${escn(entry.path || '')}</strong><small>tailscale serve</small></span><span class="mono">${escn(entry.target)}</span></li>`).join('');
      const mirrorRows = (state.mirrors || []).map((port) => `<li><span><strong>Port ${escn(port)}</strong><small>plain TCP mirror</small></span><span class="net-status online">no password</span></li>`).join('');
      const deviceRows = (state.devices || []).map((device) => {
        const providers = device.usage?.providers || {};
        const tokens = Object.entries(providers).map(([provider, value]) => `${provider} ${Number(value.today?.tokens?.in || 0) + Number(value.today?.tokens?.out || 0)} tokens`).join(' / ');
        const stateText = device.boxdeck ? (device.version || 'boxdeck') : 'not installed';
        return `<li><span><strong>${escn(device.name || 'Unnamed device')}</strong><small>${escn(device.os || 'unknown OS')} / ${device.online ? 'online' : 'offline'} / ${escn(stateText)}</small>${device.boxdeck ? '' : '<small>Install: curl -fsSL https://raw.githubusercontent.com/wicolian/boxdeck/main/install.sh | sh</small>'}${device.usageOk && tokens ? `<small class="net-usage">today ${escn(tokens)}</small>` : ''}</span><span class="net-status ${device.boxdeck ? 'online' : 'offline'}">${device.boxdeck ? 'boxdeck' : 'not installed'}</span></li>`;
      }).join('');
      const errors = (state.mirrorErrors || []).map(escn).join('<br>');
      board.innerHTML = `<section class="net-card"><div class="net-card-head"><h3>Tailscale</h3><span class="net-status ${ts.running ? 'online' : 'offline'}">${ts.running ? 'connected' : ts.present ? 'not connected' : 'not installed'}</span></div><p class="net-message">${escn(ts.message || 'No status message')}</p>${self.name ? `<div class="net-self"><strong>${escn(self.name)}</strong><small>${escn(self.dnsName || '')} / ${escn(self.os || '')}</small></div>` : ''}<div class="net-copy-list">${addressRows || '<span class="net-muted">No non-loopback addresses found.</span>'}</div><ul class="net-list">${peerRows || '<li class="net-muted">No tailnet peers reported.</li>'}</ul></section><section class="net-card"><div class="net-card-head"><h3>Exposed by Tailscale serve</h3><span class="net-status">${(state.serve || []).length} entries</span></div><ul class="net-list">${serveRows || '<li class="net-muted">Nothing is exposed by tailscale serve.</li>'}</ul></section><section class="net-card"><div class="net-card-head"><h3>Local interfaces</h3><span class="net-status">${(state.interfaces || []).length} interfaces</span></div><ul class="net-list">${interfaceRows || '<li class="net-muted">No IPv4 interfaces found.</li>'}</ul></section><section class="net-card"><div class="net-card-head"><h3>Boxdeck mirrors</h3><span class="net-status">${(state.mirrors || []).length} ports</span></div><p class="net-message">${escn(state.mirrorBind ? 'Bound to ' + state.mirrorBind : 'No private mirror address detected')}</p><ul class="net-list">${mirrorRows || '<li class="net-muted">No mirrored ports. Enable mirror auto on a tailnet address.</li>'}</ul>${errors ? `<p class="net-errors">${errors}</p>` : ''}<p class="net-rule">${escn(state.mirrorRule || 'Mirrored ports carry no password')}</p></section><section class="net-card net-wide"><div class="net-card-head"><h3>Devices on this tailnet</h3><span class="net-status">${(state.devices || []).length} peers</span></div><p class="net-message">Each peer is probed over Tailscale. A fleet token adds today's usage when the peer accepts it.</p><ul class="net-list">${deviceRows || '<li class="net-muted">No online tailnet peers found.</li>'}</ul></section>`;
      board.querySelectorAll('[data-copy]').forEach((button) => { button.addEventListener('click', () => copyValue(button.dataset.copy)); });
    } catch (error) { board.innerHTML = `<div class="net-empty">${escn(error.message || 'Could not read network state')}</div>`; }
  }

  addView('herd', 'Agent controls', herdView);
  addView('browser', 'Browser', browserView);
  addView('network', 'Network', networkView);
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && browserStopArmed) {
      browserStopArmed = false;
      const button = document.querySelector('[data-browser="stop"]');
      if (button) { button.textContent = 'Stop browser'; button.classList.remove('confirm'); }
      setStatus('Browser stop cancelled.');
    }
  });
  window.addEventListener('hashchange', route);
  route();
}());

#!/usr/bin/env node
// boxdeck: a one-page cockpit for your ssh dev box.
// Live ports, machine health, agents, tmux, browsers, docker, fresh reports.
// Zero dependencies. Reads /proc; shells out only when a client is watching.
'use strict';
const http = require('http'), fs = require('fs'), path = require('path'), os = require('os');
const { execFileSync } = require('child_process'), crypto = require('crypto'), net = require('net');

// ---------- config: env > ~/.config/boxdeck/config.json > defaults ----------
const HOME = os.homedir();
const CONFIG_PATH = process.env.BOXDECK_CONFIG || path.join(HOME, '.config', 'boxdeck', 'config.json');
let file = {}; try { file = JSON.parse(fs.readFileSync(CONFIG_PATH, 'utf8')); } catch {}
const cfg = {
  port: +(process.env.BOXDECK_PORT || file.port || 8100),
  bind: process.env.BOXDECK_BIND || file.bind || '127.0.0.1',
  host: process.env.BOXDECK_HOST || file.host || os.hostname(),       // hostname your browser uses to reach this box
  user: process.env.BOXDECK_USER || file.user || 'admin',
  password: process.env.BOXDECK_PASSWORD || file.password || '',
  filesPort: +(process.env.BOXDECK_FILES_PORT || file.filesPort || 0),  // a file viewer to link reports through (0 = none)
  known: Object.assign({}, DEFAULT_KNOWN(), file.known || {}),         // port -> label
  hide: new Set((file.hide || [22, 53, 111, 139, 445]).map(Number)),
  quick: file.quick || [],                                             // [["Files", 8090], ...] shortcuts in the masthead
  reportRoots: (file.reportRoots || ['~/reports', '~/box']).map(p => p.replace(/^~/, HOME)),
  reportDays: +(file.reportDays || 3),
  agentPattern: new RegExp(file.agentPattern || '^(\\S*/)?(claude|codex|aider|opencode|goose)(\\s|$)'),
  title: file.title || 'Deck',
  mirror: process.env.BOXDECK_MIRROR ? process.env.BOXDECK_MIRROR !== '0' : (file.mirror ?? 'auto'),   // re-publish every local port on the tailnet IP
  mirrorBind: process.env.BOXDECK_MIRROR_BIND || file.mirrorBind || '',                                  // the private IP to publish on (auto: tailscale 100.x)
};
if (!cfg.password) { console.error(`boxdeck: set a password (BOXDECK_PASSWORD or "password" in ${CONFIG_PATH})`); process.exit(1); }
function DEFAULT_KNOWN() { return { 3000: 'App', 3001: 'App', 4000: 'API', 5173: 'Vite', 4173: 'Vite preview', 8080: 'HTTP', 8000: 'HTTP', 6006: 'Storybook', 5432: 'Postgres', 6379: 'Redis', 27017: 'MongoDB', 9222: 'Chrome CDP', 7681: 'Terminal (ttyd)', 8384: 'Syncthing', 3773: 'T3 Code' }; }
cfg.known[cfg.port] = cfg.known[cfg.port] || `${cfg.title} (this page)`;
if (cfg.filesPort) cfg.known[cfg.filesPort] = cfg.known[cfg.filesPort] || 'Files';

const sh = (cmd, args, opt = {}) => { try { return execFileSync(cmd, args, { encoding: 'utf8', timeout: 4000, stdio: ['ignore', 'pipe', 'ignore'], ...opt }); } catch { return ''; } };
const tilde = p => p.startsWith(HOME) ? '~' + p.slice(HOME.length) : p;
const memo = (ms, fn) => { let at = 0, v; return (...a) => { if (Date.now() - at > ms) { v = fn(...a); at = Date.now(); } return v; }; };

// ---------- health: /proc only, cheap enough to sample always ----------
const HIST = 120, hist = { cpu: [], mem: [], swap: [], load: [] };
let prevCpu = null, health = {};
function cpuPct() {
  const l = fs.readFileSync('/proc/stat', 'utf8').split('\n')[0].trim().split(/\s+/).slice(1).map(Number);
  const idle = l[3] + l[4], total = l.reduce((a, b) => a + b, 0); let pct = 0;
  if (prevCpu) { const dt = total - prevCpu.total, di = idle - prevCpu.idle; pct = dt ? Math.round(100 * (1 - di / dt)) : 0; }
  prevCpu = { idle, total }; return pct;
}
function meminfo() { const m = {}; for (const ln of fs.readFileSync('/proc/meminfo', 'utf8').split('\n')) { const [k, v] = ln.split(':'); if (v) m[k] = parseInt(v) * 1024; } return m; }
const disk = memo(30000, () => { const c = sh('df', ['-k', '/']).split('\n')[1]?.trim().split(/\s+/) || []; return { total: (+c[1] || 0) * 1024, used: (+c[2] || 0) * 1024, pct: parseInt(c[4]) || 0 }; });
let watchers = 0; // clients that polled in the last 15 s; sampling slows down when nobody is looking
function sample() {
  const m = meminfo(), load = fs.readFileSync('/proc/loadavg', 'utf8').trim().split(' '), cpu = cpuPct(), d = disk();
  const memUsed = m.MemTotal - m.MemAvailable, swapUsed = m.SwapTotal - m.SwapFree;
  health = { cpu, cores: os.cpus().length, load1: +load[0], load5: +load[1], load15: +load[2], memTotal: m.MemTotal, memUsed, memAvail: m.MemAvailable,
    swapTotal: m.SwapTotal, swapUsed, swapFree: m.SwapFree, diskTotal: d.total, diskUsed: d.used, diskPct: d.pct, uptime: os.uptime(), host: os.hostname(), t: Date.now() };
  const push = (k, v) => { hist[k].push(v); if (hist[k].length > HIST) hist[k].shift(); };
  push('cpu', cpu); push('mem', Math.round(100 * memUsed / m.MemTotal)); push('swap', m.SwapTotal ? Math.round(100 * swapUsed / m.SwapTotal) : 0); push('load', +load[0]);
  setTimeout(sample, watchers > 0 ? 3000 : 30000);
}
sample();

// ---------- ports ----------
const titles = new Map();
function probeTitle(port) {
  const c = titles.get(port); if (c && Date.now() - c.at < 120000) return;
  titles.set(port, { title: c?.title || '', at: Date.now() });
  const req = http.get({ host: '127.0.0.1', port, path: '/', timeout: 1500, headers: { Accept: 'text/html' } }, res => {
    let body = ''; res.setEncoding('utf8');
    res.on('data', d => { body += d; if (body.length > 65536) req.destroy(); });
    res.on('end', () => { const m = body.match(/<title[^>]*>([^<]{1,80})/i); titles.set(port, { title: m ? m[1].trim() : res.statusCode === 401 ? 'needs login' : '', at: Date.now() }); });
  });
  req.on('timeout', () => req.destroy()); req.on('error', () => {});
}
const ports = memo(3000, () => {
  const out = sh('ss', ['-H', '-ltnp']); const seen = new Map();
  for (const ln of out.split('\n')) {
    const c = ln.trim().split(/\s+/); if (c.length < 5) continue;
    const local = c[3], i = local.lastIndexOf(':'), port = +local.slice(i + 1), addr = local.slice(0, i);
    if (!port || cfg.hide.has(port)) continue;
    if (/^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./.test(addr) || addr.startsWith('[fd7a:115c:a1e0')) continue; // tailscale serve mirrors of a local port
    const pm = ln.match(/users:\(\("([^"]+)",pid=(\d+)/); const proc = pm ? (pm[1] === 'MainThread' ? 'node' : pm[1]) : '', pid = pm ? +pm[2] : 0;
    if (!seen.has(port) || (proc && !seen.get(port).proc)) seen.set(port, { port, addr, proc, pid });
  }
  const list = [...seen.values()].sort((a, b) => a.port - b.port);
  for (const p of list) {
    if (p.pid) { try { p.cwd = tilde(fs.readlinkSync(`/proc/${p.pid}/cwd`)); } catch { p.cwd = ''; } }
    p.label = cfg.known[p.port] || ''; p.ephemeral = p.port >= 32768 && !p.label;
    if (!p.ephemeral) probeTitle(p.port); p.title = titles.get(p.port)?.title || ''; p.url = `http://${cfg.host}:${p.port}`;
  }
  return list;
});

// ---------- processes: agents, tmux, browsers ----------
const tmuxPanes = memo(3000, () => sh('tmux', ['list-panes', '-a', '-F', '#{session_name}\t#{window_index}\t#{window_name}\t#{pane_index}\t#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}\t#{session_attached}'])
  .trim().split('\n').filter(Boolean).map(l => { const [session, win, wname, pane, pid, cmd, cwd, attached] = l.split('\t');
    return { session, win: +win, wname, pane: +pane, pid: +pid, cmd, cwd: tilde(cwd || ''), attached: attached !== '0' }; }));
const procs = memo(3000, () => { const all = [];
  for (const ln of sh('ps', ['-eo', 'pid=,ppid=,etimes=,pcpu=,rss=,args=']).split('\n')) { const m = ln.trim().match(/^(\d+)\s+(\d+)\s+(\d+)\s+([\d.]+)\s+(\d+)\s+(.*)$/); if (m) all.push({ pid: +m[1], ppid: +m[2], secs: +m[3], cpu: +m[4], rss: +m[5] * 1024, args: m[6] }); }
  return all; });
function agents(all, panes) {
  const byPid = new Map(all.map(p => [p.pid, p])), paneByPid = new Map(panes.map(p => [p.pid, p])), list = [];
  for (const p of all) {
    if (!cfg.agentPattern.test(p.args) || /^(\/bin\/)?(ba)?sh -c|^(rg|grep|ps|tail|less) /.test(p.args)) continue;
    const kind = (p.args.match(cfg.agentPattern) || [])[2] || 'agent';
    let cur = p, pane = null, depth = 0; while (cur && depth++ < 12) { if (paneByPid.has(cur.pid)) { pane = paneByPid.get(cur.pid); break; } cur = byPid.get(cur.ppid); }
    let cwd = ''; try { cwd = tilde(fs.readlinkSync(`/proc/${p.pid}/cwd`)); } catch {}
    const model = (p.args.match(/--model[= ]([\w.:-]+)/) || [])[1] || '';
    list.push({ pid: p.pid, kind, model, secs: p.secs, cpu: p.cpu, rss: p.rss, cwd, pane: pane ? `${pane.session}:${pane.win}.${pane.pane}` : '', win: pane?.wname || '' });
  }
  return list.sort((a, b) => b.secs - a.secs);
}
function browsers(all) {
  const heads = all.filter(p => /chrom(e|ium)/.test(p.args) && /--remote-debugging|--headless/.test(p.args) && !/--type=/.test(p.args));
  const helpers = all.filter(p => /--type=/.test(p.args) && /chrom/.test(p.args));
  return heads.map(h => ({ pid: h.pid, port: (h.args.match(/--remote-debugging-port=(\d+)/) || [])[1] || '', secs: h.secs, headless: /--headless/.test(h.args),
    rss: h.rss + helpers.filter(p => p.ppid === h.pid || p.args.includes(`--remote-debugging-port=${(h.args.match(/--remote-debugging-port=(\d+)/) || [])[1]}`)).reduce((s, p) => s + p.rss, 0) }));
}
let dockerOk = true;
const docker = memo(10000, () => {
  if (!dockerOk) return [];
  let out = sh('docker', ['ps', '--format', '{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}']);
  if (!out) { out = sh('sg', ['docker', '-c', 'docker ps --format "{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"']); if (!out) { dockerOk = false; setTimeout(() => { dockerOk = true; }, 300000); } }
  return out.trim().split('\n').filter(Boolean).map(l => { const [name, image, status, ports] = l.split('\t'); return { name, image, status, ports }; });
});
const reports = memo(20000, () => {
  const roots = cfg.reportRoots.filter(d => fs.existsSync(d)); if (!roots.length) return [];
  const out = sh('find', [...roots, '-xdev', '-not', '-path', '*/node_modules/*', '-not', '-path', '*/.git/*', '-type', 'f', '(', '-name', '*.md', '-o', '-name', '*.html', '-o', '-name', '*.png', '-o', '-name', '*.pdf', ')', '-mtime', `-${cfg.reportDays}`, '-printf', '%T@\t%s\t%p\n'], { timeout: 8000, maxBuffer: 8 << 20 });
  return out.trim().split('\n').filter(Boolean).map(l => { const [t, s, p] = l.split('\t'); return { t: +t * 1000, size: +s, path: p }; })
    .sort((a, b) => b.t - a.t).slice(0, 25)
    .map(r => ({ t: r.t, size: r.size, rel: tilde(r.path), url: cfg.filesPort ? `http://${cfg.host}:${cfg.filesPort}${r.path.startsWith(HOME) ? r.path.slice(HOME.length) : r.path}` : '' }));
});
// ---------- mirror: publish every local port on the private (tailscale) IP, plain TCP, so
// http://box:3001 reaches 127.0.0.1:3001 from any device on the tailnet. No sudo, no tailscale serve.
const mirrors = new Map();
const isTailnetIp = a => /^100\.(6[4-9]|[7-9]\d|1[01]\d|12[0-7])\./.test(a);
function mirrorIp() { if (cfg.mirrorBind) return cfg.mirrorBind; for (const addrs of Object.values(os.networkInterfaces())) for (const a of addrs) if (a.family === 'IPv4' && isTailnetIp(a.address)) return a.address; return ''; }
function syncMirrors() {
  const ip = mirrorIp(); if (!ip) return;
  const want = new Set(ports().filter(p => !p.ephemeral).map(p => p.port)); want.add(cfg.port);
  for (const port of want) if (!mirrors.has(port)) {
    const srv = net.createServer(sock => { const up = net.connect(port, '127.0.0.1'); const kill = () => { sock.destroy(); up.destroy(); };
      sock.on('error', kill); up.on('error', kill); sock.pipe(up); up.pipe(sock); });
    srv.on('error', () => { srv.close(); mirrors.set(port, null); setTimeout(() => mirrors.delete(port), 60000); }); // taken (tailscale serve?) - retry in a minute
    srv.listen(port, ip); mirrors.set(port, srv);
  }
  for (const [port, srv] of mirrors) if (!want.has(port)) { srv && srv.close(); mirrors.delete(port); }
}
if (cfg.mirror === true || (cfg.mirror === 'auto' && mirrorIp())) { syncMirrors(); setInterval(syncMirrors, 5000); console.log(`mirroring local ports on ${mirrorIp()}`); }

function state() {
  const all = procs(), panes = tmuxPanes(), sessions = {};
  for (const p of panes) { (sessions[p.session] ||= { name: p.session, windows: new Set(), attached: p.attached }).windows.add(p.win); }
  return { title: cfg.title, health, hist, ports: ports(), agents: agents(all, panes), tmux: Object.values(sessions).map(s => ({ ...s, windows: s.windows.size })),
    browsers: browsers(all), docker: docker(), reports: reports(), quick: cfg.quick, host: cfg.host, mirror: [...mirrors.keys()].filter(p => mirrors.get(p)), now: Date.now() };
}

// ---------- http ----------
const PAGE = fs.readFileSync(path.join(__dirname, 'index.html'));
function authed(req) {
  const h = req.headers.authorization || ''; if (!h.startsWith('Basic ')) return false;
  const [u, ...rest] = Buffer.from(h.slice(6), 'base64').toString().split(':'), p = rest.join(':');
  const eq = (a, b) => a.length === b.length && crypto.timingSafeEqual(Buffer.from(a), Buffer.from(b));
  return eq(u, cfg.user) && eq(p, cfg.password);
}
let lastPoll = 0; setInterval(() => { watchers = Date.now() - lastPoll < 15000 ? 1 : 0; }, 5000);
http.createServer((req, res) => {
  if (!authed(req)) { res.writeHead(401, { 'WWW-Authenticate': `Basic realm="${cfg.title}", charset="UTF-8"`, 'Content-Type': 'text/plain' }); return res.end('sign in'); }
  const url = req.url.split('?')[0];
  if (url === '/api/state') { lastPoll = Date.now(); watchers = 1; let s; try { s = state(); } catch (e) { res.writeHead(500); return res.end(String(e)); }
    res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); return res.end(JSON.stringify(s)); }
  if (url === '/' || url === '/index.html') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(PAGE); }
  res.writeHead(404); res.end('not found');
}).listen(cfg.port, cfg.bind, () => console.log(`boxdeck on http://${cfg.bind}:${cfg.port}  (reach it as http://${cfg.host}:${cfg.port})`));

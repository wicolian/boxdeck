#!/usr/bin/env node
// Optional companion: a tiny file viewer for the home folder. Renders .md, lists folders, serves images.
// Same basic-auth as boxdeck (reads the same config). BOXDECK_FILES_PORT or "filesPort" picks the port.
'use strict';
const http = require('http'), fs = require('fs'), path = require('path'), os = require('os'), crypto = require('crypto');
const HOME = os.homedir(), CONFIG_PATH = process.env.BOXDECK_CONFIG || path.join(HOME, '.config', 'boxdeck', 'config.json');
let file = {}; try { file = JSON.parse(fs.readFileSync(CONFIG_PATH, 'utf8')); } catch {}
const PORT = +(process.env.BOXDECK_FILES_PORT || file.filesPort || 8090), ROOT = process.env.BOXDECK_FILES_ROOT || file.filesRoot || HOME;
const USER = process.env.BOXDECK_USER || file.user || 'admin', PASS = process.env.BOXDECK_PASSWORD || file.password || '';
const AUTH = process.env.BOXDECK_FILES_AUTH ? process.env.BOXDECK_FILES_AUTH !== '0' : file.filesAuth !== false;
if (AUTH && !PASS) { console.error(`boxdeck-files: set a password (BOXDECK_PASSWORD or "password" in ${CONFIG_PATH}), or set filesAuth:false on purpose`); process.exit(1); }
const ROOT_REAL = fs.realpathSync(ROOT);
// Files you keep are rendered as-is, so a hostile .html/.svg/.md could run script. Every response is
// sandboxed (opaque origin) and the server sends no CORS headers, so such a page cannot read other files.
const CSP_DOC = "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src * data:";
const CSP_RAW = 'sandbox allow-scripts allow-popups allow-modals';
const stripScript = h => h.replace(/<script[\s\S]*?<\/script>/gi, '').replace(/\son[a-z]+\s*=/gi, ' data-x=').replace(/(href|src)\s*=\s*(["']?)\s*javascript:/gi, '$1=$2#');
let marked = null; try { marked = require('marked').marked; } catch {}
const md2html = md => marked ? stripScript(marked.parse(md)) : `<pre>${esc(md)}</pre>`;
const MIME = { '.html': 'text/html', '.css': 'text/css', '.js': 'text/javascript', '.json': 'application/json', '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.gif': 'image/gif', '.svg': 'image/svg+xml', '.webp': 'image/webp', '.pdf': 'application/pdf', '.txt': 'text/plain; charset=utf-8', '.log': 'text/plain; charset=utf-8', '.csv': 'text/plain; charset=utf-8', '.mp4': 'video/mp4', '.webm': 'video/webm' };
const CSS = `<style>body{max-width:860px;margin:2rem auto;padding:0 1rem;font:16px/1.6 -apple-system,system-ui,sans-serif;color:#222;background:#fff}pre{background:#f4f4f4;padding:.8rem;overflow:auto;border-radius:6px}code{font-size:.9em}table{border-collapse:collapse}td,th{border:1px solid #ddd;padding:.3rem .6rem}img{max-width:100%}a{color:#0a58ca}ul.ls{list-style:none;padding:0}ul.ls li{padding:.15rem 0}@media(prefers-color-scheme:dark){body{background:#111;color:#ddd}pre{background:#1e1e1e}a{color:#7ab7ff}td,th{border-color:#333}}</style>`;
const esc = s => s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
function authed(req) { if (!AUTH) return true; const h = req.headers.authorization || ''; if (!h.startsWith('Basic ')) return false;
  const [u, ...r] = Buffer.from(h.slice(6), 'base64').toString().split(':'), p = r.join(':'); const eq = (a, b) => a.length === b.length && crypto.timingSafeEqual(Buffer.from(a), Buffer.from(b)); return eq(u, USER) && eq(p, PASS); }
http.createServer((req, res) => {
  if (!authed(req)) { res.writeHead(401, { 'WWW-Authenticate': 'Basic realm="files", charset="UTF-8"' }); return res.end('sign in'); }
  const url = decodeURIComponent(req.url.split('?')[0]), abs = path.normalize(path.join(ROOT, url));
  if (!abs.startsWith(ROOT + path.sep) && abs !== ROOT) { res.writeHead(403); return res.end('forbidden'); }
  let st, real; try { real = fs.realpathSync(abs); st = fs.statSync(real); } catch { res.writeHead(404); return res.end('not found: ' + url); }
  // symlinks that point outside the root are followed on purpose (~/box -> a mount), the path itself may not escape
  if (st.isDirectory()) {
    if (!url.endsWith('/')) { res.writeHead(302, { Location: url + '/' }); return res.end(); }
    const rows = fs.readdirSync(abs, { withFileTypes: true }).filter(d => !d.name.startsWith('.') && d.name !== 'node_modules')
      .sort((a, b) => (b.isDirectory() - a.isDirectory()) || a.name.localeCompare(b.name))
      .map(d => `<li><a href="${esc(encodeURIComponent(d.name))}${d.isDirectory() ? '/' : ''}">${esc(d.name)}${d.isDirectory() ? '/' : ''}</a></li>`);
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Content-Security-Policy': CSP_DOC });
    return res.end(`<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><title>${esc(url)}</title>${CSS}<h2>${esc(url)}</h2><ul class="ls">${url === '/' ? '' : '<li><a href="../">../</a></li>'}${rows.join('')}</ul>`);
  }
  const ext = path.extname(abs).toLowerCase();
  if (ext === '.md' || ext === '.markdown') { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Content-Security-Policy': CSP_DOC });
    return res.end(`<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><title>${esc(path.basename(abs))}</title>${CSS}<p><a href="./">${esc(path.dirname(url))}/</a></p>${md2html(fs.readFileSync(abs, 'utf8'))}`); }
  const h = { 'Content-Type': MIME[ext] || 'application/octet-stream', 'Content-Length': st.size, 'X-Content-Type-Options': 'nosniff' };
  if (ext === '.html' || ext === '.svg') h['Content-Security-Policy'] = CSP_RAW;
  res.writeHead(200, h); fs.createReadStream(real).pipe(res);
}).listen(PORT, '127.0.0.1', () => console.log(`files on http://127.0.0.1:${PORT} root=${ROOT} auth=${AUTH}`));

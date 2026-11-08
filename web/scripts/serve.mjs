// Minimal static server that mimics the Go panel: serves a dist folder under a
// random-looking prefix (/<panel_path>/) with SPA fallback. Usage:
//   node scripts/serve.mjs dist [port] [prefix]
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';

const root = path.resolve(process.argv[2] || 'dist');
const port = Number(process.argv[3] || 4173);
const prefix = '/' + (process.argv[4] || 'x7k2p9');
const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.woff': 'font/woff', '.png': 'image/png', '.json': 'application/json' };

http.createServer((req, res) => {
  const url = decodeURIComponent((req.url || '/').split('?')[0]);
  if (url === '/' ) { res.writeHead(302, { location: prefix + '/' }); return res.end(); }
  if (!url.startsWith(prefix)) { res.writeHead(404); return res.end('not found'); }
  let rel = url.slice(prefix.length) || '/';
  let file = path.join(root, rel);
  if (!file.startsWith(root) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(root, 'index.html');
  res.writeHead(200, { 'content-type': types[path.extname(file)] || 'application/octet-stream' });
  fs.createReadStream(file).pipe(res);
}).listen(port, () => console.log(`serving ${root} at http://localhost:${port}${prefix}/`));

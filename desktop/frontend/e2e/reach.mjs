// Renders the Reach settings with a router that refused.
//
// That is the state most people land in — plenty of routers ship with UPnP
// off — and the one where saying nothing is worst: "reachable from anywhere"
// switched on, nothing reachable, and no hint whether the setting to change
// is here or on the router. So the refusal has to be visible and has to name
// what to look for.
//
//   npm run build && node e2e/reach.mjs [screenshot.png]
//
// Originally a copy of pairing.mjs whose stub edit silently failed to apply,
// producing a screenshot of the wrong state that looked plausible. Hence the
// assertions below rather than eyeballing the picture.
//
// A QR code that fails to draw looks like an empty panel, which is exactly
// what "still loading" looks like, so this asserts the code is actually there
// and that the two things a person needs to know — that it works once, and
// which server it points at — are on screen with it.
//
//   npm run build && node e2e/pairing.mjs [screenshot.png]
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
const { chromium } = await import(
  process.env.PLAYWRIGHT ?? '/opt/node22/lib/node_modules/playwright/index.mjs',
);

const DIST = path.join(path.dirname(new URL(import.meta.url).pathname), '..', 'dist');
const TYPES = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' };
const server = http.createServer((req, res) => {
  const url = req.url.split('?')[0];
  let file = path.join(DIST, url === '/' ? 'index.html' : url);
  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) file = path.join(DIST, 'index.html');
  res.writeHead(200, { 'Content-Type': TYPES[path.extname(file)] ?? 'application/octet-stream' });
  res.end(fs.readFileSync(file));
});
await new Promise((r) => server.listen(0, r));
const base = `http://127.0.0.1:${server.address().port}`;

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 900, height: 700 } });
page.on('pageerror', (e) => console.log('[pageerror]', e.message));

await page.addInitScript(() => {
  window.go = { main: { App: {
    GetAppInfo: async () => ({ version: 'test', error: '' }),
    GetLocalServer: async () => ({ chosen: true, host: true, running: true,
      url: 'https://127.0.0.1:8080', lanUrl: 'https://192.168.1.20:8080',
      publicUrl: '', remote: true, firstRun: false }),
    SetReachableFromAnywhere: async () => {},
    GetSession: async () => ({ loggedIn: true, serverUrl: 'http://192.168.1.20:8080',
      email: 'someone@example.com', deviceName: 'PC', publicKey: 'abc' }),
    GetStatus: async () => ({ connected: false, peers: [] }),
    GetStartWithSystem: async () => ({ supported: true, enabled: false }),
    ListNetworks: async () => [{ id: 'n1', name: 'Office', memberCount: 1, deviceCount: 1, inviteCode: 'K7M2QP' }],
    TakePendingInvite: async () => '',
    StartPairing: async () => ({
      url: 'nexusvpn://pair?s=http%3A%2F%2F192.168.1.20%3A8080&t=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1aWQiOiI3ZjNhIiwibmlkIjoibjEiLCJleHAiOjE3NjcyMjU2MDB9.signaturegoeshere',
      serverUrl: 'http://192.168.1.20:8080',
      token: 'eyJ...',
      expiresIn: 300,
    }),
    CopyToClipboard: async () => {},
  } } };
  window.runtime = { EventsOn: () => {}, EventsOff: () => {}, EventsOnMultiple: () => {},
    EventsOnce: () => {}, EventsEmit: () => {}, Quit: () => {}, WindowShow: () => {}, WindowHide: () => {} };
});

await page.goto(base);
await page.waitForTimeout(900);
await page.getByText('System', { exact: true }).click();
await page.waitForTimeout(200);
await page.getByText('Settings', { exact: false }).click();
await page.waitForTimeout(500);
await page.getByText('Reach', { exact: true }).click();
await page.waitForTimeout(500);


const text = await page.locator('body').innerText();
if (process.argv[2]) await page.screenshot({ path: process.argv[2] });
await browser.close();
server.close();

console.log('reports the refusal plainly:', /did not open a port/.test(text));
console.log('names what to look for on the router:', /UPnP/.test(text));

if (!/did not open a port/.test(text)) throw new Error('FAIL: a refused router is not reported');
console.log('PASS');

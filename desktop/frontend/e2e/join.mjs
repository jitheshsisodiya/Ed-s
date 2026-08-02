// The screen a second computer sees, with the Go bridge stubbed.
//
// The claim is that joining a network you already run needs no account: no
// address to look up, no email, no password. That claim lives entirely in
// what this screen asks for, so this checks what it asks for — and that the
// link actually reaches ClaimPairing rather than being typed into a field
// nothing reads.
//
//   npm run build && node e2e/join.mjs [screenshot.png]
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

const LINK =
  'nexusvpn://pair?s=http%3A%2F%2F192.168.1.20%3A8080&t=token-goes-here&f=AA%3ABB';

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 900, height: 700 } });
page.on('pageerror', (e) => console.log('[pageerror]', e.message));

await page.addInitScript((link) => {
  window.__claimed = [];
  let joined = false;
  window.go = { main: { App: {
    GetAppInfo: async () => ({ version: 'test', error: '' }),
    // Role chosen, and it is not this machine that hosts: the second computer.
    GetLocalServer: async () => ({ chosen: true, host: false, running: false, url: '', firstRun: false }),
    GetSession: async () =>
      joined
        ? { loggedIn: true, serverUrl: 'http://192.168.1.20:8080', email: 'owner@pc.nexusvpn.local',
            deviceName: 'Laptop', publicKey: 'abc' }
        : { loggedIn: false, serverUrl: '' },
    GetStatus: async () => ({ connected: false, peers: [] }),
    GetStartWithSystem: async () => ({ supported: true, enabled: false }),
    ListNetworks: async () => (joined ? [{ id: 'n1', name: 'Home', memberCount: 1, deviceCount: 2 }] : []),
    TakePendingInvite: async () => '',
    ClaimPairing: async (l) => {
      window.__claimed.push(l);
      if (l !== link) throw new Error('that is not a NexusVPN pairing code');
      joined = true;
      return 'n1';
    },
  } } };
  window.runtime = { EventsOn: () => {}, EventsOff: () => {}, EventsOnMultiple: () => {},
    EventsOnce: () => {}, EventsEmit: () => {}, Quit: () => {}, WindowShow: () => {}, WindowHide: () => {} };
}, LINK);

await page.goto(base);
await page.waitForTimeout(900);

const before = await page.locator('body').innerText();
const passwordFields = await page.locator('input[type="password"]').count();
const emailFields = await page.locator('input[type="email"]').count();

const field = page.locator('textarea');
await field.fill(`  ${LINK}  `);
await page.getByRole('button', { name: /^join$/i }).click();
await page.waitForTimeout(900);

const claimed = await page.evaluate(() => window.__claimed);
const after = await page.locator('body').innerText();

if (process.argv[2]) await page.screenshot({ path: process.argv[2] });
await browser.close();
server.close();

console.log('asks for no password:', passwordFields === 0);
console.log('asks for no email:', emailFields === 0);
console.log('says where to get the code:', /hosting your network/i.test(before));
console.log('claimed:', JSON.stringify(claimed));

if (passwordFields > 0) throw new Error('FAIL: a second computer is still being asked for a password');
if (emailFields > 0) throw new Error('FAIL: a second computer is still being asked for an email');
if (!/hosting your network/i.test(before))
  throw new Error('FAIL: nothing tells the user where the link comes from');
if (claimed.length !== 1) throw new Error(`FAIL: ClaimPairing called ${claimed.length} times`);
// Pasted text picks up whitespace; the server sees a token that does not match.
if (claimed[0] !== LINK) throw new Error('FAIL: the pasted link was not trimmed before use');
if (!/Home/.test(after)) throw new Error('FAIL: joining did not land on the network');

// The account route must remain for a real deployment with real users.
console.log('PASS  a second computer joins by pasting a link, with no account');

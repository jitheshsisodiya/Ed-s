// The screen a hosting machine sees after signing out, with the Go bridge
// stubbed.
//
// Its account was generated, never typed, so a form asking for an email and a
// password is a door that locks behind you: the credentials it wants are ones
// nobody chose and nobody knows. There has to be a way back in that asks for
// nothing.
//
//   npm run build && node e2e/hostaccess.mjs [screenshot.png]
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
  window.__adopted = 0;
  let signedIn = false;
  window.go = { main: { App: {
    GetAppInfo: async () => ({ version: 'test', error: '' }),
    // Hosting, running, and an account already exists — the state after a
    // deliberate sign-out.
    GetLocalServer: async () => ({ chosen: true, host: true, running: true,
      url: 'https://127.0.0.1:8080', lanUrl: 'https://192.168.1.20:8080', firstRun: false }),
    GetSession: async () =>
      signedIn
        ? { loggedIn: true, serverUrl: 'https://192.168.1.20:8080',
            email: 'owner@pc.nexusvpn.local', deviceName: 'PC', publicKey: 'abc' }
        : { loggedIn: false, serverUrl: 'https://192.168.1.20:8080' },
    GetStatus: async () => ({ connected: false, peers: [] }),
    GetStartWithSystem: async () => ({ supported: true, enabled: false }),
    ListNetworks: async () => (signedIn ? [{ id: 'n1', name: 'Home', memberCount: 1, deviceCount: 1 }] : []),
    TakePendingInvite: async () => '',
    AdoptThisMachine: async () => { window.__adopted += 1; signedIn = true; },
  } } };
  window.runtime = { EventsOn: () => {}, EventsOff: () => {}, EventsOnMultiple: () => {},
    EventsOnce: () => {}, EventsEmit: () => {}, Quit: () => {}, WindowShow: () => {}, WindowHide: () => {} };
});

await page.goto(base);
await page.waitForTimeout(900);

const button = page.getByRole('button', { name: /use this machine/i });
const offered = (await button.count()) > 0;
if (offered) await button.click();
await page.waitForTimeout(900);

const adopted = await page.evaluate(() => window.__adopted);
const after = await page.locator('body').innerText();

if (process.argv[2]) await page.screenshot({ path: process.argv[2] });
await browser.close();
server.close();

console.log('offers a way back in:', offered);
console.log('adopted:', adopted);

if (!offered)
  throw new Error('FAIL: a hosting machine that signed out is asked for credentials nobody chose');
if (adopted !== 1) throw new Error(`FAIL: AdoptThisMachine called ${adopted} times`);
if (!/Home/.test(after)) throw new Error('FAIL: signing back in did not land on the network');
console.log('PASS  a hosting machine signs back into its own network with nothing typed');

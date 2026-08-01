// Drives the built desktop UI with the Go bridge stubbed, to prove the app
// leaves the signed-in screen when the session ends.
//
// Both cases here shipped broken. Signing out called Logout and stayed on the
// deck, showing networks nothing could act on; and a session that died
// mid-use put "sign in again to continue" on the one screen with no way to do
// it. Neither is visible to a type checker, and neither shows up in a unit
// test of any single component — the bug is that state nobody re-read.
//
//   npm run build && node e2e/session.mjs
//
// Playwright is not a dependency of this package: the sandbox provides it
// globally, and adding a browser download to every `npm install` for two
// tests is not a trade worth making. Set PLAYWRIGHT to point elsewhere.
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const { chromium } = await import(
  process.env.PLAYWRIGHT ?? '/opt/node22/lib/node_modules/playwright/index.mjs'
);

const DIST = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'dist');
const TYPES = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' };

if (!fs.existsSync(path.join(DIST, 'index.html'))) {
  throw new Error(`no build at ${DIST} — run "npm run build" first`);
}

const server = http.createServer((req, res) => {
  const url = req.url.split('?')[0];
  let file = path.join(DIST, url === '/' ? 'index.html' : url);
  if (!fs.existsSync(file) || fs.statSync(file).isDirectory()) {
    file = path.join(DIST, 'index.html');
  }
  // Content type comes from the resolved file, not the URL: a SPA fallback
  // serves index.html for paths with no extension, and guessing from the URL
  // labelled the page as a download.
  res.writeHead(200, { 'Content-Type': TYPES[path.extname(file)] ?? 'application/octet-stream' });
  res.end(fs.readFileSync(file));
});
await new Promise((r) => server.listen(0, r));
const base = `http://127.0.0.1:${server.address().port}`;

const browser = await chromium.launch();

/** Loads the app signed in, with a bridge that fails however the case needs. */
async function open({ createNetworkFails = false } = {}) {
  const page = await browser.newPage({ viewport: { width: 1000, height: 720 } });
  page.on('pageerror', (e) => console.log('[pageerror]', e.message));

  await page.addInitScript((fails) => {
    let loggedIn = true;
    window.__logoutCalls = 0;
    window.go = {
      main: {
        App: {
          GetAppInfo: async () => ({ version: 'test', error: '' }),
          GetLocalServer: async () => ({
            chosen: true, host: true, running: true,
            url: 'http://127.0.0.1:8080',
            lanUrl: 'http://192.168.1.20:8080',
            firstRun: false,
          }),
          GetSession: async () => ({
            loggedIn,
            serverUrl: 'http://192.168.1.20:8080',
            email: 'someone@example.com',
            deviceName: 'PC',
            publicKey: 'abc123',
          }),
          GetStatus: async () => ({ connected: false, peers: [] }),
          GetStartWithSystem: async () => ({ supported: true, enabled: false }),
          ListNetworks: async () =>
            loggedIn ? [{ id: 'n1', name: 'Office', memberCount: 1 }] : [],
          TakePendingInvite: async () => '',
          Logout: async () => {
            window.__logoutCalls++;
            loggedIn = false;
          },
          CreateNetwork: async () => {
            if (fails) throw 'rpc error: code = Unauthenticated desc = token is expired';
          },
          CopyToClipboard: async () => {},
        },
      },
    };
    window.runtime = {
      EventsOn: () => {}, EventsOff: () => {}, EventsOnMultiple: () => {},
      EventsOnce: () => {}, EventsEmit: () => {}, Quit: () => {},
      WindowShow: () => {}, WindowHide: () => {},
    };
  }, createNetworkFails);

  await page.goto(base);
  await page.waitForTimeout(900);
  if ((await page.getByText('Office', { exact: false }).count()) === 0) {
    throw new Error('setup failed: never reached the signed-in screen');
  }
  return page;
}

/** Asserts the app has left the deck for the sign-in screen. */
async function expectSignedOut(page, label) {
  const calls = await page.evaluate(() => window.__logoutCalls);
  const onDeck = await page.getByText('Office', { exact: false }).count();
  const onSignIn = await page.getByText('Sign in', { exact: false }).count();

  if (calls !== 1) throw new Error(`${label}: the dead session was not cleared`);
  if (onDeck > 0) throw new Error(`${label}: still showing networks after signing out`);
  if (onSignIn === 0) throw new Error(`${label}: never reached the sign-in screen`);
  console.log(`PASS  ${label}`);
}

// 1. Signing out on purpose.
{
  const page = await open();
  await page.getByText('System', { exact: true }).click();
  await page.waitForTimeout(150);
  await page.getByText('Sign out', { exact: true }).click();
  await page.waitForTimeout(900);
  await expectSignedOut(page, 'signing out leaves the signed-in screen');
  await page.close();
}

// 2. The session dying underneath ordinary work.
{
  const page = await open({ createNetworkFails: true });
  await page.getByText('Network', { exact: true }).click();
  await page.waitForTimeout(150);
  await page.getByText('Create network', { exact: false }).click();
  await page.waitForTimeout(300);
  await page.getByPlaceholder('Home Lab').fill('Anything');
  await page.getByText('Create', { exact: true }).last().click();
  await page.waitForTimeout(1200);
  await expectSignedOut(page, 'an expired session returns to sign-in');
  await page.close();
}

await browser.close();
server.close();

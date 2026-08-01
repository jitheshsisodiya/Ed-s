# NexusVPN Mobile

Flutter client for Android and iOS: sign in to a self-hosted control plane,
join virtual networks, and bring up a WireGuard tunnel to your peers.

## Scope boundary — read this first

Everything that runs in the Dart layer is implemented and tested:
authentication (including MFA), token storage and refresh, network and device
management, invite QR scanning and sharing, WireGuard key generation, peer
discovery, and assembling the tunnel configuration.

The part that **cannot be completed without real developer accounts** is the
native VPN extension:

- **Android** needs a `VpnService` bound with the `BIND_VPN_SERVICE`
  permission. The manifest declares it, and the `wireguard_flutter` plugin
  supplies the service.
- **iOS** needs a **Network Extension** (`NEPacketTunnelProvider`) in a
  separate app-extension target, signed with the
  `com.apple.developer.networking.networkextension` entitlement. Apple only
  issues that entitlement to a paid Apple Developer account, and it must be
  provisioned for a real bundle identifier.

  The Xcode project is **not committed**, because one generated without ever
  being opened in Xcode would be a project nobody has built. Run
  `flutter create --platforms=ios .` from `mobile/` to generate it, then add
  the extension target; `kNetworkExtensionBundleId` in
  `lib/core/vpn_controller.dart` is the identifier it must match.

So: the app builds, analyzes and tests cleanly here, and the tunnel
configuration it produces is correct — but **the tunnel has not been brought
up on a physical device in this repository**, because doing so requires
signing credentials that cannot exist in CI. Treat first-device bring-up as
an integration step you must perform yourself.

## Setup

Requires Flutter 3.35+ (Dart 3.9+).

```bash
cd mobile
flutter pub get
flutter analyze
flutter test
flutter run           # with a device or emulator attached
```

Point the app at your control plane on the sign-in screen, or change
`kDefaultServerUrl` in `lib/core/api_client.dart`.

## Building an APK

```bash
flutter build apk --release        # one APK, every architecture
flutter build apk --split-per-abi  # smaller, one per architecture
```

The result lands in `build/app/outputs/flutter-apk/`. Sideloading it needs
"Install unknown apps" allowed for whatever app is doing the installing —
Files, Chrome, Drive — which Android asks about on first attempt.

### Signing

An APK Android will install has to be signed. With no configuration the
build falls back to the debug key, which works for sideloading onto your own
device and is refused by Play.

For anything you intend to update later, make your own key and keep it. The
signature is the app's identity: Android refuses an update signed by a
different key, and there is no recovery — a lost keystore means the next
version can only ship under a new package name, as a separate app that
cannot see the first one's data.

```bash
keytool -genkeypair -v -keystore ~/nexusvpn-release.jks \
  -keyalg RSA -keysize 4096 -validity 10950 -alias nexusvpn
```

Then create `android/key.properties` — which is gitignored, and must stay
that way:

```properties
storeFile=/absolute/path/to/nexusvpn-release.jks
storePassword=...
keyAlias=nexusvpn
keyPassword=...
```

The Gradle build picks it up automatically and uses the debug key when it is
absent, so a fresh clone still builds.

### A note on the server address

The app talks to your control plane over the network, so `localhost` is
whatever the phone itself is, not the machine running the server. On a phone,
point it at the LAN address the desktop app shows you — `http://192.168.x.x:8080`.
That also means the phone has to be on the same network as the server, or the
server has to be reachable from outside it.

## Layout

```
lib/core/          api_client (REST + refresh-on-401), models, secure storage,
                   auth and VPN controllers, preferences, connection quality,
                   router
lib/screens/       home, login, register, MFA, forgot password, networks,
                   network detail, devices, settings
lib/widgets/       deck: reactor core, scrambler, signal bars, network
                   tree, device sheet
test/              model (de)serialization, API client, connection quality,
                   widget and layout tests
android/           platform project: VPN, camera and notification
                   permissions, the launcher icon, and the backup opt-out
                   below
tool/icongen/      regenerates the launcher icons from the same mark the
                   desktop tray draws
```

## What the app shows

The app opens on the **deck**: an identity strip carrying one round control
and this device's address, then a tree of every network you belong to with
its machines nested underneath. It is the same structure as the desktop app,
sized for a thumb — you open this to find one machine among a handful and do
something with it, and a tree puts every candidate on screen with no
navigation.

Where the desktop uses right-click, this uses a tap or long press onto an
action sheet carrying the actions *and* the properties. On a phone the
reason you opened it is almost always to copy an address, and a menu that
leads to a menu puts that two taps away.

Connection health is carried twice: four signal bars for the glance, and the
millisecond figure in its own column for anyone who wants it. Both use the
thresholds in `lib/core/connection_quality.dart`, a deliberate copy of
`describeQuality` in `client/agent/agent.go` pinned to the same test table —
so four bars here and "Excellent" on the desktop cannot disagree.

The four connection states — offline, linking, online, tunnel lost — differ
by motion as well as by colour, since a hue-only difference fails for the
eight percent of men who cannot reliably separate red from green. Idle is
still, linking sweeps, online breathes, lost pulses hard and off-rhythm.

## Design

`lib/core/deck_theme.dart` holds the palette, type and the `DeckPhase` enum.
It restates `desktop/frontend/src/theme.css` because the two platforms cannot
share a stylesheet; keeping one short file per platform is what makes "are
these the same product" answerable by reading rather than grepping for hex
codes.

The app is **dark only**, deliberately. The accent colour carries state, and
the glows that make that legible have nothing to glow against on a light
ground. A theme switch here would not be a preference, it would be a second
design.

## Security

- The WireGuard **private key is generated on-device and never transmitted**;
  only the public key is registered with the control plane.
- Tokens and the private key live in `flutter_secure_storage`, which is
  backed by the Android Keystore and the iOS Keychain. The keychain item uses
  `first_unlock_this_device`, so the VPN extension can read it after a reboot
  while still requiring the device to have been unlocked once — and the
  `ThisDeviceOnly` half keeps it out of iCloud Keychain *and* out of
  encrypted device backups.
- **Nothing this app stores is backed up or transferred.** The Android
  manifest sets `allowBackup="false"` and excludes every domain from both
  cloud backup and device transfer (`res/xml/data_extraction_rules.xml`).
  The encryption key lives in the Keystore and is never part of a backup, so
  a restored copy is ciphertext nobody can read anyway; shipping it would
  gain a user nothing and put an encrypted copy of their credentials
  somewhere they did not ask for it. A restored phone signs in again and
  generates a fresh device key — which is also what you want, since a device
  key that can be cloned onto a second handset identifies two devices.
- **Plain HTTP is accepted only for an address that cannot leave the local
  network** — the private and loopback ranges, `localhost`, and mDNS `.local`
  names. Everything else must be `https`, because a server on the internet
  carries sign-in credentials across networks nobody involved controls. This
  is enforced in `lib/core/server_url.dart`, at the one point where a server
  address enters the app, and not by Android's network security config: that
  file matches hostnames rather than address ranges, so the platform flag can
  only say "all cleartext" or "none". A hostname is never resolved to decide
  this — where a name points when somebody types it is not where it points
  when the request goes out.
- Screen-reader labels carry what colour and motion cannot: the connect
  control announces its state in words, and status pills are uppercased for
  the eye but announced in natural casing, because assistive tech spells
  all-caps words out letter by letter.
- The API client refreshes an expired access token exactly once per failure,
  de-duplicating concurrent refreshes, and signals session expiry so the app
  returns to sign-in rather than retrying forever.
- Relayed traffic stays end-to-end encrypted: a relay only ever forwards
  ciphertext (see `relay/README.md`).

## Tests

```bash
flutter test
```

91 tests, no server needed — the HTTP layer is mocked.

They cover model (de)serialization against the exact shapes in
`api/openapi.yaml`; the API client's behaviour (bearer-token attachment, the
MFA challenge, refresh-once-then-retry on 401, session-expiry signalling,
error-envelope surfacing); the invite-code parser, case for case against the
Go one in `client/agent/invite.go`, so a link one platform generates is a
link the other accepts; which addresses may be reached over plain HTTP; and a
layout pass at 320×568 asserting the deck does not overflow in any of its
four states.

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

## Layout

```
lib/core/          api_client (REST + refresh-on-401), models, secure storage,
                   auth and VPN controllers, router
lib/screens/       login, register, MFA, forgot password, networks,
                   network detail, devices, settings
lib/widgets/       shared UI pieces
test/              model (de)serialization and API client tests
android/ ios/      platform projects, including VPN permissions/entitlements
```

## Security

- The WireGuard **private key is generated on-device and never transmitted**;
  only the public key is registered with the control plane.
- Tokens and the private key live in `flutter_secure_storage`, which is
  backed by the Android Keystore and the iOS Keychain. The keychain item uses
  `first_unlock` accessibility, so the VPN extension can read it after a
  reboot while still requiring the device to have been unlocked once, and it
  is never synchronised to iCloud.
- The API client refreshes an expired access token exactly once per failure,
  de-duplicating concurrent refreshes, and signals session expiry so the app
  returns to sign-in rather than retrying forever.
- Relayed traffic stays end-to-end encrypted: a relay only ever forwards
  ciphertext (see `relay/README.md`).

## Tests

```bash
flutter test
```

17 tests cover model (de)serialization against the exact shapes in
`api/openapi.yaml`, and the API client's behaviour: bearer-token attachment,
the MFA challenge, refresh-once-then-retry on 401, session-expiry signalling,
and error-envelope surfacing. The HTTP layer is mocked, so no server is
needed.

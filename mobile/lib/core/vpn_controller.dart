import 'dart:async';
import 'dart:convert';

import 'package:cryptography/cryptography.dart';
import 'package:flutter/foundation.dart';
import 'package:wireguard_flutter/wireguard_flutter.dart';
// The plugin's entrypoint re-exports only VpnStage, so the interface type
// used for dependency injection (and faking in tests) comes from here.
import 'package:wireguard_flutter/wireguard_flutter_platform_interface.dart';

import 'api_client.dart';
import 'app_exception.dart';
import 'models/models.dart';
import 'secure_storage.dart';

/// Simplified, UI-facing connection state. Collapses the plugin's more
/// granular [VpnStage] values (waiting_connection, authenticating,
/// preparing, exiting, denied, ...) into the four states product/UI cares
/// about, plus `error` for anything that needs surfacing to the user.
enum AppVpnState { disconnected, connecting, connected, reconnecting, error }

/// WireGuard interface name used for every tunnel this app brings up.
const String kWgInterfaceName = 'nexus0';

/// Default WireGuard UDP listen port assumed for peers, since the
/// `Device` schema in api/openapi.yaml does not (yet) expose a per-peer
/// listen port. This mirrors the port `client/` and `desktop/` use by
/// default; a self-hosted deployment that changes it will need a schema
/// addition to communicate the real port to clients.
const int kDefaultWireGuardPort = 51820;

/// The iOS Network Extension's bundle identifier. Must match the
/// `NetworkExtension` target configured in Xcode (see ios/README notes) -
/// it cannot be activated without real Apple Developer provisioning, but
/// the value is wired through correctly so that step is the only one left.
const String kNetworkExtensionBundleId = 'com.nexusvpn.mobile.network-extension';

/// Wraps the `wireguard_flutter` plugin: generates/loads this device's
/// WireGuard keypair, assembles a wg-quick-style config from the peer list
/// returned by `GET /devices/{id}/peers`, and drives the plugin's
/// start/stop/status API. Exposes a simplified connection-state stream for
/// the UI to bind to.
///
/// Scope note: this class talks to the plugin's documented Dart API only.
/// The actual packet-tunnel implementation on Android (`VpnService`) and
/// iOS (`NEPacketTunnelProvider` running in a Network Extension target)
/// lives inside the plugin's native code / the platform extension target,
/// which requires real signing entitlements to activate - see
/// mobile/README.md.
class VpnController extends ChangeNotifier {
  VpnController({
    required this.api,
    SecureStorage? storage,
    WireGuardFlutterInterface? plugin,
  })  : _storage = storage ?? SecureStorage.instance,
        _plugin = plugin ?? WireGuardFlutter.instance;

  final ApiClient api;
  final SecureStorage _storage;
  final WireGuardFlutterInterface _plugin;

  AppVpnState state = AppVpnState.disconnected;
  String? lastError;
  Network? _activeNetwork;
  Device? _activeDevice;
  List<Device> lastPeers = const [];

  StreamSubscription<VpnStage>? _stageSub;
  Timer? _heartbeatTimer;
  bool _initialized = false;

  final _stateController = StreamController<AppVpnState>.broadcast();

  /// Stream of simplified connection states for widgets that prefer
  /// StreamBuilder over Provider/ChangeNotifier.
  Stream<AppVpnState> get stateStream => _stateController.stream;

  Network? get activeNetwork => _activeNetwork;
  Device? get activeDevice => _activeDevice;

  Future<void> _ensureInitialized() async {
    if (_initialized) return;
    await _plugin.initialize(interfaceName: kWgInterfaceName);
    _stageSub = _plugin.vpnStageSnapshot.listen(_onStage);
    _initialized = true;
  }

  void _onStage(VpnStage stage) {
    switch (stage) {
      case VpnStage.connected:
        _setState(AppVpnState.connected);
        _startHeartbeat();
        break;
      case VpnStage.connecting:
      case VpnStage.preparing:
      case VpnStage.authenticating:
      case VpnStage.waitingConnection:
        _setState(AppVpnState.connecting);
        break;
      case VpnStage.reconnect:
        _setState(AppVpnState.reconnecting);
        break;
      case VpnStage.disconnecting:
      case VpnStage.exiting:
        _setState(AppVpnState.disconnected);
        _stopHeartbeat();
        break;
      case VpnStage.disconnected:
      case VpnStage.noConnection:
        _setState(AppVpnState.disconnected);
        _stopHeartbeat();
        break;
      case VpnStage.denied:
        lastError = 'VPN permission was denied by the OS.';
        _setState(AppVpnState.error);
        _stopHeartbeat();
        break;
    }
  }

  void _setState(AppVpnState next) {
    state = next;
    _stateController.add(next);
    notifyListeners();
  }

  // ---------------------------------------------------------------------
  // Keypair management
  // ---------------------------------------------------------------------

  /// Returns this device's persisted WireGuard keypair (base64 X25519),
  /// generating and storing a fresh one on first use. Rotation discards
  /// the old key and generates a new one - the caller is responsible for
  /// re-registering the new public key with the backend.
  Future<({String privateKey, String publicKey})> ensureKeypair({
    bool rotate = false,
  }) async {
    if (!rotate) {
      final existingPriv = await _storage.readDevicePrivateKey();
      final existingPub = await _storage.readDevicePublicKey();
      if (existingPriv != null && existingPub != null) {
        return (privateKey: existingPriv, publicKey: existingPub);
      }
    }
    final algorithm = X25519();
    final keyPair = await algorithm.newKeyPair();
    final privBytes = await keyPair.extractPrivateKeyBytes();
    final pub = await keyPair.extractPublicKey();
    final privateKey = base64Encode(privBytes);
    final publicKey = base64Encode(pub.bytes);
    await _storage.saveDeviceKeypair(
      privateKey: privateKey,
      publicKey: publicKey,
    );
    return (privateKey: privateKey, publicKey: publicKey);
  }

  // ---------------------------------------------------------------------
  // Config assembly
  // ---------------------------------------------------------------------

  /// Builds a wg-quick-style config string for [selfDevice] on [network],
  /// with one `[Peer]` block per entry in [peers].
  String buildConfig({
    required String privateKey,
    required Device selfDevice,
    required Network network,
    required List<Device> peers,
  }) {
    final buffer = StringBuffer();
    buffer.writeln('[Interface]');
    buffer.writeln('PrivateKey = $privateKey');
    final address = (selfDevice.virtualIp != null &&
            selfDevice.virtualIp!.isNotEmpty)
        ? '${selfDevice.virtualIp}/32'
        : network.cidr;
    buffer.writeln('Address = $address');
    if (network.dnsServers.isNotEmpty) {
      buffer.writeln('DNS = ${network.dnsServers.join(', ')}');
    }
    buffer.writeln();

    for (final peer in peers) {
      if (peer.publicKey.isEmpty) continue;
      buffer.writeln('[Peer]');
      buffer.writeln('PublicKey = ${peer.publicKey}');
      final allowedIp = (peer.virtualIp != null && peer.virtualIp!.isNotEmpty)
          ? '${peer.virtualIp}/32'
          : network.cidr;
      buffer.writeln('AllowedIPs = $allowedIp');
      if (peer.lastPublicIp != null && peer.lastPublicIp!.isNotEmpty) {
        buffer.writeln(
          'Endpoint = ${peer.lastPublicIp}:$kDefaultWireGuardPort',
        );
      }
      buffer.writeln('PersistentKeepalive = 25');
      buffer.writeln();
    }
    return buffer.toString().trim();
  }

  // ---------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------

  /// Fetches the current peer list, (re)generates a keypair if needed,
  /// assembles the config and starts the tunnel.
  Future<void> connect({
    required Network network,
    required Device selfDevice,
  }) async {
    lastError = null;
    _setState(AppVpnState.connecting);
    try {
      await _ensureInitialized();
      final keys = await ensureKeypair();
      final peers = await api.getPeers(selfDevice.id);
      lastPeers = peers;
      _activeNetwork = network;
      _activeDevice = selfDevice;

      final config = buildConfig(
        privateKey: keys.privateKey,
        selfDevice: selfDevice,
        network: network,
        peers: peers,
      );

      final primaryEndpoint = peers
          .where((p) => p.lastPublicIp != null && p.lastPublicIp!.isNotEmpty)
          .map((p) => p.lastPublicIp!)
          .firstOrNull;

      await _plugin.startVpn(
        serverAddress: primaryEndpoint ?? (selfDevice.virtualIp ?? network.cidr),
        wgQuickConfig: config,
        providerBundleIdentifier: kNetworkExtensionBundleId,
      );
    } on ApiException catch (e) {
      lastError = e.message;
      _setState(AppVpnState.error);
      rethrow;
    } catch (e) {
      lastError = 'Failed to start VPN tunnel: $e';
      _setState(AppVpnState.error);
      rethrow;
    }
  }

  Future<void> disconnect() async {
    try {
      await _ensureInitialized();
      await _plugin.stopVpn();
    } catch (e) {
      lastError = 'Failed to stop VPN tunnel: $e';
    } finally {
      _stopHeartbeat();
      _setState(AppVpnState.disconnected);
    }
  }

  Future<AppVpnState> refreshState() async {
    await _ensureInitialized();
    await _plugin.refreshStage();
    final stage = await _plugin.stage();
    _onStage(stage);
    return state;
  }

  void _startHeartbeat() {
    _stopHeartbeat();
    final device = _activeDevice;
    if (device == null) return;
    _heartbeatTimer = Timer.periodic(const Duration(seconds: 25), (_) async {
      try {
        await api.heartbeat(device.id);
      } catch (_) {
        // Best-effort; connectivity monitoring will surface real failures
        // via the plugin's own stage stream.
      }
    });
  }

  void _stopHeartbeat() {
    _heartbeatTimer?.cancel();
    _heartbeatTimer = null;
  }

  @override
  void dispose() {
    _stageSub?.cancel();
    _stopHeartbeat();
    _stateController.close();
    super.dispose();
  }
}

extension _FirstOrNull<T> on Iterable<T> {
  T? get firstOrNull => isEmpty ? null : first;
}

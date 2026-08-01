import 'package:connectivity_plus/connectivity_plus.dart';

import 'server_url.dart';

/// Why a server on a private address might not be answering.
///
/// "Could not reach 192.168.31.91, check the address" is true and unhelpful
/// when the address is right and the phone is on mobile data: a 192.168
/// address exists only on the network it belongs to, so from a mobile
/// connection there is nothing there to answer and the request simply times
/// out after twenty seconds. Somebody reading that message checks the address
/// again, finds it correct, and tries again.
///
/// So the commonest cause is named instead of described.
class Reachability {
  const Reachability._();

  /// Returns a sentence explaining an unreachable host, or null if there is
  /// nothing specific to say and the general message should stand.
  static Future<String?> explain(String host) async {
    List<ConnectivityResult> connection;
    try {
      connection = await Connectivity().checkConnectivity();
    } on Object {
      // A platform that will not answer tells us nothing, and a guess here
      // would be worse than the general message.
      return null;
    }
    return explainWith(host: host, connection: connection);
  }

  /// Whether a failure looks like plain HTTP sent to a server speaking TLS.
  ///
  /// The server hangs up mid-handshake, which reaches Dart as a connection
  /// closed before any headers arrived — a sentence that describes the
  /// symptom and hides the cause. An address stored before the server moved
  /// to TLS produces exactly this, and re-typing the same address does not
  /// fix it.
  static String? explainScheme({required String serverUrl, required String error}) {
    if (!serverUrl.toLowerCase().startsWith('http://')) return null;
    final lower = error.toLowerCase();
    final looksLikeTLS = lower.contains('closed before full header') ||
        lower.contains('connection reset') ||
        lower.contains('handshake');
    if (!looksLikeTLS) return null;

    final asHttps = serverUrl.replaceFirst(RegExp(r'^http://', caseSensitive: false), 'https://');
    return 'That server speaks encrypted connections now, and this address '
        'does not. Try $asHttps instead, or take a fresh pairing code from '
        'the machine running NexusVPN, which carries the right address with '
        'it.';
  }

  /// The decision, separated from asking the platform so it can be tested.
  static String? explainWith({
    required String host,
    required List<ConnectivityResult> connection,
  }) {
    // A public address is reachable from anywhere or from nowhere, and which
    // network this phone is on has no bearing on it.
    if (!isPrivateHost(host)) return null;

    if (connection.isEmpty || connection.contains(ConnectivityResult.none)) {
      return 'This phone has no network connection at all.';
    }
    if (_onSameNetworkAs(connection)) {
      // On Wi-Fi and still nothing there: a real problem, and not one this
      // can diagnose. The general message already says what to check.
      return null;
    }
    return 'This phone is on mobile data, and $host is an address that only '
        'exists on your home network — there is nothing at it from here. '
        'Connect to the same Wi-Fi as the machine running NexusVPN.';
  }

  /// Whether this connection could carry traffic to a machine on the same
  /// local network.
  ///
  /// A VPN counts: something already tunnelling may well reach the address,
  /// and claiming otherwise would be a confident wrong answer.
  static bool _onSameNetworkAs(List<ConnectivityResult> connection) =>
      connection.contains(ConnectivityResult.wifi) ||
      connection.contains(ConnectivityResult.ethernet) ||
      connection.contains(ConnectivityResult.vpn);
}

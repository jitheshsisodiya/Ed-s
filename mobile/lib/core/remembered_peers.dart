import 'dart:convert';

import 'package:shared_preferences/shared_preferences.dart';

import 'models/models.dart';

/// The peer list from the last time this phone reached the control plane.
///
/// A phone away from home can reach its peers — NAT traversal handles that —
/// but not necessarily the machine that hands out the peer list, which needs
/// an inbound connection the router at home may refuse. Everything WireGuard
/// requires was already known the last time this device connected, so
/// insisting the control plane repeat it is what turns "away from home" into
/// "does not work".
///
/// Stored in ordinary preferences rather than secure storage: a peer's public
/// key and address are what this device announces to the network anyway.
/// Nothing here is a secret, and the private key it is used with stays where
/// it was.
class RememberedPeers {
  const RememberedPeers._();

  static String _key(String deviceId) => 'nexusvpn.peers.$deviceId';
  static String _savedAtKey(String deviceId) => 'nexusvpn.peers.$deviceId.at';

  /// Records the peers for a device.
  static Future<void> save(String deviceId, List<Device> peers) async {
    if (deviceId.isEmpty) return;
    try {
      final store = await SharedPreferences.getInstance();
      await store.setString(
        _key(deviceId),
        jsonEncode(peers.map((p) => p.toJson()).toList()),
      );
      await store.setInt(
        _savedAtKey(deviceId),
        DateTime.now().millisecondsSinceEpoch,
      );
    } on Object {
      // Failing to remember is not a reason to fail to connect. It only
      // costs the next connection its fallback.
    }
  }

  /// Returns the peers recorded for a device, or an empty list.
  static Future<List<Device>> load(String deviceId) async {
    if (deviceId.isEmpty) return const [];
    try {
      final store = await SharedPreferences.getInstance();
      final blob = store.getString(_key(deviceId));
      if (blob == null || blob.isEmpty) return const [];

      final decoded = jsonDecode(blob);
      if (decoded is! List) return const [];
      return decoded
          .whereType<Map<String, dynamic>>()
          .map(Device.fromJson)
          .toList();
    } on Object {
      // Unreadable is the same as absent: connect without a fallback rather
      // than refuse to connect at all.
      return const [];
    }
  }

  /// How old the record is, so somebody can be told they are connecting on
  /// information that may have moved on.
  static Future<Duration?> age(String deviceId) async {
    if (deviceId.isEmpty) return null;
    try {
      final store = await SharedPreferences.getInstance();
      final at = store.getInt(_savedAtKey(deviceId));
      if (at == null) return null;
      return DateTime.now()
          .difference(DateTime.fromMillisecondsSinceEpoch(at));
    } on Object {
      return null;
    }
  }

  /// Describes an age in words, for a log line or a status.
  static String describe(Duration? age) {
    if (age == null) return 'unknown age';
    if (age.inHours < 1) return '${age.inMinutes} minutes old';
    if (age.inDays < 2) return '${age.inHours} hours old';
    return '${age.inDays} days old';
  }
}

import 'package:multicast_dns/multicast_dns.dart';

import 'server_url.dart';

/// A NexusVPN control plane found on this network.
class DiscoveredServer {
  const DiscoveredServer({
    required this.name,
    required this.url,
    required this.fingerprint,
  });

  /// What to show somebody choosing between several.
  final String name;

  /// Where to point the app.
  final String url;

  /// The certificate that server presents, carried in the announcement so a
  /// device learns which one to expect at the same moment it learns where to
  /// look.
  final String fingerprint;
}

/// Finds control planes announcing themselves on the local network.
///
/// This is the answer to addresses going stale. Every address written down
/// somewhere — typed in, carried in a pairing code, remembered in storage —
/// is right until the router hands that machine a different one, which it
/// will, because addresses are leases. Then a phone that paired last week
/// cannot find the server, and nothing about the failure says "the address
/// changed": it just stops working.
///
/// Asking the network where something is has no expiry date.
class Discovery {
  const Discovery._();

  /// The service NexusVPN announces itself as, matching backend/local.
  static const String serviceType = '_nexusvpn._tcp.local';

  /// Looks for servers, returning an empty list if the network will not carry
  /// the question — normal on guest and enterprise wireless, which commonly
  /// filter multicast, and not a failure worth reporting.
  static Future<List<DiscoveredServer>> find({
    Duration timeout = const Duration(seconds: 4),
  }) async {
    final client = MDnsClient();
    final found = <String, DiscoveredServer>{};

    try {
      await client.start();

      await for (final ptr in client
          .lookup<PtrResourceRecord>(
            ResourceRecordQuery.serverPointer(serviceType),
          )
          .timeout(timeout, onTimeout: (sink) => sink.close())) {
        await for (final srv in client.lookup<SrvResourceRecord>(
          ResourceRecordQuery.service(ptr.domainName),
        )) {
          final fingerprint = await _fingerprintOf(client, ptr.domainName);
          final address = await _addressOf(client, srv.target);
          if (address == null) continue;

          final url = 'https://$address:${srv.port}';
          found[url] = DiscoveredServer(
            name: _instanceName(ptr.domainName),
            url: url,
            fingerprint: fingerprint,
          );
        }
      }
    } on Object {
      // A platform that refuses multicast tells us nothing useful, and the
      // address still works by other means.
      return found.values.toList();
    } finally {
      client.stop();
    }

    return found.values.toList();
  }

  /// Reads the certificate out of the announcement's text records.
  static Future<String> _fingerprintOf(MDnsClient client, String domain) async {
    try {
      await for (final txt in client.lookup<TxtResourceRecord>(
        ResourceRecordQuery.text(domain),
      )) {
        for (final line in txt.text.split(RegExp(r'[\r\n]+'))) {
          final trimmed = line.trim();
          if (trimmed.startsWith('fp=')) return trimmed.substring(3).trim();
        }
      }
    } on Object {
      // An announcement with no readable text is still an announcement. The
      // empty value means ordinary verification, not no verification.
    }
    return '';
  }

  /// Resolves the announced host to an address something can connect to.
  ///
  /// Only addresses on a local network are accepted. An announcement is
  /// unauthenticated — anything on the network can send one — so one claiming
  /// a public address is either broken or somebody redirecting devices
  /// somewhere of their choosing, and neither is worth following.
  static Future<String?> _addressOf(MDnsClient client, String target) async {
    try {
      await for (final ip in client.lookup<IPAddressResourceRecord>(
        ResourceRecordQuery.addressIPv4(target),
      )) {
        final address = ip.address.address;
        if (isPrivateHost(address)) return address;
      }
    } on Object {
      return null;
    }
    return null;
  }

  /// Turns `NexusVPN on desk._nexusvpn._tcp.local` into `NexusVPN on desk`.
  static String _instanceName(String domain) {
    final cut = domain.indexOf('._');
    final name = cut > 0 ? domain.substring(0, cut) : domain;
    return name.trim().isEmpty ? domain : name.trim();
  }
}

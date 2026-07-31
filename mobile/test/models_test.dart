import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/models/models.dart';

void main() {
  group('User', () {
    test('parses the API representation', () {
      final user = User.fromJson({
        'id': 'u-1',
        'email': 'alice@example.com',
        'displayName': 'Alice',
        'mfaEnabled': true,
        'status': 'active',
        'createdAt': '2026-01-02T03:04:05Z',
      });

      expect(user.id, 'u-1');
      expect(user.email, 'alice@example.com');
      expect(user.displayName, 'Alice');
      expect(user.mfaEnabled, isTrue);
      expect(user.status, 'active');
    });

    test('tolerates omitted optional fields', () {
      final user = User.fromJson({'id': 'u-2', 'email': 'bob@example.com'});

      expect(user.id, 'u-2');
      expect(user.mfaEnabled, isFalse);
    });
  });

  group('TokenPair', () {
    test('parses an issued pair', () {
      final tokens = TokenPair.fromJson({
        'accessToken': 'access-1',
        'refreshToken': 'refresh-1',
        'expiresIn': 900,
      });

      expect(tokens.accessToken, 'access-1');
      expect(tokens.refreshToken, 'refresh-1');
      expect(tokens.expiresIn, 900);
      expect(tokens.mfaRequired, isFalse);
    });

    test('represents the MFA challenge, which carries no tokens', () {
      final tokens = TokenPair.fromJson({'mfaRequired': true});

      expect(tokens.mfaRequired, isTrue);
      // The server withholds tokens until the second factor is supplied.
      expect(tokens.accessToken, isNull);
      expect(tokens.hasTokens, isFalse);
    });
  });

  group('Network', () {
    test('parses a network', () {
      final network = Network.fromJson({
        'id': 'n-1',
        'name': 'Home Lab',
        'description': 'my lab',
        'cidr': '10.77.0.0/24',
        'dnsServers': ['1.1.1.1', '8.8.8.8'],
        'role': 'owner',
        'memberCount': 3,
        'deviceCount': 5,
        'inviteCode': 'ABC123',
        'createdAt': '2026-01-02T03:04:05Z',
      });

      expect(network.name, 'Home Lab');
      expect(network.cidr, '10.77.0.0/24');
      expect(network.dnsServers, ['1.1.1.1', '8.8.8.8']);
      expect(network.role, 'owner');
      expect(network.memberCount, 3);
      expect(network.inviteCode, 'ABC123');
    });

    test('defaults counts and lists when absent', () {
      final network = Network.fromJson({'id': 'n-2', 'name': 'Bare', 'cidr': '10.1.0.0/24'});

      expect(network.dnsServers, isEmpty);
      expect(network.memberCount, 0);
      expect(network.deviceCount, 0);
    });
  });

  group('Device', () {
    test('parses a device with live counters', () {
      final device = Device.fromJson({
        'id': 'd-1',
        'name': 'laptop',
        'os': 'linux',
        'osVersion': '6.1',
        'publicKey': 'pubkey',
        'virtualIp': '10.77.0.2',
        'lastPublicIp': '203.0.113.5',
        'status': 'online',
        'natType': 'port_restricted_cone',
        'bytesSent': 1024,
        'bytesReceived': 2048,
        'lastSeenAt': '2026-01-02T03:04:05Z',
      });

      expect(device.name, 'laptop');
      expect(device.virtualIp, '10.77.0.2');
      expect(device.status, 'online');
      expect(device.isOnline, isTrue);
      expect(device.bytesSent, 1024);
      expect(device.bytesReceived, 2048);
    });

    test('treats a non-online status as offline', () {
      final device = Device.fromJson({
        'id': 'd-2',
        'name': 'phone',
        'publicKey': 'k',
        'virtualIp': '10.77.0.3',
        'status': 'offline',
      });

      expect(device.isOnline, isFalse);
    });
  });

  group('Member', () {
    test('parses a member', () {
      final member = Member.fromJson({
        'userId': 'u-1',
        'email': 'alice@example.com',
        'displayName': 'Alice',
        'role': 'admin',
        'joinedAt': '2026-01-02T03:04:05Z',
      });

      expect(member.userId, 'u-1');
      expect(member.role, 'admin');
    });
  });

  group('DashboardStats', () {
    test('parses aggregate stats', () {
      final stats = DashboardStats.fromJson({
        'activeUsers': 4,
        'activeNetworks': 2,
        'onlineDevices': 7,
        'totalDevices': 11,
        'relayBandwidthBytes': 123456,
        'p2pConnectionRatio': 0.75,
      });

      expect(stats.activeUsers, 4);
      expect(stats.onlineDevices, 7);
      expect(stats.relayBandwidthBytes, 123456);
      expect(stats.p2pConnectionRatio, closeTo(0.75, 1e-9));
    });
  });
}

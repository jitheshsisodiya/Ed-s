import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/server_url.dart';

void main() {
  group('http is accepted only where it cannot leave the local network', () {
    for (final host in [
      '192.168.1.20',
      '192.168.0.1',
      '10.0.0.5',
      '10.255.255.255',
      '172.16.0.1',
      '172.31.255.254',
      '127.0.0.1',
      '169.254.10.1',
      '100.64.0.1',
      'localhost',
      'nexus.local',
    ]) {
      test('http://$host is allowed', () {
        expect(validateServerUrl('http://$host:8080'), 'http://$host:8080');
      });
    }
  });

  group('http is refused anywhere it would cross the internet', () {
    for (final host in [
      'api.nexusvpn.example.com',
      '8.8.8.8',
      '172.32.0.1', // just past 172.16.0.0/12
      '172.15.255.255', // just before it
      '11.0.0.1', // just past 10.0.0.0/8
      '192.169.0.1', // just past 192.168.0.0/16
      '100.128.0.1', // just past 100.64.0.0/10
      '169.253.0.1', // just before link-local
    ]) {
      test('http://$host is refused', () {
        expect(
          () => validateServerUrl('http://$host:8080'),
          throwsA(isA<InsecureServerUrl>()),
        );
      });
    }
  });

  test('https is accepted anywhere, including on the local network', () {
    expect(validateServerUrl('https://api.example.com'), 'https://api.example.com');
    expect(validateServerUrl('https://192.168.1.20'), 'https://192.168.1.20');
  });

  test('a trailing slash is removed, however many there are', () {
    expect(validateServerUrl('http://192.168.1.20:8080/'), 'http://192.168.1.20:8080');
    expect(validateServerUrl('http://192.168.1.20:8080///'), 'http://192.168.1.20:8080');
  });

  test('surrounding whitespace is forgiven', () {
    expect(validateServerUrl('  http://10.0.0.1:8080  '), 'http://10.0.0.1:8080');
  });

  group('what is not an address at all', () {
    test('empty', () {
      expect(() => validateServerUrl('   '), throwsA(isA<FormatException>()));
    });
    test('no scheme', () {
      expect(
        () => validateServerUrl('192.168.1.20:8080'),
        throwsA(isA<FormatException>()),
      );
    });
    test('a scheme we do not speak', () {
      expect(
        () => validateServerUrl('ftp://192.168.1.20'),
        throwsA(isA<FormatException>()),
      );
    });
  });

  group('addresses that only look private', () {
    // A hostname is not resolved, because where it points when somebody types
    // it is not where it will point when the request goes out.
    test('a name that merely mentions a private address is not private', () {
      expect(isPrivateHost('192.168.1.20.evil.com'), isFalse);
    });

    // Leading zeros are read as octal by some resolvers and as decimal by
    // others, so an address written that way is refused rather than guessed.
    test('octal-looking octets are not treated as private', () {
      expect(isPrivateHost('010.0.0.1'), isFalse);
    });

    test('a short form is not expanded', () {
      expect(isPrivateHost('10.1'), isFalse);
    });

    test('out-of-range octets are not an address', () {
      expect(isPrivateHost('192.168.1.999'), isFalse);
    });
  });

  group('IPv6', () {
    test('loopback and local ranges are private', () {
      expect(isPrivateHost('::1'), isTrue);
      expect(isPrivateHost('fe80::1'), isTrue);
      expect(isPrivateHost('fd00::1'), isTrue);
      expect(isPrivateHost('fc00::1'), isTrue);
    });
    test('a link-local address keeps its zone', () {
      expect(isPrivateHost('fe80::1%wlan0'), isTrue);
    });
    test('a global address is not private', () {
      expect(isPrivateHost('2001:4860:4860::8888'), isFalse);
    });
  });
}

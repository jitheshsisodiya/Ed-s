import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/api_client.dart';

/// What gets stored is the machine's address, because that is what a pairing
/// code carries, what a discovery announcement carries, and what a person can
/// be shown and check. The version prefix belongs to the API, not the address.
///
/// This did not exist, and every request the phone made went to a path without
/// the prefix. The server answered 404 and the app reported it as a rejected
/// pairing code — a wrong explanation for a request that never reached the
/// endpoint it was aimed at.
void main() {
  test('an address from a pairing code gains the API prefix', () {
    expect(
      apiBaseUrl('https://192.168.31.91:8080'),
      'https://192.168.31.91:8080/api/v1',
    );
  });

  test('a trailing slash does not produce a doubled separator', () {
    expect(
      apiBaseUrl('https://192.168.31.91:8080/'),
      'https://192.168.31.91:8080/api/v1',
    );
    expect(
      apiBaseUrl('https://192.168.31.91:8080///'),
      'https://192.168.31.91:8080/api/v1',
    );
  });

  test('an address that already has the prefix is left alone', () {
    // Otherwise a stored value would grow a prefix on every read.
    expect(
      apiBaseUrl('https://api.example.com/api/v1'),
      'https://api.example.com/api/v1',
    );
    expect(
      apiBaseUrl('https://api.example.com/api/v1/'),
      'https://api.example.com/api/v1',
    );
  });

  test('applying it twice changes nothing', () {
    const once = 'https://192.168.31.91:8080';
    expect(apiBaseUrl(apiBaseUrl(once)), apiBaseUrl(once));
  });

  test('an empty address stays empty', () {
    // No address means no request; inventing a path would turn "not
    // configured" into a request to nowhere in particular.
    expect(apiBaseUrl(''), '');
  });

  test('a hosted deployment behind a path keeps it', () {
    expect(
      apiBaseUrl('https://example.com/nexus'),
      'https://example.com/nexus/api/v1',
    );
  });
}

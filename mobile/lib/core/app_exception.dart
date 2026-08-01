/// Thrown by [ApiClient] for any non-2xx response or network failure.
/// Carries the HTTP status (0 for network/parse failures that never reached
/// the server) plus a best-effort machine code and human message extracted
/// from the API's `Error` schema (`{ error: { code, message } }`).
class ApiException implements Exception {
  final int statusCode;
  final String code;
  final String message;

  const ApiException({
    required this.statusCode,
    required this.code,
    required this.message,
  });

  /// [host] is the server the request was aimed at, so the message can name
  /// the mistake instead of describing the symptom.
  factory ApiException.network(Object error, {String host = ''}) {
    return ApiException(
      statusCode: 0,
      code: 'network_error',
      message: '${_reach(host)}\n\n($error)',
    );
  }

  /// Pointing somebody at Settings is no help while they are signed out —
  /// every route leads back to sign-in until they are. The address is on the
  /// screen they are already looking at, so the message says so.
  static String _reach(String host) {
    final h = host.toLowerCase();
    if (h == 'localhost' || h == '127.0.0.1' || h == '::1') {
      return 'Nothing is running at $host on this phone. On a phone, '
          '$host means the phone itself — not the machine running NexusVPN. '
          'Use the address the desktop app shows you, which looks like '
          'http://192.168.1.20:8080.';
    }
    return 'Could not reach $host. Check the Server address above, and that '
        'this phone is on the same network as the machine running NexusVPN.';
  }

  factory ApiException.timeout() {
    return const ApiException(
      statusCode: 0,
      code: 'timeout',
      message: 'The request timed out. The server may be unreachable.',
    );
  }

  bool get isUnauthorized => statusCode == 401;
  bool get isForbidden => statusCode == 403;
  bool get isNotFound => statusCode == 404;
  bool get isConflict => statusCode == 409;
  bool get isNetworkFailure => statusCode == 0;

  @override
  String toString() => message;
}

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

  factory ApiException.network(Object error) {
    return ApiException(
      statusCode: 0,
      code: 'network_error',
      message: 'Could not reach the server. Check your connection and '
          'server URL in Settings.\n($error)',
    );
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

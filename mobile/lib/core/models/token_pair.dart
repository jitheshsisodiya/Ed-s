/// Mirrors the `TokenPair` schema in api/openapi.yaml.
class TokenPair {
  final String? accessToken;
  final String? refreshToken;
  final int? expiresIn;
  final bool mfaRequired;

  const TokenPair({
    this.accessToken,
    this.refreshToken,
    this.expiresIn,
    this.mfaRequired = false,
  });

  factory TokenPair.fromJson(Map<String, dynamic> json) {
    return TokenPair(
      accessToken: json['accessToken'] as String?,
      refreshToken: json['refreshToken'] as String?,
      expiresIn: json['expiresIn'] as int?,
      mfaRequired: json['mfaRequired'] as bool? ?? false,
    );
  }

  Map<String, dynamic> toJson() => {
        if (accessToken != null) 'accessToken': accessToken,
        if (refreshToken != null) 'refreshToken': refreshToken,
        if (expiresIn != null) 'expiresIn': expiresIn,
        'mfaRequired': mfaRequired,
      };

  bool get hasTokens => accessToken != null && refreshToken != null;
}

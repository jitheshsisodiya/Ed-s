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

/// What a pairing code buys: a session, plus where the device should go next.
class ClaimedPairing {
  const ClaimedPairing({
    required this.tokens,
    required this.email,
    required this.networkId,
  });

  final TokenPair tokens;

  /// The account the code belonged to. Shown back to the person so they can
  /// see whose network they have just joined.
  final String email;

  /// The network the code was made for, empty if it named none.
  final String networkId;

  factory ClaimedPairing.fromJson(Map<String, dynamic> json) => ClaimedPairing(
        tokens: TokenPair.fromJson(json),
        email: (json['email'] as String?) ?? '',
        networkId: (json['networkId'] as String?) ?? '',
      );
}

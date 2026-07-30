/// Mirrors the `User` schema in api/openapi.yaml.
class User {
  final String id;
  final String email;
  final String displayName;
  final bool mfaEnabled;
  final String status; // active | disabled | pending_verification
  final DateTime? createdAt;

  const User({
    required this.id,
    required this.email,
    required this.displayName,
    required this.mfaEnabled,
    required this.status,
    this.createdAt,
  });

  factory User.fromJson(Map<String, dynamic> json) {
    return User(
      id: json['id'] as String? ?? '',
      email: json['email'] as String? ?? '',
      displayName: json['displayName'] as String? ?? '',
      mfaEnabled: json['mfaEnabled'] as bool? ?? false,
      status: json['status'] as String? ?? 'active',
      createdAt: json['createdAt'] != null
          ? DateTime.tryParse(json['createdAt'] as String)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'email': email,
        'displayName': displayName,
        'mfaEnabled': mfaEnabled,
        'status': status,
        if (createdAt != null) 'createdAt': createdAt!.toIso8601String(),
      };
}

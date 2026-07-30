/// Mirrors the `Member` schema in api/openapi.yaml.
class Member {
  final String userId;
  final String email;
  final String displayName;
  final String role; // owner | admin | member
  final DateTime? joinedAt;

  const Member({
    required this.userId,
    required this.email,
    required this.displayName,
    required this.role,
    this.joinedAt,
  });

  factory Member.fromJson(Map<String, dynamic> json) {
    return Member(
      userId: json['userId'] as String? ?? '',
      email: json['email'] as String? ?? '',
      displayName: json['displayName'] as String? ?? '',
      role: json['role'] as String? ?? 'member',
      joinedAt: json['joinedAt'] != null
          ? DateTime.tryParse(json['joinedAt'] as String)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'userId': userId,
        'email': email,
        'displayName': displayName,
        'role': role,
        if (joinedAt != null) 'joinedAt': joinedAt!.toIso8601String(),
      };
}

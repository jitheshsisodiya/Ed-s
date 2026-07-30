/// Mirrors the `AuditLog` schema in api/openapi.yaml.
class AuditLog {
  final String id;
  final String? actorUserId;
  final String action;
  final String? targetType;
  final String? targetId;
  final DateTime? createdAt;

  const AuditLog({
    required this.id,
    this.actorUserId,
    required this.action,
    this.targetType,
    this.targetId,
    this.createdAt,
  });

  factory AuditLog.fromJson(Map<String, dynamic> json) {
    return AuditLog(
      id: json['id'] as String? ?? '',
      actorUserId: json['actorUserId'] as String?,
      action: json['action'] as String? ?? '',
      targetType: json['targetType'] as String?,
      targetId: json['targetId'] as String?,
      createdAt: json['createdAt'] != null
          ? DateTime.tryParse(json['createdAt'] as String)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        if (actorUserId != null) 'actorUserId': actorUserId,
        'action': action,
        if (targetType != null) 'targetType': targetType,
        if (targetId != null) 'targetId': targetId,
        if (createdAt != null) 'createdAt': createdAt!.toIso8601String(),
      };
}

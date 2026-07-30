/// Mirrors the `ConnectionLog` schema in api/openapi.yaml.
class ConnectionLog {
  final String id;
  final String deviceId;
  final String? peerDeviceId;
  final String eventType;
  final int? latencyMs;
  final DateTime? createdAt;

  const ConnectionLog({
    required this.id,
    required this.deviceId,
    this.peerDeviceId,
    required this.eventType,
    this.latencyMs,
    this.createdAt,
  });

  factory ConnectionLog.fromJson(Map<String, dynamic> json) {
    return ConnectionLog(
      id: json['id'] as String? ?? '',
      deviceId: json['deviceId'] as String? ?? '',
      peerDeviceId: json['peerDeviceId'] as String?,
      eventType: json['eventType'] as String? ?? '',
      latencyMs: json['latencyMs'] as int?,
      createdAt: json['createdAt'] != null
          ? DateTime.tryParse(json['createdAt'] as String)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'deviceId': deviceId,
        if (peerDeviceId != null) 'peerDeviceId': peerDeviceId,
        'eventType': eventType,
        if (latencyMs != null) 'latencyMs': latencyMs,
        if (createdAt != null) 'createdAt': createdAt!.toIso8601String(),
      };
}

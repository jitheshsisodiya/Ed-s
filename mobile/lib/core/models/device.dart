/// Mirrors the `Device` schema in api/openapi.yaml. Used both for a user's
/// own registered devices and for peer entries returned by
/// `GET /devices/{id}/peers`.
class Device {
  final String id;
  final String name;
  final String os;
  final String? osVersion;
  final String publicKey;
  final String? virtualIp;
  final String? lastPublicIp;
  final String status; // online | offline | unknown
  final String? natType;
  final int? latencyMs;
  final int bytesSent;
  final int bytesReceived;
  final DateTime? lastSeenAt;

  const Device({
    required this.id,
    required this.name,
    required this.os,
    this.osVersion,
    required this.publicKey,
    this.virtualIp,
    this.lastPublicIp,
    this.status = 'unknown',
    this.natType,
    this.latencyMs,
    this.bytesSent = 0,
    this.bytesReceived = 0,
    this.lastSeenAt,
  });

  factory Device.fromJson(Map<String, dynamic> json) {
    return Device(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      os: json['os'] as String? ?? '',
      osVersion: json['osVersion'] as String?,
      publicKey: json['publicKey'] as String? ?? '',
      virtualIp: json['virtualIp'] as String?,
      lastPublicIp: json['lastPublicIp'] as String?,
      status: json['status'] as String? ?? 'unknown',
      natType: json['natType'] as String?,
      latencyMs: json['latencyMs'] as int?,
      bytesSent: json['bytesSent'] as int? ?? 0,
      bytesReceived: json['bytesReceived'] as int? ?? 0,
      lastSeenAt: json['lastSeenAt'] != null
          ? DateTime.tryParse(json['lastSeenAt'] as String)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'os': os,
        if (osVersion != null) 'osVersion': osVersion,
        'publicKey': publicKey,
        if (virtualIp != null) 'virtualIp': virtualIp,
        if (lastPublicIp != null) 'lastPublicIp': lastPublicIp,
        'status': status,
        if (natType != null) 'natType': natType,
        if (latencyMs != null) 'latencyMs': latencyMs,
        'bytesSent': bytesSent,
        'bytesReceived': bytesReceived,
        if (lastSeenAt != null) 'lastSeenAt': lastSeenAt!.toIso8601String(),
      };

  bool get isOnline => status == 'online';
}

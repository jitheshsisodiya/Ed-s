/// Mirrors the `DashboardStats` schema in api/openapi.yaml.
class DashboardStats {
  final int activeUsers;
  final int activeNetworks;
  final int onlineDevices;
  final int totalDevices;
  final int relayBandwidthBytes;
  final double p2pConnectionRatio;

  const DashboardStats({
    this.activeUsers = 0,
    this.activeNetworks = 0,
    this.onlineDevices = 0,
    this.totalDevices = 0,
    this.relayBandwidthBytes = 0,
    this.p2pConnectionRatio = 0,
  });

  factory DashboardStats.fromJson(Map<String, dynamic> json) {
    return DashboardStats(
      activeUsers: json['activeUsers'] as int? ?? 0,
      activeNetworks: json['activeNetworks'] as int? ?? 0,
      onlineDevices: json['onlineDevices'] as int? ?? 0,
      totalDevices: json['totalDevices'] as int? ?? 0,
      relayBandwidthBytes: json['relayBandwidthBytes'] as int? ?? 0,
      p2pConnectionRatio:
          (json['p2pConnectionRatio'] as num?)?.toDouble() ?? 0.0,
    );
  }

  Map<String, dynamic> toJson() => {
        'activeUsers': activeUsers,
        'activeNetworks': activeNetworks,
        'onlineDevices': onlineDevices,
        'totalDevices': totalDevices,
        'relayBandwidthBytes': relayBandwidthBytes,
        'p2pConnectionRatio': p2pConnectionRatio,
      };
}

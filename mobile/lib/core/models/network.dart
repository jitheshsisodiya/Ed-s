/// Mirrors the `Network` schema in api/openapi.yaml.
class Network {
  final String id;
  final String name;
  final String? description;
  final String cidr;
  final List<String> dnsServers;
  final String role; // owner | admin | member
  final int memberCount;
  final int deviceCount;
  final String? inviteCode;
  final DateTime? createdAt;

  const Network({
    required this.id,
    required this.name,
    this.description,
    required this.cidr,
    this.dnsServers = const [],
    required this.role,
    this.memberCount = 0,
    this.deviceCount = 0,
    this.inviteCode,
    this.createdAt,
  });

  factory Network.fromJson(Map<String, dynamic> json) {
    return Network(
      id: json['id'] as String? ?? '',
      name: json['name'] as String? ?? '',
      description: json['description'] as String?,
      cidr: json['cidr'] as String? ?? '',
      dnsServers: (json['dnsServers'] as List<dynamic>? ?? const [])
          .map((e) => e.toString())
          .toList(),
      role: json['role'] as String? ?? 'member',
      memberCount: json['memberCount'] as int? ?? 0,
      deviceCount: json['deviceCount'] as int? ?? 0,
      inviteCode: json['inviteCode'] as String?,
      createdAt: json['createdAt'] != null
          ? DateTime.tryParse(json['createdAt'] as String)
          : null,
    );
  }

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        if (description != null) 'description': description,
        'cidr': cidr,
        'dnsServers': dnsServers,
        'role': role,
        'memberCount': memberCount,
        'deviceCount': deviceCount,
        if (inviteCode != null) 'inviteCode': inviteCode,
        if (createdAt != null) 'createdAt': createdAt!.toIso8601String(),
      };

  Network copyWith({
    String? name,
    String? description,
    String? cidr,
    List<String>? dnsServers,
    String? inviteCode,
  }) {
    return Network(
      id: id,
      name: name ?? this.name,
      description: description ?? this.description,
      cidr: cidr ?? this.cidr,
      dnsServers: dnsServers ?? this.dnsServers,
      role: role,
      memberCount: memberCount,
      deviceCount: deviceCount,
      inviteCode: inviteCode ?? this.inviteCode,
      createdAt: createdAt,
    );
  }

  bool get canManage => role == 'owner' || role == 'admin';
}

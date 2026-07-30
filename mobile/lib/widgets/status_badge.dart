import 'package:flutter/material.dart';

/// Small colored pill used for device/network/VPN status everywhere in the
/// app ("online", "offline", "connecting", ...).
class StatusBadge extends StatelessWidget {
  const StatusBadge({super.key, required this.label, required this.color});

  factory StatusBadge.forDeviceStatus(String status) {
    switch (status) {
      case 'online':
        return StatusBadge(label: 'Online', color: Colors.green.shade600);
      case 'offline':
        return StatusBadge(label: 'Offline', color: Colors.grey.shade600);
      default:
        return StatusBadge(label: 'Unknown', color: Colors.orange.shade700);
    }
  }

  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.15),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: color.withValues(alpha: 0.4)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(color: color, shape: BoxShape.circle),
          ),
          const SizedBox(width: 6),
          Text(
            label,
            style: TextStyle(
              color: color,
              fontWeight: FontWeight.w600,
              fontSize: 12,
            ),
          ),
        ],
      ),
    );
  }
}

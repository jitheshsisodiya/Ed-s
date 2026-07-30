import 'package:flutter/material.dart';

import '../core/models/models.dart';
import 'status_badge.dart';

/// Row for a single device in a device list: name, OS, status badge and
/// optional latency/trailing action.
class DeviceTile extends StatelessWidget {
  const DeviceTile({
    super.key,
    required this.device,
    this.trailing,
    this.onTap,
    this.subtitleOverride,
  });

  final Device device;
  final Widget? trailing;
  final VoidCallback? onTap;
  final String? subtitleOverride;

  IconData get _osIcon {
    final os = device.os.toLowerCase();
    if (os.contains('android')) return Icons.android;
    if (os.contains('ios') || os.contains('mac')) return Icons.apple;
    if (os.contains('win')) return Icons.desktop_windows_outlined;
    if (os.contains('linux')) return Icons.terminal;
    return Icons.devices_other_outlined;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final subtitleParts = <String>[
      if (device.virtualIp != null) device.virtualIp!,
      if (device.latencyMs != null) '${device.latencyMs}ms',
    ];
    return ListTile(
      onTap: onTap,
      leading: CircleAvatar(
        backgroundColor: theme.colorScheme.surfaceContainerHighest,
        child: Icon(_osIcon, color: theme.colorScheme.onSurfaceVariant),
      ),
      title: Text(device.name),
      subtitle: Text(
        subtitleOverride ??
            (subtitleParts.isEmpty
                ? '${device.os}${device.osVersion != null ? " ${device.osVersion}" : ""}'
                : subtitleParts.join(' · ')),
      ),
      trailing: trailing ?? StatusBadge.forDeviceStatus(device.status),
    );
  }
}

import 'package:flutter/material.dart';

import '../core/connection_quality.dart';
import '../core/models/models.dart';
import 'quality_chip.dart';

/// Row for a single device: who it is, how well it is reachable, and — only
/// in Advanced mode — where it is.
///
/// A device is addressed by its name here. The virtual IP is the thing
/// people paste into a game browser or a file share, so it stays available
/// but off screen by default.
class DeviceTile extends StatelessWidget {
  const DeviceTile({
    super.key,
    required this.device,
    this.advanced = false,
    this.trailing,
    this.onTap,
    this.subtitleOverride,
  });

  final Device device;
  final bool advanced;
  final Widget? trailing;
  final VoidCallback? onTap;
  final String? subtitleOverride;

  IconData get _osIcon {
    final os = device.os.toLowerCase();
    if (os.contains('android')) return Icons.android;
    if (os.contains('ios') || os.contains('mac')) return Icons.apple;
    if (os.contains('server')) return Icons.dns_outlined;
    if (os.contains('win')) return Icons.desktop_windows_outlined;
    if (os.contains('linux')) return Icons.terminal;
    return Icons.devices_other_outlined;
  }

  ConnectionQuality get _quality => describeDeviceQuality(
        online: device.isOnline,
        latencyMs: device.latencyMs,
      );

  String _subtitle() {
    if (subtitleOverride != null) return subtitleOverride!;
    if (!_quality.isOnline) {
      final seen = device.lastSeenAt;
      return seen == null ? 'Not connected yet' : 'Last seen ${_since(seen)}';
    }
    if (advanced) {
      return [
        if (device.virtualIp != null) device.virtualIp!,
        if (device.latencyMs != null) '${device.latencyMs} ms',
        if (device.natType != null) device.natType!,
      ].join(' · ');
    }
    return describePath('direct');
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListTile(
      onTap: onTap,
      leading: CircleAvatar(
        backgroundColor: theme.colorScheme.surfaceContainerHighest,
        child: Icon(_osIcon, color: theme.colorScheme.onSurfaceVariant),
      ),
      title: Text(device.name),
      subtitle: Text(_subtitle()),
      trailing: trailing ?? QualityChip(quality: _quality),
    );
  }
}

/// "just now", "4 min ago", "2 days ago".
String _since(DateTime when) {
  final secs = DateTime.now().difference(when).inSeconds;
  if (secs < 45) return 'just now';
  if (secs < 3600) return '${(secs / 60).round()} min ago';
  if (secs < 86400) return '${(secs / 3600).round()} hr ago';
  return '${(secs / 86400).round()} days ago';
}

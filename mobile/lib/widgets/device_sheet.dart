import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core/connection_quality.dart';
import '../core/deck_theme.dart';
import '../core/models/models.dart';
import 'signal_bars.dart';

/// The phone's answer to the desktop's right-click menu: an action sheet
/// carrying what you can do with one machine, and the facts about it.
///
/// Actions and properties are in the same sheet rather than two screens
/// because on a phone the reason you opened this is almost always to copy
/// the address, and a menu that leads to a menu puts that two taps away.
Future<void> showDeviceSheet(
  BuildContext context, {
  required Device device,
  required String networkName,
  required bool isSelf,
}) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    builder: (sheet) => _DeviceSheet(
      device: device,
      networkName: networkName,
      isSelf: isSelf,
    ),
  );
}

class _DeviceSheet extends StatelessWidget {
  const _DeviceSheet({
    required this.device,
    required this.networkName,
    required this.isSelf,
  });

  final Device device;
  final String networkName;
  final bool isSelf;

  @override
  Widget build(BuildContext context) {
    final quality = describeDeviceQuality(
      online: device.isOnline,
      latencyMs: device.latencyMs,
    );
    final address = device.virtualIp ?? '';

    return SafeArea(
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // Header: who this is, and how well you can reach them.
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 12),
              child: Row(
                children: [
                  SignalBars(
                    latencyMs: device.latencyMs,
                    online: device.isOnline,
                    height: 18,
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          device.name,
                          style: const TextStyle(fontSize: 16, color: Deck.ink),
                          overflow: TextOverflow.ellipsis,
                        ),
                        const SizedBox(height: 2),
                        Text(
                          quality.label.toUpperCase(),
                          style: Deck.mono(
                            size: 10,
                            color: _qualityColor(quality),
                            spacing: 1.4,
                          ),
                        ),
                      ],
                    ),
                  ),
                  Text(
                    address.isEmpty ? '—' : address,
                    style: Deck.mono(size: 14, color: Deck.inkDim),
                  ),
                ],
              ),
            ),
            const Divider(height: 1, color: Deck.line),

            _SheetAction(
              icon: Icons.copy,
              label: 'Copy IP address',
              enabled: address.isNotEmpty,
              onTap: () async {
                await Clipboard.setData(ClipboardData(text: address));
                if (!context.mounted) return;
                Navigator.pop(context);
                ScaffoldMessenger.of(context).showSnackBar(
                  SnackBar(
                    content: Text('${device.name} address copied'),
                    backgroundColor: Deck.deck700,
                    behavior: SnackBarBehavior.floating,
                  ),
                );
              },
            ),

            const Divider(height: 1, color: Deck.line),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 14, 16, 8),
              child: Text('PROPERTIES', style: Deck.eyebrow()),
            ),
            _Fact('Platform', device.os.isEmpty ? 'unknown' : device.os),
            if (device.osVersion != null) _Fact('Version', device.osVersion!),
            _Fact('Network', networkName.isEmpty ? '—' : networkName),
            _Fact('Address', address.isEmpty ? '—' : address, mono: true),
            _Fact(
              'Round trip',
              device.latencyMs != null ? '${device.latencyMs} ms' : 'not measured',
              mono: true,
            ),
            if (device.natType != null) _Fact('NAT type', device.natType!),
            _Fact(
              'Last seen',
              device.lastSeenAt != null
                  ? device.lastSeenAt!.toLocal().toString().split('.').first
                  : 'never',
            ),
            _Fact(
              'Transferred',
              '${_bytes(device.bytesSent)} up · ${_bytes(device.bytesReceived)} down',
              mono: true,
            ),
            if (isSelf)
              const Padding(
                padding: EdgeInsets.fromLTRB(16, 10, 16, 0),
                child: Text(
                  'This is the device you are holding.',
                  style: TextStyle(fontSize: 12, color: Deck.inkFaint),
                ),
              ),
            const SizedBox(height: 20),
          ],
        ),
      ),
    );
  }

  static Color _qualityColor(ConnectionQuality q) => switch (q) {
        ConnectionQuality.excellent => Deck.live,
        ConnectionQuality.good => Deck.live,
        ConnectionQuality.limited => Deck.work,
        ConnectionQuality.offline => Deck.inkFaint,
      };

  static String _bytes(int n) {
    if (n <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    var value = n.toDouble();
    var i = 0;
    while (value >= 1024 && i < units.length - 1) {
      value /= 1024;
      i++;
    }
    return '${value.toStringAsFixed(i == 0 ? 0 : 1)} ${units[i]}';
  }
}

class _SheetAction extends StatelessWidget {
  const _SheetAction({
    required this.icon,
    required this.label,
    required this.onTap,
    this.enabled = true,
  });

  final IconData icon;
  final String label;
  final VoidCallback onTap;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: Icon(icon, size: 18, color: enabled ? Deck.inkDim : Deck.inkFaint),
      title: Text(
        label,
        style: TextStyle(fontSize: 14, color: enabled ? Deck.ink : Deck.inkFaint),
      ),
      onTap: enabled ? onTap : null,
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact(this.label, this.value, {this.mono = false});

  final String label;
  final String value;
  final bool mono;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 104,
            child: Text(
              label,
              style: const TextStyle(fontSize: 12, color: Deck.inkFaint),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: mono
                  ? Deck.mono(size: 12.5, color: Deck.inkDim)
                  : const TextStyle(fontSize: 13, color: Deck.inkDim),
            ),
          ),
        ],
      ),
    );
  }
}

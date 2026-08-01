import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core/deck_theme.dart';
import '../core/models/models.dart';
import 'signal_bars.dart';

/// Every network you belong to, with its machines nested underneath.
///
/// The same shape as the desktop, because it is the right one: you open this
/// to find one machine among a handful and do something with it, and a tree
/// puts every candidate on screen with no navigation.
///
/// Where the desktop uses right-click, this uses a long press onto an action
/// sheet. Tapping a row opens the same sheet, because on a phone a row with
/// no tap target is a row people tap anyway and then think is broken.
class NetworkTree extends StatefulWidget {
  const NetworkTree({
    super.key,
    required this.networks,
    required this.devices,
    required this.activeNetworkId,
    required this.selfPublicKey,
    required this.busy,
    required this.connected,
    required this.onConnect,
    required this.onDisconnect,
    required this.onShare,
    required this.onAction,
  });

  final List<Network> networks;

  /// The machines on the active network. Empty for every other network,
  /// which is honest: we have not talked to those.
  final List<Device> devices;

  final String activeNetworkId;
  final String selfPublicKey;
  final bool busy;
  final bool connected;

  final void Function(Network) onConnect;
  final VoidCallback onDisconnect;
  final void Function(Network) onShare;
  final void Function(Device) onAction;

  @override
  State<NetworkTree> createState() => _NetworkTreeState();
}

class _NetworkTreeState extends State<NetworkTree> {
  final _collapsed = <String>{};

  @override
  Widget build(BuildContext context) {
    if (widget.networks.isEmpty) return const _NoNetworks();

    return ListView(
      padding: EdgeInsets.zero,
      children: [
        for (final n in widget.networks) ..._section(n),
        const SizedBox(height: 24),
      ],
    );
  }

  List<Widget> _section(Network n) {
    final isActive = n.id == widget.activeNetworkId;
    final open = !_collapsed.contains(n.id);
    final members = isActive ? widget.devices : const <Device>[];
    final online = members.where((d) => d.isOnline).length;

    return [
      Container(
        decoration: BoxDecoration(
          color: isActive ? Deck.live.withValues(alpha: 0.07) : Deck.deck800,
          border: const Border(
            top: BorderSide(color: Deck.line),
            bottom: BorderSide(color: Deck.line),
          ),
        ),
        child: InkWell(
          onTap: () => setState(() {
            if (open) {
              _collapsed.add(n.id);
            } else {
              _collapsed.remove(n.id);
            }
          }),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(8, 10, 4, 10),
            child: Row(
              children: [
                Icon(
                  open ? Icons.keyboard_arrow_down : Icons.keyboard_arrow_right,
                  size: 18,
                  color: Deck.inkFaint,
                ),
                const SizedBox(width: 4),
                Expanded(
                  child: Text(
                    n.name,
                    style: TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.w500,
                      color: isActive ? Deck.live : Deck.ink,
                    ),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                Text(
                  isActive ? '$online/${members.length}' : '${n.deviceCount}',
                  style: Deck.mono(size: 11, color: Deck.inkFaint),
                ),
                if (n.inviteCode != null && n.inviteCode!.isNotEmpty)
                  _Action(
                    icon: Icons.qr_code_2,
                    label: 'Invite people to ${n.name}',
                    onTap: () => widget.onShare(n),
                  ),
                if (isActive)
                  _Action(
                    icon: Icons.link_off,
                    label: 'Disconnect',
                    color: Deck.fail,
                    onTap: widget.busy ? null : widget.onDisconnect,
                  )
                else
                  _Action(
                    icon: Icons.link,
                    label: 'Connect to ${n.name}',
                    color: Deck.live,
                    onTap: widget.busy || widget.connected
                        ? null
                        : () => widget.onConnect(n),
                  ),
              ],
            ),
          ),
        ),
      ),
      if (open) ...[
        if (isActive && members.isEmpty)
          const _Note('No other machines here yet.')
        else if (!isActive)
          const _Note('Connect to see the machines on this network.'),
        for (final d in members)
          _DeviceRow(
            device: d,
            isSelf: d.publicKey == widget.selfPublicKey,
            onAction: () => widget.onAction(d),
          ),
      ],
    ];
  }
}

class _DeviceRow extends StatelessWidget {
  const _DeviceRow({
    required this.device,
    required this.isSelf,
    required this.onAction,
  });

  final Device device;
  final bool isSelf;
  final VoidCallback onAction;

  @override
  Widget build(BuildContext context) {
    final online = device.isOnline;

    return InkWell(
      onTap: onAction,
      onLongPress: () {
        HapticFeedback.selectionClick();
        onAction();
      },
      child: Padding(
        padding: const EdgeInsets.fromLTRB(30, 9, 12, 9),
        child: Row(
          children: [
            SignalBars(
              latencyMs: device.latencyMs,
              online: online,
              relayed: false,
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Text.rich(
                TextSpan(
                  text: device.name,
                  children: isSelf
                      ? [
                          TextSpan(
                            text: '  (this device)',
                            style: TextStyle(color: Deck.inkFaint, fontSize: 12),
                          ),
                        ]
                      : null,
                ),
                style: TextStyle(
                  fontSize: 14,
                  color: online ? Deck.ink : Deck.inkFaint,
                ),
                overflow: TextOverflow.ellipsis,
              ),
            ),
            if (online && device.latencyMs != null)
              Padding(
                padding: const EdgeInsets.only(right: 10),
                child: Text(
                  '${device.latencyMs}ms',
                  style: Deck.mono(size: 11, color: _rttColor(device.latencyMs!)),
                ),
              ),
            Text(
              device.virtualIp ?? '—',
              style: Deck.mono(
                size: 12.5,
                color: online ? Deck.inkDim : Deck.inkFaint,
              ),
            ),
          ],
        ),
      ),
    );
  }

  /// The same thresholds `client/agent` uses to pick a word, so a colour
  /// here and "Excellent" anywhere else can never disagree.
  static Color _rttColor(int ms) {
    if (ms < 50) return Deck.live;
    if (ms < 150) return Deck.inkDim;
    return Deck.fail;
  }
}

class _Action extends StatelessWidget {
  const _Action({
    required this.icon,
    required this.label,
    required this.onTap,
    this.color,
  });

  final IconData icon;
  final String label;
  final VoidCallback? onTap;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    return IconButton(
      icon: Icon(icon, size: 17),
      color: color ?? Deck.inkFaint,
      disabledColor: Deck.inkFaint.withValues(alpha: 0.35),
      tooltip: label,
      visualDensity: VisualDensity.compact,
      constraints: const BoxConstraints.tightFor(width: 36, height: 36),
      padding: EdgeInsets.zero,
      onPressed: onTap,
    );
  }
}

class _Note extends StatelessWidget {
  const _Note(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(30, 10, 16, 10),
      child: Text(text, style: const TextStyle(fontSize: 12.5, color: Deck.inkFaint)),
    );
  }
}

class _NoNetworks extends StatelessWidget {
  const _NoNetworks();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.hub_outlined, size: 22, color: Deck.inkFaint),
            const SizedBox(height: 14),
            Text('NO NETWORKS', style: Deck.eyebrow(color: Deck.inkDim)),
            const SizedBox(height: 10),
            const Text(
              'Create one for your own machines, or join a friend’s with '
              'their invite code. Both are in the menu.',
              textAlign: TextAlign.center,
              style: TextStyle(fontSize: 13, height: 1.5, color: Deck.inkFaint),
            ),
          ],
        ),
      ),
    );
  }
}

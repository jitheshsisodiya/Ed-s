import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nexusvpn/core/deck_theme.dart';
import 'package:nexusvpn/core/models/models.dart';
import 'package:nexusvpn/widgets/network_tree.dart';
import 'package:nexusvpn/widgets/reactor_core.dart';
import 'package:nexusvpn/widgets/scrambler.dart';

/// Layout tests for the deck.
///
/// These exist because of a bug that no amount of reading found: the
/// identity strip is a Column inside a Column, and a Column's non-flex
/// children are laid out with *unbounded* height, so an inner Column
/// defaulting to MainAxisSize.max asked to be a hundred thousand pixels
/// tall. It analyzed clean and passed every behavioural test; it simply did
/// not fit on a screen.
///
/// So the assertion here is the plain one every phone layout needs and
/// almost nobody writes: at the smallest size we support, in every state,
/// nothing overflows.
void main() {
  // A 320x568 logical screen — an iPhone SE, the smallest thing anyone
  // still runs. If it fits here it fits everywhere.
  const smallest = Size(320, 568);

  for (final phase in DeckPhase.values) {
    testWidgets('the deck fits a small screen in ${phase.name}', (tester) async {
      await tester.binding.setSurfaceSize(smallest);
      addTearDown(() => tester.binding.setSurfaceSize(null));

      await tester.pumpWidget(_Deck(phase: phase));
      await tester.pump(const Duration(milliseconds: 400));

      expect(
        tester.takeException(),
        isNull,
        reason: 'the deck overflowed at ${smallest.width}x${smallest.height} '
            'in the ${phase.name} state',
      );
    });
  }

  testWidgets('the reactor core reports its state to assistive tech',
      (tester) async {
    final semantics = tester.ensureSemantics();

    await tester.pumpWidget(
      MaterialApp(
        theme: Deck.theme(),
        home: Scaffold(
          body: Center(
            child: ReactorCore(
              phase: DeckPhase.active,
              label: 'on',
              onPressed: () {},
            ),
          ),
        ),
      ),
    );

    // The colour and the motion are the whole point of this control, and
    // neither reaches somebody using a screen reader. The label has to
    // carry the state in words.
    final node = tester.getSemantics(find.byType(ReactorCore));
    expect(node.label, contains('online'));
    expect(node.flagsCollection.isButton, isTrue);

    semantics.dispose();
  });

  testWidgets('an address with no value shows a placeholder, not nothing',
      (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: Deck.theme(),
        home: const Scaffold(
          body: Center(child: Scrambler(value: '', active: false)),
        ),
      ),
    );

    // An empty slot where an address goes reads as a rendering fault. A
    // dash reads as "not yet".
    expect(find.text('—'), findsOneWidget);
  });

  testWidgets('a settled address is shown exactly, never mid-scramble',
      (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: Deck.theme(),
        home: const Scaffold(
          body: Center(child: Scrambler(value: '100.84.7.109', active: false)),
        ),
      ),
    );

    expect(find.text('100.84.7.109'), findsOneWidget);
  });
}

/// The deck's real composition: top bar, identity strip, tree.
class _Deck extends StatelessWidget {
  const _Deck({required this.phase});

  final DeckPhase phase;

  @override
  Widget build(BuildContext context) {
    final connected = phase == DeckPhase.active;
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      theme: Deck.theme(),
      home: Scaffold(
        body: SafeArea(
          child: Column(
            children: [
              Container(
                height: 40,
                color: Deck.deck800,
                padding: const EdgeInsets.symmetric(horizontal: 12),
                alignment: Alignment.centerLeft,
                child: Text(
                  phase.word.toUpperCase(),
                  style: Deck.mono(size: 10, color: phase.color),
                ),
              ),
              Container(
                width: double.infinity,
                color: Deck.deck800,
                padding: const EdgeInsets.fromLTRB(16, 18, 16, 20),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    ReactorCore(phase: phase, label: 'on', onPressed: () {}),
                    const SizedBox(height: 16),
                    const Text('Jithesh S', style: TextStyle(fontSize: 15)),
                    Scrambler(
                      value: connected ? '100.84.7.109' : '',
                      active: false,
                      style: Deck.mono(size: 24, color: phase.color),
                    ),
                  ],
                ),
              ),
              Expanded(
                child: NetworkTree(
                  networks: _networks,
                  devices: connected ? _devices : const [],
                  activeNetworkId: connected ? 'n1' : '',
                  selfPublicKey: 'self',
                  busy: false,
                  connected: connected,
                  onConnect: (_) {},
                  onDisconnect: () {},
                  onShare: (_) {},
                  onAction: (_) {},
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

const _networks = [
  Network(
    id: 'n1',
    name: 'J D S J & Co _ Tally',
    cidr: '100.84.0.0/16',
    role: 'owner',
    memberCount: 9,
    deviceCount: 6,
    inviteCode: 'K7M2QP',
  ),
  Network(
    id: 'n2',
    name: 'Weekend Raids',
    cidr: '100.85.0.0/16',
    role: 'member',
    memberCount: 6,
    deviceCount: 9,
  ),
];

final _devices = [
  _dev('Aditya Yadav', '100.84.208.35', 'online', 11),
  _dev('Bhavik C Jain', '100.84.58.162', 'offline', null),
  _dev('TT Server', '100.84.86.12', 'online', 88),
  _dev('PAVAN', '100.84.207.22', 'online', 186),
];

Device _dev(String name, String ip, String status, int? latency) => Device(
      id: name,
      name: name,
      os: 'windows',
      publicKey: name,
      virtualIp: ip,
      status: status,
      latencyMs: latency,
    );

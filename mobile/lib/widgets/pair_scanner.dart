import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

/// The camera, pointed at a computer screen.
///
/// Separate from the invite scanner because it means something different: an
/// invite code joins a network you are already signed in for, and this one
/// signs you in. Sharing a screen between them would mean guessing which the
/// person meant from whatever happened to be in frame.
class PairScannerScreen extends StatefulWidget {
  const PairScannerScreen({super.key});

  @override
  State<PairScannerScreen> createState() => _PairScannerScreenState();
}

class _PairScannerScreenState extends State<PairScannerScreen> {
  /// A camera reads the same code many times a second. Without this the
  /// screen is popped once per frame, and every one of those pops but the
  /// first lands on whatever screen came next.
  bool _handled = false;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Scan to connect')),
      body: Stack(
        children: [
          MobileScanner(
            onDetect: (capture) {
              if (_handled) return;
              for (final barcode in capture.barcodes) {
                final value = barcode.rawValue;
                if (value != null && value.isNotEmpty) {
                  _handled = true;
                  Navigator.of(context).pop(value);
                  return;
                }
              }
            },
          ),
          Align(
            alignment: Alignment.bottomCenter,
            child: Container(
              width: double.infinity,
              padding: const EdgeInsets.fromLTRB(24, 20, 24, 40),
              color: Colors.black.withValues(alpha: 0.6),
              child: Text(
                'On your computer, open NexusVPN, find the network, and tap '
                'the phone icon on its row.',
                textAlign: TextAlign.center,
                style: Theme.of(context)
                    .textTheme
                    .bodyMedium
                    ?.copyWith(color: Colors.white),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

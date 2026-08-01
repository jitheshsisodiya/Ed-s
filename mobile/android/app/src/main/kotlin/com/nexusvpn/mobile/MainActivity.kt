package com.nexusvpn.mobile

import io.flutter.embedding.android.FlutterActivity

// Everything this app does lives in Dart. The one Android-specific concern —
// standing up the VpnService tunnel — belongs to the wireguard_flutter
// plugin, which registers itself through the generated plugin registrant, so
// there is nothing to override here.
class MainActivity : FlutterActivity()

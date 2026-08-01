# R8 keeps anything named in the merged manifest, which covers the WireGuard
# VpnService itself. These rules cover what the manifest does not name.

# The tunnel backend reaches its native library and its own service through
# JNI and reflection, neither of which R8 can see.
-keep class com.wireguard.** { *; }
-keep class billion.group.wireguard_flutter.** { *; }

# ML Kit loads barcode models by class name at runtime. Its own consumer
# rules cover the common path; this covers the optional modules it probes for
# and would otherwise log as missing on every scan.
-dontwarn com.google.mlkit.**

# The plugins' own consumer rules travel with their AARs and are applied
# automatically, so nothing else belongs here. Anything added should say
# which reflection it protects — a keep rule with no reason is a rule nobody
# can ever safely delete.

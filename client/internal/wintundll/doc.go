// Package wintundll ships the Wintun driver inside the binary and puts it
// where Windows can find it.
//
// Wintun is the kernel driver that gives a Windows process a TUN interface;
// without it there is no tunnel at all, only the error "Error loading
// wintun.dll DLL: Unable to load library". Every other WireGuard client on
// Windows solves this with an installer. Making somebody download a zip from
// a second website and drop a DLL beside an executable, correctly, before the
// app will do anything is not a setup step — it is a wall.
//
// So the DLL travels inside this program and is written out on first use.
//
// # What is embedded
//
// bin/wintun-amd64.dll and bin/wintun-arm64.dll are the unmodified contents
// of wintun-0.14.1.zip from wintun.net/builds, whose published SHA2-256 is
// 07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51. They are
// signed by WireGuard LLC, and that signature is why they must never be
// rebuilt, patched or recompressed here: a modified copy is an unsigned copy,
// and an unsigned copy cannot load the driver.
//
// bin/LICENSE.txt is WireGuard LLC's Prebuilt Binaries License, kept beside
// them as clause 3(c) requires. Clause 3(d) permits redistribution when the
// software is "distributed alongside other software that uses the Software
// only via the Permitted API" — which is what this is: NexusVPN reaches
// Wintun only through the documented wintun.h surface, by way of
// golang.zx2c4.com/wintun.
package wintundll

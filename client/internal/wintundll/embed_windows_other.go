//go:build windows && !amd64 && !arm64

package wintundll

// No driver is bundled for this architecture. Wintun also ships x86 and arm
// builds, but NexusVPN is not released for either, and carrying a driver no
// release can use would be dead weight in every binary that does.
var driver []byte

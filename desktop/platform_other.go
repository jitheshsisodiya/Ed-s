//go:build !windows

package main

// isWindows selects the tray icon container: Windows wants an ICO wrapper,
// every other platform takes the PNG directly.
const isWindows = false

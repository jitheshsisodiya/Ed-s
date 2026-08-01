//go:build !linux

package egress

import "fmt"

// Supported reports whether this build can act as an exit node.
//
// It cannot, here. Windows would need Internet Connection Sharing or a WFP
// callout driver; macOS needs a pf NAT anchor plus a forwarding sysctl Apple
// has relocated more than once. Both are shippable, neither is shippable
// untested, and an exit node that silently drops every packet is worse for
// its peers than one that was never offered — they would route their whole
// internet through it and lose all of it.
//
// So this build says no, and the client refuses to advertise rather than
// making a promise it cannot keep. Using someone else's exit node works on
// every platform; only offering to be one is restricted.
func Supported() bool { return false }

// New returns a forwarder that refuses, so a caller that ignores Supported
// still cannot half-enable anything.
func New() Forwarder { return unsupported{} }

type unsupported struct{}

func (unsupported) Enable(Config) error {
	return fmt.Errorf("egress: acting as an exit node is not supported on this platform yet")
}

func (unsupported) Disable() error { return nil }

func (unsupported) Enabled() (bool, error) { return false, nil }

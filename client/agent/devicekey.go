package agent

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
)

// errStaleDeviceKey means the control plane already knows this machine's
// WireGuard key, under a different account or a different network, and will
// not let it be reused.
var errStaleDeviceKey = errors.New("agent: device key belongs to another account or network")

// deviceKey returns this machine's identity on one network, minting one the
// first time it is needed.
//
// The first network to ask adopts the installation-wide key that older
// versions used, so an existing device keeps its identity — and with it its
// address — instead of appearing as a new machine. Every network after that
// gets its own, because the server allows a public key to belong to exactly
// one device.
func (a *Agent) deviceKey(networkID string) (config.Keypair, error) {
	var out config.Keypair

	_, err := a.store.Update(func(c *config.Config) error {
		if c.Networks == nil {
			c.Networks = map[string]*config.NetworkState{}
		}
		st := c.Networks[networkID]
		if st == nil {
			st = &config.NetworkState{NetworkID: networkID}
			c.Networks[networkID] = st
		}
		if !st.Keypair.IsZero() {
			out = st.Keypair
			return nil
		}

		if !c.Keypair.IsZero() && !keyTaken(c, c.Keypair.PublicKey) {
			st.Keypair = c.Keypair
			out = st.Keypair
			return nil
		}

		kp, err := config.GenerateKeypair()
		if err != nil {
			return fmt.Errorf("generate device key: %w", err)
		}
		st.Keypair = kp
		out = kp
		return nil
	})
	if err != nil {
		return config.Keypair{}, err
	}
	return out, nil
}

// resetDeviceKey replaces this machine's identity on one network.
//
// Called only after the server has refused the current key. The old device
// record stays where it is — it belongs to whoever registered it, and this
// installation has no standing to remove it.
func (a *Agent) resetDeviceKey(networkID string) error {
	kp, err := config.GenerateKeypair()
	if err != nil {
		return fmt.Errorf("generate device key: %w", err)
	}
	_, err = a.store.Update(func(c *config.Config) error {
		if c.Networks == nil {
			c.Networks = map[string]*config.NetworkState{}
		}
		st := c.Networks[networkID]
		if st == nil {
			st = &config.NetworkState{NetworkID: networkID}
			c.Networks[networkID] = st
		}
		st.Keypair = kp
		// The address came with the old identity and does not survive it.
		st.VirtualIP = ""
		st.DeviceID = ""
		return nil
	})
	return err
}

// keyTaken reports whether a public key is already serving some network.
func keyTaken(c *config.Config, publicKey string) bool {
	for _, st := range c.Networks {
		if st != nil && st.Keypair.PublicKey == publicKey {
			return true
		}
	}
	return false
}

// isStaleKeyRejection reports whether the server refused a registration in
// the one way a fresh key would fix.
//
// PermissionDenied covers two cases: the account is not a member of this
// network, and the key is spoken for. They are indistinguishable from here,
// so both are retried once — a retry cannot grant membership, because the
// membership check does not look at the key, so the only case it changes is
// the one it is meant to.
func isStaleKeyRejection(err error) bool {
	return status.Code(err) == codes.PermissionDenied
}

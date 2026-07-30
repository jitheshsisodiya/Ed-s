package auth

import (
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// MFAManager generates and validates RFC 6238 TOTP codes for account MFA.
type MFAManager struct {
	issuer string
}

// NewMFAManager builds an MFAManager. issuer is shown in authenticator apps.
func NewMFAManager(issuer string) *MFAManager {
	return &MFAManager{issuer: issuer}
}

// GenerateSecret creates a new TOTP secret + otpauth:// provisioning URL for
// the given account email.
func (m *MFAManager) GenerateSecret(accountEmail string) (secret string, otpauthURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      m.issuer,
		AccountName: accountEmail,
		SecretSize:  20,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// Validate checks a TOTP code against a secret, allowing the standard +/-1
// time-step skew.
func (m *MFAManager) Validate(secret, code string) bool {
	valid, _ := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return valid
}

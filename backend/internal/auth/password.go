package auth

import "golang.org/x/crypto/bcrypt"

// PasswordHasher wraps bcrypt with an application-chosen cost factor.
type PasswordHasher struct {
	cost int
}

// NewPasswordHasher builds a PasswordHasher. cost <= 0 uses bcrypt's default.
func NewPasswordHasher(cost int) *PasswordHasher {
	if cost <= 0 {
		cost = bcrypt.DefaultCost
	}
	return &PasswordHasher{cost: cost}
}

// Hash returns the bcrypt hash of a plaintext password.
func (h *PasswordHasher) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Verify reports whether password matches the given bcrypt hash.
func (h *PasswordHasher) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

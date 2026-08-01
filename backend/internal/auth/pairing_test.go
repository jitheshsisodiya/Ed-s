package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A pairing token is a bearer credential for somebody's whole account, so
// each of these is a property it has to keep rather than a behaviour it
// happens to have.
func TestPairingTokenRoundTrip(t *testing.T) {
	m := NewPairingManager("secret", time.Minute)
	user := uuid.New()

	token, expires, err := m.Issue(user, "network-1")
	if err != nil {
		t.Fatal(err)
	}
	if !expires.After(time.Now()) {
		t.Fatal("issued a token that is already expired")
	}

	claims, err := m.Consume(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != user.String() {
		t.Fatalf("user = %q, want %q", claims.UserID, user.String())
	}
	if claims.NetworkID != "network-1" {
		t.Fatalf("network = %q, want network-1", claims.NetworkID)
	}
}

// Single use is what makes a QR code on a screen acceptable. Without it,
// anyone who photographed the screen has the account until the token expires.
func TestPairingTokenCannotBeUsedTwice(t *testing.T) {
	m := NewPairingManager("secret", time.Minute)
	token, _, err := m.Issue(uuid.New(), "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Consume(token); err != nil {
		t.Fatal(err)
	}
	_, err = m.Consume(token)
	if !errors.Is(err, ErrPairingTokenUsed) {
		t.Fatalf("second use returned %v, want ErrPairingTokenUsed", err)
	}
}

func TestPairingTokenExpires(t *testing.T) {
	m := NewPairingManager("secret", time.Second)
	token, _, err := m.Issue(uuid.New(), "")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)

	if _, err := m.Consume(token); err == nil {
		t.Fatal("an expired pairing token was accepted")
	}
}

// A lifetime too short to write down would produce tokens that are expired
// on arrival — JWT expiry is whole seconds — so the floor is enforced rather
// than left to whoever picks the number.
func TestPairingTTLHasAFloor(t *testing.T) {
	m := NewPairingManager("secret", time.Millisecond)
	if m.TTL() < time.Second {
		t.Fatalf("ttl = %v, want at least a second", m.TTL())
	}

	token, _, err := m.Issue(uuid.New(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Consume(token); err != nil {
		t.Fatalf("a freshly issued token was already invalid: %v", err)
	}
}

// A token signed by somebody else's server must not open an account on this
// one — which is why the signing secret is per installation.
func TestPairingTokenFromAnotherServerIsRefused(t *testing.T) {
	theirs := NewPairingManager("their-secret", time.Minute)
	ours := NewPairingManager("our-secret", time.Minute)

	token, _, err := theirs.Issue(uuid.New(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ours.Consume(token); err == nil {
		t.Fatal("accepted a token this server never issued")
	}
}

// "alg: none" is the oldest JWT attack there is, and the check that stops it
// is one line that is easy to drop.
func TestPairingTokenRejectsUnsignedToken(t *testing.T) {
	m := NewPairingManager("secret", time.Minute)

	// {"alg":"none","typ":"JWT"} . {"uid":"...","exp":<far future>} . <empty>
	unsigned := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJ1aWQiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDEiLCJleHAiOjQxMDI0NDQ4MDAsImp0aSI6ImEifQ."
	if _, err := m.Consume(unsigned); err == nil {
		t.Fatal("accepted an unsigned token")
	}
}

// The used-set is memory that grows with every pairing, so it has to shed
// entries it can no longer be asked about.
func TestSpentTokensAreForgottenOnceExpired(t *testing.T) {
	// One second is the shortest lifetime JWT expiry can express, so it is
	// also the shortest this test can wait out.
	m := NewPairingManager("secret", time.Second)
	for range 5 {
		token, _, err := m.Issue(uuid.New(), "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Consume(token); err != nil {
			t.Fatal(err)
		}
	}

	m.mu.Lock()
	before := len(m.used)
	m.mu.Unlock()
	if before != 5 {
		t.Fatalf("remembered %d spent tokens, want 5", before)
	}

	time.Sleep(1200 * time.Millisecond)
	// Consuming anything sweeps; the token itself is expired and rejected.
	fresh, _, _ := m.Issue(uuid.New(), "")
	_, _ = m.Consume(fresh)

	m.mu.Lock()
	after := len(m.used)
	m.mu.Unlock()
	if after > 1 {
		t.Fatalf("still remembering %d expired tokens", after)
	}
}

func TestPairingConsumeIsSafeUnderConcurrency(t *testing.T) {
	m := NewPairingManager("secret", time.Minute)
	token, _, err := m.Issue(uuid.New(), "")
	if err != nil {
		t.Fatal(err)
	}

	// Two devices scanning the same code at once must not both get in.
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := m.Consume(token)
			results <- err
		}()
	}
	var ok, spent int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			ok++
		case errors.Is(err, ErrPairingTokenUsed):
			spent++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 || spent != 1 {
		t.Fatalf("%d succeeded and %d were refused, want exactly one of each", ok, spent)
	}
}

// Package crypto holds the Ed25519 keys, signatures and SHA-256 hashes shared
// by the node and its clients. An account's address is its public key in
// lowercase hex.
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DevSeedPrefix is prepended to an account name to derive its development
// key. The web app derives the same keys, so both sides agree on who is who.
const DevSeedPrefix = "car-rental dev account: "

// Key is an Ed25519 keypair.
type Key struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

// FromSeed builds the keypair for a 32-byte Ed25519 seed.
func FromSeed(seed []byte) Key {
	priv := ed25519.NewKeyFromSeed(seed)
	return Key{Public: priv.Public().(ed25519.PublicKey), Private: priv}
}

// DevKey returns the well-known development key for name. Like Hardhat's
// pre-funded accounts, these keys are public knowledge and must never guard
// anything of value.
func DevKey(name string) Key {
	seed := sha256.Sum256([]byte(DevSeedPrefix + name))
	return FromSeed(seed[:])
}

// Address returns the account address for k.
func (k Key) Address() string { return hex.EncodeToString(k.Public) }

// Sign returns the hex signature of SHA-512(msg). This is exactly what
// nacl.sign.detached(nacl.hash(msg), secretKey) produces in the browser.
func (k Key) Sign(msg []byte) string {
	digest := sha512.Sum512(msg)
	return hex.EncodeToString(ed25519.Sign(k.Private, digest[:]))
}

// Verify reports whether sigHex is addr's signature over SHA-512(msg).
func Verify(addr string, msg []byte, sigHex string) bool {
	if !IsAddress(addr) {
		return false
	}
	pub, _ := hex.DecodeString(addr)
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	digest := sha512.Sum512(msg)
	return ed25519.Verify(pub, digest[:], sig)
}

// IsAddress reports whether s is 64 lowercase hex characters. Only one
// spelling is accepted because addresses are ledger map keys: "AB.." and
// "ab.." must not become two different accounts.
func IsAddress(s string) bool {
	if len(s) != 2*ed25519.PublicKeySize {
		return false
	}
	for _, c := range s {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

// Load reads a key file holding a hex-encoded 32-byte seed.
func Load(path string) (Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Key{}, err
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return Key{}, fmt.Errorf("%s: expected a hex-encoded %d-byte seed", path, ed25519.SeedSize)
	}
	return FromSeed(seed), nil
}

// LoadOrCreate loads the key at path, or generates a random one and saves it
// there if the file does not exist yet.
func LoadOrCreate(path string) (Key, error) {
	k, err := Load(path)
	if !errors.Is(err, os.ErrNotExist) {
		return k, err
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return Key{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Key{}, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(seed)+"\n"), 0o600); err != nil {
		return Key{}, err
	}
	return FromSeed(seed), nil
}

// Hash is a SHA-256 digest. In JSON it is 0x-prefixed hex, and the zero hash
// (nothing recorded yet) is "".
type Hash [32]byte

// Sum returns the SHA-256 hash of data.
func Sum(data []byte) Hash { return sha256.Sum256(data) }

func (h Hash) IsZero() bool   { return h == Hash{} }
func (h Hash) String() string { return "0x" + hex.EncodeToString(h[:]) }

func (h Hash) MarshalJSON() ([]byte, error) {
	if h.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(h.String())
}

func (h *Hash) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("hash must be a hex string")
	}
	parsed, err := ParseHash(s)
	if err != nil {
		return err
	}
	*h = parsed
	return nil
}

// ParseHash accepts 64 hex characters, with or without a 0x prefix; "" is the
// zero hash.
func ParseHash(s string) (Hash, error) {
	var h Hash
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return h, nil
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(h) {
		return h, errors.New("hash must be 32 bytes of hex")
	}
	copy(h[:], b)
	return h, nil
}

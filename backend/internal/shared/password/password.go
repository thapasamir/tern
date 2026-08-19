// Package password hashes and verifies passwords with argon2id, the algorithm OWASP recommends.
//
// Hashes are stored as PHC strings, which carry their own parameters and salt:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<base64 salt>$<base64 hash>
//
// Because the parameters travel with the hash, they can be raised later without invalidating
// existing passwords: Verify reads whatever each hash was made with.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are the argon2id cost settings.
type Params struct {
	MemoryKiB uint32 // memory used per hash
	Time      uint32 // passes over that memory
	Threads   uint8
}

// DefaultParams follow OWASP's minimum recommendation for argon2id (19 MiB, 2 passes, 1 thread).
var DefaultParams = Params{MemoryKiB: 19 * 1024, Time: 2, Threads: 1}

const (
	saltBytes = 16
	keyBytes  = 32
)

// ErrMalformedHash means a stored hash is not a valid argon2id PHC string.
var ErrMalformedHash = errors.New("password: malformed hash")

// Hasher creates and checks password hashes with one set of parameters for new hashes.
type Hasher struct {
	params Params
}

func NewHasher(p Params) *Hasher { return &Hasher{params: p} }

// Hash returns the PHC string for password, using a fresh random salt.
func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, keyBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Time, h.params.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether password matches the stored hash. The comparison takes the same time
// whether or not the password is close. An unreadable hash is an error, not a mismatch.
func (h *Hasher) Verify(password, stored string) (bool, error) {
	p, salt, want, err := parse(stored)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func parse(stored string) (Params, []byte, []byte, error) {
	parts := strings.Split(stored, "$")
	// "$argon2id$v=19$m=..,t=..,p=..$salt$hash" splits into ["", "argon2id", "v=19", params, salt, hash].
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, ErrMalformedHash
	}
	var p Params
	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &threads); err != nil || threads == 0 || threads > 255 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	p.Threads = uint8(threads)
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	return p, salt, key, nil
}

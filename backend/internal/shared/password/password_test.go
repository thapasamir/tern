package password

import (
	"errors"
	"strings"
	"testing"
)

// cheap keeps the tests fast; the algorithm is the same as with DefaultParams.
var cheap = NewHasher(Params{MemoryKiB: 8, Time: 1, Threads: 1})

func TestHashThenVerify(t *testing.T) {
	hash, err := cheap.Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=8,t=1,p=1$") {
		t.Fatalf("hash %q is not an argon2id PHC string with the configured parameters", hash)
	}
	if ok, err := cheap.Verify("correct horse", hash); err != nil || !ok {
		t.Fatalf("Verify(right password) = %v, %v", ok, err)
	}
	if ok, err := cheap.Verify("wrong horse", hash); err != nil || ok {
		t.Fatalf("Verify(wrong password) = %v, %v", ok, err)
	}
}

func TestSaltMakesEveryHashDifferent(t *testing.T) {
	a, _ := cheap.Hash("same")
	b, _ := cheap.Hash("same")
	if a == b {
		t.Fatal("two hashes of the same password were identical: the salt is not random")
	}
}

// A hash made with other parameters must still verify: that is what lets the cost be raised.
func TestVerifyUsesTheParametersStoredInTheHash(t *testing.T) {
	hash, _ := NewHasher(Params{MemoryKiB: 16, Time: 2, Threads: 2}).Hash("pw")
	if ok, err := cheap.Verify("pw", hash); err != nil || !ok {
		t.Fatalf("Verify = %v, %v, want a match", ok, err)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	good, _ := cheap.Hash("pw")
	for name, stored := range map[string]string{
		"empty":         "",
		"plain text":    "hunter2",
		"wrong algo":    strings.Replace(good, "argon2id", "bcrypt", 1),
		"wrong version": strings.Replace(good, "v=19", "v=16", 1),
		"bad params":    strings.Replace(good, "m=8,t=1,p=1", "m=x", 1),
		"zero threads":  strings.Replace(good, "p=1", "p=0", 1),
		"bad salt":      strings.Replace(good, "$", "$!", 4),
		"truncated":     good[:strings.LastIndex(good, "$")],
	} {
		if _, err := cheap.Verify("pw", stored); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("%s: err = %v, want ErrMalformedHash", name, err)
		}
	}
}

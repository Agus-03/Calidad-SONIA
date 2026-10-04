package encryption

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCipher(t *testing.T) *Cipher {
	t.Helper()

	key := make([]byte, KeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	c, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestWhatGoesInComesBack is the whole point, checked on the four shapes the sensitive columns actually hold.
func TestWhatGoesInComesBack(t *testing.T) {
	c := testCipher(t)

	for _, plaintext := range []string{
		"20123456789",             // CUIT
		"0170099220000067797151",  // CBU
		"36.415.782",              // DNI as a human types it
		strings.Repeat("9", 1024), // longer than one AES block, several times over
	} {
		stored, err := c.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if bytes.Contains(stored, []byte(plaintext)) {
			t.Fatalf("the ciphertext of %q contains it in the clear", plaintext)
		}

		back, err := c.Decrypt(stored)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if back != plaintext {
			t.Fatalf("round trip gave %q, want %q", back, plaintext)
		}
	}
}

// TestTheSameNumberEncryptsDifferentlyEveryTime is why nothing can be searched or indexed by these columns, and why it is deliberate (ADR-0031).
func TestTheSameNumberEncryptsDifferentlyEveryTime(t *testing.T) {
	c := testCipher(t)
	const dni = "36415782"

	first, err := c.Encrypt(dni)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	second, err := c.Encrypt(dni)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if bytes.Equal(first, second) {
		t.Fatalf("the same value encrypted to the same bytes twice, so a nonce is being reused")
	}
	// Both must still read back, or the difference would be a bug rather than the nonce.
	for _, stored := range [][]byte{first, second} {
		if back, err := c.Decrypt(stored); err != nil || back != dni {
			t.Fatalf("Decrypt gave %q, %v, want %q and no error", back, err, dni)
		}
	}
}

// TestAnEmptyValueStoresNothing keeps a contact without a CBU from holding the ciphertext of the empty string.
func TestAnEmptyValueStoresNothing(t *testing.T) {
	c := testCipher(t)

	stored, err := c.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if stored != nil {
		t.Fatalf("an empty value stored %d bytes, want nil so the column is NULL", len(stored))
	}

	back, err := c.Decrypt(nil)
	if err != nil || back != "" {
		t.Fatalf("Decrypt(nil) gave %q, %v, want an empty string and no error", back, err)
	}
}

// TestATamperedValueRefusesInsteadOfAnsweringSomethingElse is the property that made GCM the choice: for a CBU, a wrong number is worse than an error.
func TestATamperedValueRefusesInsteadOfAnsweringSomethingElse(t *testing.T) {
	c := testCipher(t)

	stored, err := c.Encrypt("0170099220000067797151")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	for _, tc := range []struct {
		name   string
		break_ func([]byte) []byte
	}{
		{"un bit del ciphertext", func(b []byte) []byte {
			out := bytes.Clone(b)
			out[len(out)-1] ^= 0x01
			return out
		}},
		{"un bit del nonce", func(b []byte) []byte {
			out := bytes.Clone(b)
			out[3] ^= 0x01
			return out
		}},
		{"el byte de version", func(b []byte) []byte {
			out := bytes.Clone(b)
			out[0] = 0x02
			return out
		}},
		{"truncado", func(b []byte) []byte { return b[:len(b)-4] }},
		{"demasiado corto para tener forma", func(b []byte) []byte { return b[:5] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Decrypt(tc.break_(stored)); !errors.Is(err, ErrCorrupted) {
				t.Fatalf("got %v, want ErrCorrupted", err)
			}
		})
	}
}

// TestAKeyFromAnotherDeploymentCannotRead is the cost written down: the key lost is the data lost, and it fails loudly.
func TestAKeyFromAnotherDeploymentCannotRead(t *testing.T) {
	mine, theirs := testCipher(t), testCipher(t)

	stored, err := mine.Encrypt("36415782")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := theirs.Decrypt(stored); !errors.Is(err, ErrCorrupted) {
		t.Fatalf("another key decrypted it with %v, want ErrCorrupted", err)
	}
}

// TestNewRefusesAKeyThatIsNotAes256 stops the wrong key at boot, which is the only moment it can still be stopped cheaply.
func TestNewRefusesAKeyThatIsNotAes256(t *testing.T) {
	for _, size := range []int{0, 1, 16, 31, 33, 64} {
		if _, err := New(make([]byte, size)); err == nil {
			t.Fatalf("New accepted a key of %d bytes, want a refusal", size)
		}
	}
}

// TestDecodeKeyRefusesWhatTheEnvironmentCanGetWrong covers the two ways the variable is typed badly.
func TestDecodeKeyRefusesWhatTheEnvironmentCanGetWrong(t *testing.T) {
	for _, tc := range []struct{ name, encoded string }{
		{"no es base64", "esto no es base64!!"},
		{"base64 valido pero corto", base64.StdEncoding.EncodeToString(make([]byte, 16))},
		{"vacio", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeKey(tc.encoded); err == nil {
				t.Fatalf("DecodeKey(%q) was accepted, want a refusal", tc.encoded)
			}
		})
	}

	key := make([]byte, KeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	got, err := DecodeKey(base64.StdEncoding.EncodeToString(key))
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("DecodeKey of a good key gave %v, %v", got, err)
	}
}

// TestAFileGoesInAndComesBackInTheColumnFormat is ADR-0064, regla 1: a document is sealed exactly like a column, so the worker
// reads it with the rules the columns already have.
func TestAFileGoesInAndComesBackInTheColumnFormat(t *testing.T) {
	c := testCipher(t)
	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{0x00, 0xff, '\n'}, 4096)...)

	stored, err := c.EncryptBytes(pdf)
	if err != nil {
		t.Fatalf("EncryptBytes: %v", err)
	}
	if stored[0] != version || bytes.Contains(stored, []byte("%PDF-")) {
		t.Fatalf("the sealed file does not start with the version or carries the signature in the clear")
	}
	back, err := c.DecryptBytes(stored)
	if err != nil || !bytes.Equal(back, pdf) {
		t.Fatalf("DecryptBytes: got %d bytes and %v, want the %d of the file", len(back), err, len(pdf))
	}
	// The column reader opens it too, which is what "the same format" means.
	if text, err := c.Decrypt(stored); err != nil || text != string(pdf) {
		t.Fatalf("Decrypt of a sealed file: %v", err)
	}

	if empty, err := c.EncryptBytes(nil); empty != nil || err != nil {
		t.Fatalf("EncryptBytes(nil): got %v and %v, want nil and nil", empty, err)
	}
	if empty, err := c.DecryptBytes(nil); empty != nil || err != nil {
		t.Fatalf("DecryptBytes(nil): got %v and %v, want nil and nil", empty, err)
	}
	stored[len(stored)-1] ^= 0x01
	if _, err := c.DecryptBytes(stored); !errors.Is(err, ErrCorrupted) {
		t.Fatalf("a flipped bit: got %v, want ErrCorrupted", err)
	}
}

// TestTheSharedVectorOpensWithTheGoReader is ADR-0064 §4 from the core's side: the vector in contracts/fixtures was sealed by
// this package, and the worker's test opens the same file with Python. If either side changes the format, one of the two fails.
func TestTheSharedVectorOpensWithTheGoReader(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "contracts", "fixtures", "document-cipher.json"))
	if err != nil {
		t.Fatalf("reading the shared vector: %v", err)
	}
	var vector struct{ Key, Plaintext, Sealed string }
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatalf("the shared vector is not json: %v", err)
	}
	key, err := DecodeKey(vector.Key)
	if err != nil {
		t.Fatalf("DecodeKey: %v", err)
	}
	c, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, _ := base64.StdEncoding.DecodeString(vector.Sealed)
	want, _ := base64.StdEncoding.DecodeString(vector.Plaintext)
	got, err := c.DecryptBytes(sealed)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("the shared vector does not open back into its plaintext: %v", err)
	}
}

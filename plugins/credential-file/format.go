package credentialfile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/argon2"
)

// On-disk file layout (version 1):
//
//     | magic (14 B) | version (1 B) | salt (16 B) | nonce (12 B) | ciphertext (N B) |
//
//   magic       = "GOREMOTE-CRED\x01"
//   version     = 0x01
//   salt        = random bytes fed to Argon2id KDF
//   nonce       = random bytes used as AES-GCM nonce
//   ciphertext  = AES-256-GCM(key, nonce, JSON-encoded `vault`) with the
//                 file's magic+version+salt+nonce prefix as associated data,
//                 binding the header to the ciphertext.
//
// The Argon2id parameters are chosen to target roughly ~100 ms on a modern
// workstation while remaining usable on laptops. They are deliberately not
// configurable through the file so that every file written by this provider
// has identical derivation cost and the format stays self-describing.

const (
	// ArgonTime is the Argon2id iteration count.
	ArgonTime uint32 = 1
	// ArgonMemoryKiB is the Argon2id memory cost in KiB (64 MiB).
	ArgonMemoryKiB uint32 = 64 * 1024
	// ArgonThreads is the Argon2id parallelism parameter.
	ArgonThreads uint8 = 4
	// ArgonKeyLen is the length in bytes of the derived AES-256 key.
	ArgonKeyLen uint32 = 32

	saltLen   = 16
	nonceLen  = 12
	version1  = 0x01
	headerLen = len(magic) + 1 + saltLen + nonceLen // magic+ver+salt+nonce
)

// magic is the file type marker. Its trailing 0x01 is independent of the
// version byte that follows it; the constant itself never changes, while the
// version byte allows future format migrations.
const magic = "GOREMOTE-CRED\x01"

// ErrBadMagic is returned when the file header does not start with the
// expected magic bytes.
var ErrBadMagic = errors.New("credential-file: bad magic header")

// ErrUnsupportedVersion is returned for files written by a newer format.
var ErrUnsupportedVersion = errors.New("credential-file: unsupported file format version")

// ErrShortFile is returned when the file is too small to contain a valid
// header + GCM tag.
var ErrShortFile = errors.New("credential-file: file is truncated")

// entry is a single credential record inside the vault. Fields are optional;
// providers populate the subset relevant to the secret kind.
type entry struct {
	ID         string            `json:"id"`
	Username   string            `json:"username,omitempty"`
	Password   string            `json:"password,omitempty"`
	Domain     string            `json:"domain,omitempty"`
	PrivateKey []byte            `json:"private_key,omitempty"`
	Passphrase string            `json:"passphrase,omitempty"`
	OTP        string            `json:"otp,omitempty"`
	Hints      map[string]string `json:"hints,omitempty"`
	Notes      string            `json:"notes,omitempty"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// vault is the plaintext document encrypted into the file.
type vault struct {
	Version int     `json:"version"`
	Entries []entry `json:"entries"`
}

// deriveKey runs Argon2id with the package's fixed parameters.
func deriveKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, ArgonTime, ArgonMemoryKiB, ArgonThreads, ArgonKeyLen)
}

// encodeFile produces the on-disk byte payload for the given plaintext vault
// using a freshly generated salt and nonce.
func encodeFile(v *vault, passphrase string) ([]byte, error) {
	plaintext, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal vault: %w", err)
	}

	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("random salt: %w", err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("random nonce: %w", err)
	}

	key := deriveKey(passphrase, salt)
	defer zero(key)

	return sealWith(key, salt, nonce, plaintext)
}

// encodeFileWithKey is like encodeFile but reuses an already-derived key and
// salt, avoiding the Argon2id cost on every save. The nonce is always fresh.
func encodeFileWithKey(v *vault, key, salt []byte) ([]byte, error) {
	plaintext, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal vault: %w", err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("random nonce: %w", err)
	}
	return sealWith(key, salt, nonce, plaintext)
}

// sealWith encrypts plaintext and assembles the final file byte layout.
func sealWith(key, salt, nonce, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	out := make([]byte, 0, headerLen+len(plaintext)+aead.Overhead())
	out = append(out, magic...)
	out = append(out, version1)
	out = append(out, salt...)
	out = append(out, nonce...)

	// Bind header to ciphertext as AAD so tampering with the salt/nonce/
	// version is detected by GCM authentication.
	aad := append([]byte(nil), out...)
	out = aead.Seal(out, nonce, plaintext, aad)
	return out, nil
}

// decodeFile parses a file payload and decrypts the vault. It returns the
// decoded vault along with the derived key and salt so callers can reuse
// them for subsequent saves without paying the Argon2id cost again.
func decodeFile(data []byte, passphrase string) (*vault, []byte, []byte, error) {
	if len(data) < headerLen+16 /* min GCM tag */ {
		return nil, nil, nil, ErrShortFile
	}
	if string(data[:len(magic)]) != magic {
		return nil, nil, nil, ErrBadMagic
	}
	ver := data[len(magic)]
	if ver != version1 {
		return nil, nil, nil, fmt.Errorf("%w: %d", ErrUnsupportedVersion, ver)
	}
	off := len(magic) + 1
	salt := data[off : off+saltLen]
	off += saltLen
	nonce := data[off : off+nonceLen]
	off += nonceLen
	ciphertext := data[off:]

	key := deriveKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		zero(key)
		return nil, nil, nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		zero(key)
		return nil, nil, nil, fmt.Errorf("gcm: %w", err)
	}

	aad := data[:off]
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		zero(key)
		return nil, nil, nil, err
	}
	var v vault
	if err := json.Unmarshal(plaintext, &v); err != nil {
		zero(key)
		return nil, nil, nil, fmt.Errorf("unmarshal vault: %w", err)
	}
	// return copies of salt/key so caller owns them
	saltCopy := append([]byte(nil), salt...)
	return &v, key, saltCopy, nil
}

// zero wipes a byte slice in place.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

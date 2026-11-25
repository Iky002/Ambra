package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      uint32 = 64 * 1024
	argonIterations  uint32 = 3
	argonParallelism uint8  = 2
	argonSaltLen     uint32 = 16
	argonKeyLen      uint32 = 32

	minArgonMemory      uint64 = 8 * 1024
	maxArgonMemory      uint64 = 512 * 1024
	minArgonIterations  uint64 = 1
	maxArgonIterations  uint64 = 16
	minArgonParallelism uint64 = 1
	maxArgonParallelism uint64 = 16
	minArgonHashLen     int    = 16
	maxArgonHashLen     int    = 1024
)

// HashPassword hashes a password using Argon2id.
// Empty password is allowed for local Emby users and stored as an empty string.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLen)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism, b64Salt, b64Hash), nil
}

// VerifyPassword verifies a plaintext password against an Argon2id encoded hash.
// Empty hash matches only an empty password.
func VerifyPassword(password, encodedHash string) (bool, error) {
	if encodedHash == "" {
		return password == "", nil
	}
	if password == "" {
		return false, nil
	}
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("invalid argon2id hash format")
	}

	var version int
	if n, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || n != 1 || fmt.Sprintf("v=%d", version) != parts[2] {
		return false, errors.New("invalid argon2id version")
	}
	if version != argon2.Version {
		return false, fmt.Errorf("unsupported argon2id version: %d", version)
	}

	var memory, iterations, parallelism uint64
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil || n != 3 ||
		fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, parallelism) != parts[3] {
		return false, errors.New("invalid argon2id parameters")
	}
	if parallelism < minArgonParallelism || parallelism > maxArgonParallelism ||
		iterations < minArgonIterations || iterations > maxArgonIterations ||
		memory < minArgonMemory || memory > maxArgonMemory {
		return false, errors.New("argon2id parameters out of safe bounds")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	if len(expectedHash) < minArgonHashLen || len(expectedHash) > maxArgonHashLen {
		return false, errors.New("invalid argon2id hash length")
	}

	computed := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(expectedHash)))
	return subtle.ConstantTimeCompare(computed, expectedHash) == 1, nil
}

var dummyTimingSalt = []byte("ambra-timing-pad")

// dummyPasswordWork performs an Argon2id derivation with the same cost parameters as
// VerifyPassword to mitigate username enumeration via response timing differences.
func dummyPasswordWork(password string) {
	pw := password
	if pw == "" {
		pw = "ambra-dummy-password"
	}
	_ = argon2.IDKey([]byte(pw), dummyTimingSalt, argonIterations, argonMemory, argonParallelism, argonKeyLen)
}

// GenerateToken returns a cryptographically random 32-byte hex string (64 chars).
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

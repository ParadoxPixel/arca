package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	scheme     = "pbkdf2-sha256"
	iterations = 100_000
	keyLength  = 32
	saltLength = 16

	TokenPrefix = "cnx_"
)

func derive(password string, salt []byte, rounds int) string {
	return hex.EncodeToString(pbkdf2.Key([]byte(password), salt, rounds, keyLength, sha256.New))
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	return fmt.Sprintf("%s$%d$%s$%s", scheme, iterations, hex.EncodeToString(salt), derive(password, salt, iterations)), nil
}

func VerifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != scheme {
		return false
	}

	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds <= 0 || rounds > iterations {
		return false
	}

	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(derive(password, salt, rounds)), []byte(parts[3])) == 1
}

func GenerateAPIToken() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func SHA256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

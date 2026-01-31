package agentauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

func GenerateToken(prefix string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(buf)
	return strings.TrimSpace(prefix) + encoded, nil
}

func HashToken(token string, salt string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(salt) + token))
	return hex.EncodeToString(sum[:])
}

func RoleRank(role string) int {
	switch role {
	case "owner":
		return 3
	case "admin":
		return 2
	case "viewer":
		return 1
	default:
		return 0
	}
}

func HasMinRole(role string, min string) bool {
	return RoleRank(role) >= RoleRank(min)
}

package users

import (
	"crypto/sha256"
	"encoding/base64"
)

func validShadowsocks2022Key(key string) bool {
	if len(key) != 24 && len(key) != 44 {
		return false
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(key)
	return err == nil && (len(raw) == 16 || len(raw) == 32) && base64.StdEncoding.EncodeToString(raw) == key
}

// PrepareShadowsocks2022 resolves validated input using the observed native key
// size. Explicit Key never falls back to hashing. Password conversion preserves
// an appropriately sized base64 PSK or uses SHA256 truncated to the required size.
// The returned copy has only its sensitive Key; the input remains caller-owned.
func PrepareShadowsocks2022(account Account, keyBytes int) (Account, error) {
	if keyBytes != 16 && keyBytes != 32 {
		return Account{}, ErrUnsupported
	}
	if account.Key != "" {
		raw, err := base64.StdEncoding.DecodeString(account.Key)
		if err != nil || len(raw) != keyBytes {
			return Account{}, ErrInvalid
		}
	} else {
		raw, err := base64.StdEncoding.DecodeString(account.Password)
		if err != nil || len(raw) != keyBytes {
			hash := sha256.Sum256([]byte(account.Password))
			raw = hash[:keyBytes]
		}
		account.Key = base64.StdEncoding.EncodeToString(raw)
	}
	account.Password = ""
	return account, nil
}

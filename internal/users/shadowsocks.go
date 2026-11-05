package users

// ShadowsocksMethod selects a legacy AEAD method, independently of transport.
type ShadowsocksMethod string

const (
	MethodAES128GCM         ShadowsocksMethod = "aes-128-gcm"
	MethodAES256GCM         ShadowsocksMethod = "aes-256-gcm"
	MethodChaCha20Poly1305  ShadowsocksMethod = "chacha20-poly1305"
	MethodXChaCha20Poly1305 ShadowsocksMethod = "xchacha20-poly1305"
)

func validShadowsocksMethod(method ShadowsocksMethod) bool {
	switch method {
	case MethodAES128GCM, MethodAES256GCM, MethodChaCha20Poly1305, MethodXChaCha20Poly1305:
		return true
	default:
		return false
	}
}

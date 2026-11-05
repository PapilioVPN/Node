package users

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestShadowsocks2022ExplicitConversionAndNativeSize(t *testing.T) {
	for _, test := range []struct {
		password string
		bytes    int
		key      string
	}{
		{"password", 16, "XohImNooBHFR0OVvjcYpJw=="},
		{"password", 32, "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg="},
		{"MDEyMzQ1Njc4OWFiY2RlZg==", 16, "MDEyMzQ1Njc4OWFiY2RlZg=="},
	} {
		input := Account{Email: "owned", Protocol: Shadowsocks2022, Password: test.password}
		prepared, err := PrepareShadowsocks2022(input, test.bytes)
		if err != nil || prepared.Key != test.key || prepared.Password != "" || input.Password != test.password {
			t.Fatal("explicit password conversion/ownership changed", err)
		}
	}
	input := Account{Email: "owned", Protocol: Shadowsocks2022, Key: "MDEyMzQ1Njc4OWFiY2RlZg=="}
	if _, err := PrepareShadowsocks2022(input, 32); !errors.Is(err, ErrInvalid) {
		t.Fatal("explicit wrong-size key was hashed", err)
	}
	if _, err := PrepareShadowsocks2022(input, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatal("missing authoritative method became a default key size", err)
	}
}

func FuzzShadowsocks2022Credentials(f *testing.F) {
	f.Add("password", byte(0), false)
	f.Add("MDEyMzQ1Njc4OWFiY2RlZg==", byte(0), true)
	f.Add("MDEyMzQ1Njc4OWFiY2RlZg==\n", byte(1), true)
	f.Fuzz(func(t *testing.T, value string, size byte, keyInput bool) {
		if len(value) > MaxPasswordBytes+1 {
			return
		}
		input := Account{Email: "owned", Protocol: Shadowsocks2022}
		if keyInput {
			input.Key = value
		} else {
			input.Password = value
		}
		if !validAccount(input) {
			return
		}
		keyBytes := 16 + int(size%2)*16
		before := input
		prepared, err := PrepareShadowsocks2022(input, keyBytes)
		if input != before {
			t.Fatal("credential preparation mutated input")
		}
		if err != nil {
			if !keyInput || !errors.Is(err, ErrInvalid) {
				t.Fatal("validated conversion failed", err)
			}
			return
		}
		raw, err := base64.StdEncoding.DecodeString(prepared.Key)
		if err != nil || len(raw) != keyBytes || prepared.Password != "" || !validAccount(prepared) {
			t.Fatal("prepared credential violates its declared key size")
		}
	})
}

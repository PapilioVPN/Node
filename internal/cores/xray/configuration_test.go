package xray

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

func TestAPIOverlayPreservesNativeServicesAndTag(t *testing.T) {
	for _, test := range []struct {
		name     string
		config   string
		tag      string
		services []string
	}{
		{"absent-api", `{"outbounds":[{"protocol":"freedom"}]}`, "papilio-api", []string{"StatsService"}},
		{"null-api", `{"api":null}`, "papilio-api", []string{"StatsService"}},
		{"existing-services", `{"api":{"tag":"custom","listen":"127.0.0.1:1234","services":["HandlerService","LoggerService"]}}`, "custom", []string{"HandlerService", "LoggerService", "StatsService"}},
		{"native-case-matching", `{"API":{"TAG":"custom","SERVICES":["statsservice","HandlerService"]}}`, "custom", []string{"statsservice", "HandlerService"}},
		{"native-duplicate-object-merge", `{"api":{"tag":"custom"},"api":{"services":["HandlerService"]}}`, "custom", []string{"HandlerService", "StatsService"}},
		{"empty-tag-remains-invalid", `{"api":{"tag":""}}`, "", []string{"StatsService"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := []byte(test.config)
			before := bytes.Clone(input)
			overlay, err := prepareAPIOverlay(input, "/owned/api.sock")
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				API apiConfiguration `json:"api"`
			}
			if err := json.Unmarshal(overlay, &result); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(input, before) || result.API.Listen != "/owned/api.sock" || result.API.Tag != test.tag || !slices.Equal(result.API.Services, test.services) {
				t.Fatalf("overlay changed native identity/services or input: %s", overlay)
			}
		})
	}
}

func TestAPIOverlayRejectsInvalidManagementShapes(t *testing.T) {
	for _, input := range []string{"null", `[]`, `{"api":1}`, `{"api":{"services":{}}}`, `{"api":{"tag":123}}`, `{`} {
		if _, err := prepareAPIOverlay([]byte(input), "/owned/api.sock"); err == nil {
			t.Errorf("accepted invalid management shape %s", input)
		}
	}
}

func FuzzAPIOverlay(f *testing.F) {
	for _, seed := range []string{`{}`, `{"api":null}`, `{"api":{"tag":"api","services":["HandlerService"]}}`, "null", `{"api":[]} `} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4096 {
			t.Skip()
		}
		before := bytes.Clone(input)
		overlay, err := prepareAPIOverlay(input, "/owned/api.sock")
		if !bytes.Equal(input, before) {
			t.Fatal("preparation mutated the captured input")
		}
		if err != nil {
			return
		}
		var result struct {
			API apiConfiguration `json:"api"`
		}
		if err := json.Unmarshal(overlay, &result); err != nil || result.API.Listen != "/owned/api.sock" || len(result.API.Services) == 0 {
			t.Fatalf("invalid owned overlay: %s, %v", overlay, err)
		}
	})
}

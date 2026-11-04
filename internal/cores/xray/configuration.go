package xray

import (
	"encoding/json"
	"errors"
	"strings"
)

// apiConfiguration projects the three fields understood by the pinned engine.
type apiConfiguration struct {
	Tag      string   `json:"tag"`
	Listen   string   `json:"listen"`
	Services []string `json:"services"`
}

func prepareAPIOverlay(config []byte, socket string) ([]byte, error) {
	var document *struct {
		API *apiConfiguration `json:"api"`
	}
	if err := json.Unmarshal(config, &document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, errors.New("Xray configuration must be a JSON object")
	}
	api := document.API
	if api == nil {
		api = &apiConfiguration{Tag: "papilio-api"}
	}
	api.Listen = socket
	hasStats := false
	for _, service := range api.Services {
		if strings.EqualFold(service, "StatsService") {
			hasStats = true
			break
		}
	}
	if !hasStats {
		api.Services = append(api.Services, "StatsService")
	}
	return json.Marshal(struct {
		API *apiConfiguration `json:"api"`
	}{API: api})
}

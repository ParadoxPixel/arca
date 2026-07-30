package docker

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Config is the subset of an image config blob worth showing. Times are
// milliseconds since the epoch, as every other timestamp in this server is.
type Config struct {
	Architecture string
	OS           string
	Variant      string
	OSVersion    string
	Created      int64
	User         string
	WorkingDir   string
	Entrypoint   []string
	Cmd          []string
	Env          []string
	Labels       map[string]string
	ExposedPorts []string
	DiffIDs      []string
	History      []HistoryEntry
}

// HistoryEntry is one build step. An empty layer marks an instruction that
// changed metadata without producing a filesystem layer, which is what makes the
// history longer than the layer list.
type HistoryEntry struct {
	Created    int64
	CreatedBy  string
	Comment    string
	EmptyLayer bool
}

type configDocument struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant"`
	OSVersion    string `json:"os.version"`
	Created      string `json:"created"`
	Config       struct {
		User         string            `json:"User"`
		WorkingDir   string            `json:"WorkingDir"`
		Entrypoint   []string          `json:"Entrypoint"`
		Cmd          []string          `json:"Cmd"`
		Env          []string          `json:"Env"`
		Labels       map[string]string `json:"Labels"`
		ExposedPorts map[string]any    `json:"ExposedPorts"`
	} `json:"config"`
	RootFS struct {
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
	History []struct {
		Created    string `json:"created"`
		CreatedBy  string `json:"created_by"`
		Comment    string `json:"comment"`
		EmptyLayer bool   `json:"empty_layer"`
	} `json:"history"`
}

// millis reads an RFC 3339 timestamp. An absent or unreadable one reports zero
// rather than failing the parse: a config that cannot say when it was built is
// still worth everything else it carries.
func millis(raw string) int64 {
	if raw == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}

func ParseConfig(document []byte) (Config, error) {
	var parsed configDocument
	if err := json.Unmarshal(document, &parsed); err != nil {
		return Config{}, fmt.Errorf("docker: reading an image config: %w", err)
	}

	config := Config{
		Architecture: parsed.Architecture,
		OS:           parsed.OS,
		Variant:      parsed.Variant,
		OSVersion:    parsed.OSVersion,
		Created:      millis(parsed.Created),
		User:         parsed.Config.User,
		WorkingDir:   parsed.Config.WorkingDir,
		Entrypoint:   parsed.Config.Entrypoint,
		Cmd:          parsed.Config.Cmd,
		Env:          parsed.Config.Env,
		Labels:       parsed.Config.Labels,
		DiffIDs:      parsed.RootFS.DiffIDs,
	}

	// The ports arrive as a JSON object, so they are sorted rather than left in
	// map order, which would reshuffle the UI on every read.
	for port := range parsed.Config.ExposedPorts {
		config.ExposedPorts = append(config.ExposedPorts, port)
	}
	sort.Strings(config.ExposedPorts)

	for _, entry := range parsed.History {
		config.History = append(config.History, HistoryEntry{
			Created:    millis(entry.Created),
			CreatedBy:  entry.CreatedBy,
			Comment:    entry.Comment,
			EmptyLayer: entry.EmptyLayer,
		})
	}

	return config, nil
}

func (c Config) Platform() Platform {
	return Platform{
		Architecture: c.Architecture,
		OS:           c.OS,
		Variant:      c.Variant,
		OSVersion:    c.OSVersion,
	}
}

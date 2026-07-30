package docker

import "testing"

const fullConfig = `{
  "architecture": "amd64",
  "os": "linux",
  "created": "2026-07-30T10:11:12Z",
  "config": {
    "User": "app",
    "WorkingDir": "/srv",
    "Entrypoint": ["/bin/arca"],
    "Cmd": ["--port", "8080"],
    "Env": ["PATH=/usr/bin", "PORT=8080"],
    "Labels": {"org.opencontainers.image.source": "https://github.com/pixelib/arca"},
    "ExposedPorts": {"8080/tcp": {}, "443/tcp": {}}
  },
  "rootfs": {
    "type": "layers",
    "diff_ids": ["sha256:1111111111111111111111111111111111111111111111111111111111111111"]
  },
  "history": [
    {"created": "2026-07-30T10:11:10Z", "created_by": "COPY . /srv"},
    {"created": "2026-07-30T10:11:11Z", "created_by": "ENV PORT=8080", "empty_layer": true}
  ]
}`

func TestParseConfig(t *testing.T) {
	config, err := ParseConfig([]byte(fullConfig))
	if err != nil {
		t.Fatalf("ParseConfig returned %v", err)
	}

	if config.Architecture != "amd64" || config.OS != "linux" {
		t.Fatalf("platform = %q, want linux/amd64", config.Platform().String())
	}
	if config.Created != 1785406272000 {
		t.Fatalf("Created = %d, want 1785406272000", config.Created)
	}
	if config.User != "app" || config.WorkingDir != "/srv" {
		t.Fatalf("User = %q, WorkingDir = %q", config.User, config.WorkingDir)
	}
	if len(config.Entrypoint) != 1 || config.Entrypoint[0] != "/bin/arca" {
		t.Fatalf("Entrypoint = %v", config.Entrypoint)
	}
	if len(config.Env) != 2 {
		t.Fatalf("Env = %v, want 2 entries", config.Env)
	}
	if config.Labels["org.opencontainers.image.source"] == "" {
		t.Fatalf("Labels = %v, want the source label", config.Labels)
	}
	if len(config.DiffIDs) != 1 {
		t.Fatalf("DiffIDs = %v, want 1 entry", config.DiffIDs)
	}
}

// Ports arrive as a JSON object, so they are sorted rather than left in map
// order, which would reshuffle the UI between reads of the same config.
func TestParseConfigSortsExposedPorts(t *testing.T) {
	config, err := ParseConfig([]byte(fullConfig))
	if err != nil {
		t.Fatalf("ParseConfig returned %v", err)
	}

	want := []string{"443/tcp", "8080/tcp"}
	if len(config.ExposedPorts) != len(want) {
		t.Fatalf("ExposedPorts = %v, want %v", config.ExposedPorts, want)
	}
	for index := range want {
		if config.ExposedPorts[index] != want[index] {
			t.Fatalf("ExposedPorts = %v, want %v", config.ExposedPorts, want)
		}
	}
}

// The history is longer than the layer list, because a metadata-only
// instruction produces an entry with no filesystem layer behind it.
func TestParseConfigHistory(t *testing.T) {
	config, err := ParseConfig([]byte(fullConfig))
	if err != nil {
		t.Fatalf("ParseConfig returned %v", err)
	}

	if len(config.History) != 2 {
		t.Fatalf("History = %v, want 2 entries", config.History)
	}
	if config.History[0].EmptyLayer {
		t.Fatal("the first history entry reported an empty layer")
	}
	if !config.History[1].EmptyLayer {
		t.Fatal("the second history entry did not report an empty layer")
	}
	if config.History[0].Created == 0 {
		t.Fatal("the first history entry has no timestamp")
	}
}

// A config missing every optional field still has to parse. Partially populated
// configs are normal, and common in a repository that was migrated rather than
// pushed.
func TestParseConfigPartial(t *testing.T) {
	cases := []struct {
		name     string
		document string
	}{
		{"empty object", `{}`},
		{"no config block", `{"architecture":"amd64","os":"linux"}`},
		{"no timestamp", `{"architecture":"amd64","config":{"Cmd":["sh"]}}`},
		{"unparseable timestamp", `{"created":"yesterday"}`},
		{"null labels", `{"config":{"Labels":null}}`},
		{"no history", `{"architecture":"amd64","rootfs":{"diff_ids":[]}}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config, err := ParseConfig([]byte(testCase.document))
			if err != nil {
				t.Fatalf("ParseConfig returned %v", err)
			}
			// A config that cannot say when it was built is still worth
			// everything else it carries, so an unreadable time reports zero
			// rather than failing the parse.
			if config.Created != 0 {
				t.Fatalf("Created = %d, want 0", config.Created)
			}
		})
	}
}

func TestParseConfigRejectsGarbage(t *testing.T) {
	for _, document := range []string{"", "not json", "[]"} {
		t.Run(document, func(t *testing.T) {
			if _, err := ParseConfig([]byte(document)); err == nil {
				t.Fatal("ParseConfig accepted a document that is not a config")
			}
		})
	}
}

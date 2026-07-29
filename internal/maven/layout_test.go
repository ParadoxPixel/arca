package maven

import "testing"

func TestLayoutMutable(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"com/example/app/maven-metadata.xml", true},
		{"com/example/app/maven-metadata.xml.sha1", true},
		{"com/example/app/1.0.0-SNAPSHOT/maven-metadata.xml", true},
		{"com/example/app/1.0.0-SNAPSHOT/app-1.0.0-SNAPSHOT.jar", true},
		{"com/example/app/1.0.0-SNAPSHOT/app-1.0.0-SNAPSHOT.jar.sha1", true},
		{"com/example/app/1.0.0-SNAPSHOT/app-1.0.0-20240115.101500-1.jar", false},
		{"com/example/app/1.0.0/app-1.0.0.jar", false},
		{"com/example/app/1.0.0/app-1.0.0.pom", false},
		{"com/example/app/1.0.0/app-1.0.0-sources.jar", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			if got := (Layout{}).Mutable(testCase.path); got != testCase.want {
				t.Fatalf("Mutable(%q) = %v, want %v", testCase.path, got, testCase.want)
			}
		})
	}
}

func TestLayoutCoordinates(t *testing.T) {
	cases := []struct {
		path      string
		known     bool
		namespace string
		name      string
		version   string
	}{
		{"com/example/app/1.0.0/app-1.0.0.jar", true, "com.example", "app", "1.0.0"},
		{"com/example/app/1.0.0-SNAPSHOT/app-1.0.0-SNAPSHOT.jar", true, "com.example", "app", "1.0.0-SNAPSHOT"},
		{"com/example/app/maven-metadata.xml", false, "", "", ""},
		{"com/example/app/1.0.0/notes.txt", false, "", "", ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			coordinates, ok := (Layout{}).Coordinates(testCase.path)
			if ok != testCase.known {
				t.Fatalf("Coordinates(%q) ok = %v, want %v", testCase.path, ok, testCase.known)
			}
			if !ok {
				return
			}
			if coordinates.Namespace != testCase.namespace || coordinates.Name != testCase.name || coordinates.Version != testCase.version {
				t.Fatalf("Coordinates(%q) = %+v, want %s/%s/%s",
					testCase.path, coordinates, testCase.namespace, testCase.name, testCase.version)
			}
		})
	}
}

func TestLayoutComponentAt(t *testing.T) {
	cases := []struct {
		prefix    string
		known     bool
		namespace string
		name      string
	}{
		{"com/example/app/", true, "com.example", "app"},
		{"com/example/", true, "com", "example"},
		{"com/", false, "", ""},
		{"", false, "", ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.prefix, func(t *testing.T) {
			coordinates, ok := (Layout{}).ComponentAt(testCase.prefix)
			if ok != testCase.known {
				t.Fatalf("ComponentAt(%q) ok = %v, want %v", testCase.prefix, ok, testCase.known)
			}
			if !ok {
				return
			}
			if coordinates.Namespace != testCase.namespace || coordinates.Name != testCase.name {
				t.Fatalf("ComponentAt(%q) = %s/%s, want %s/%s",
					testCase.prefix, coordinates.Namespace, coordinates.Name, testCase.namespace, testCase.name)
			}
		})
	}
}

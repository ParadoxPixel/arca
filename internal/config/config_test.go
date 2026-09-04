package config

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		fail bool
	}{
		{"", "", false},
		{"https://artifacts.example.com", "https://artifacts.example.com", false},
		{"https://artifacts.example.com/", "https://artifacts.example.com", false},
		{"http://host:8080/prefix/?x=1#y", "http://host:8080/prefix", false},
		{"artifacts.example.com", "", true},
		{"ftp://artifacts.example.com", "", true},
		{"https://user:pass@artifacts.example.com", "", true},
	}

	for _, testCase := range cases {
		got, err := NormalizeBaseURL(testCase.raw)
		if (err != nil) != testCase.fail {
			t.Errorf("%q: err = %v, want failure %v", testCase.raw, err, testCase.fail)
			continue
		}
		if got != testCase.want {
			t.Errorf("%q: got %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}

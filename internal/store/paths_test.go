package store

import "testing"

func TestRangeForPrefixRoot(t *testing.T) {
	if _, ok := RangeForPrefix(""); ok {
		t.Fatal("expected the repository root to be unbounded")
	}
}

func TestRangeForPrefixUpperBound(t *testing.T) {
	cases := []struct {
		prefix string
		upper  string
	}{
		{"com/example/", "com/example0"},
		{"com/craftmend/openaudiomc/openaudiomc-api/6.10.16/", "com/craftmend/openaudiomc/openaudiomc-api/6.10.160"},
		{"app-1.0.jar", "app-1.0.jas"},
		{"a", "b"},
		{"com/café", "com/cafê"},
	}

	for _, testCase := range cases {
		t.Run(testCase.prefix, func(t *testing.T) {
			bounds, ok := RangeForPrefix(testCase.prefix)
			if !ok {
				t.Fatal("expected a bounded range")
			}
			if bounds.Lower != testCase.prefix || bounds.Upper != testCase.upper {
				t.Fatalf("got (%q, %q), want (%q, %q)", bounds.Lower, bounds.Upper, testCase.prefix, testCase.upper)
			}
		})
	}
}

func TestRangeForPrefixContainment(t *testing.T) {
	cases := []struct {
		prefix    string
		candidate string
		contained bool
	}{
		{"com/example/", "com/example/app/1.0/app-1.0.jar", true},
		{"com/example/", "com/example/", true},
		{"com/example/", "com/examples/app/1.0/app-1.0.jar", false},
		{"com/example/", "com/exampl/app/1.0/app-1.0.jar", false},
		{"com/example/", "org/example/app/1.0/app-1.0.jar", false},
		{"app-1.0.jar", "app-1.0.jar.sha1", true},
		{"app-1.0.jar", "app-1.0.jas", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.prefix+" contains "+testCase.candidate, func(t *testing.T) {
			bounds, ok := RangeForPrefix(testCase.prefix)
			if !ok {
				t.Fatal("expected a bounded range")
			}
			contained := testCase.candidate >= bounds.Lower && testCase.candidate < bounds.Upper
			if contained != testCase.contained {
				t.Fatalf("containment = %v, want %v", contained, testCase.contained)
			}
		})
	}
}

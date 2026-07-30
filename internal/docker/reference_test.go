package docker

import (
	"strings"
	"testing"
)

func TestIsValidName(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"nginx", true},
		{"erc/research-grant", true},
		{"a/b/c/d", true},
		{"my.image", true},
		{"my_image", true},
		{"my__image", true},
		{"my---image", true},
		{"a1/b2", true},

		{"", false},
		{"Nginx", false},
		{"my___image", false},
		{"my..image", false},
		{"my._image", false},
		{"-nginx", false},
		{"nginx-", false},
		{".nginx", false},
		{"_nginx", false},
		{"nginx/", false},
		{"/nginx", false},
		{"a//b", false},
		{"my image", false},
		{"my:image", false},
		{"my@image", false},
		{strings.Repeat("a", MaxNameLength+1), false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsValidName(testCase.name); got != testCase.valid {
				t.Fatalf("IsValidName(%q) = %v, want %v", testCase.name, got, testCase.valid)
			}
		})
	}
}

// The reserved directories must never validate as image names, because the
// storage layout relies on that alone to keep them apart from real images.
func TestReservedDirectoriesAreNotNames(t *testing.T) {
	for _, reserved := range []string{BlobsDirectory, ManifestsDirectory} {
		t.Run(reserved, func(t *testing.T) {
			if IsValidName(reserved) {
				t.Fatalf("IsValidName(%q) = true, want false", reserved)
			}
		})
	}
}

func TestIsValidTag(t *testing.T) {
	cases := []struct {
		tag   string
		valid bool
	}{
		{"latest", true},
		{"1.0.0", true},
		{"0.1.8-unittest-staging", true},
		{"1.25-alpine", true},
		{"RELEASE", true},
		{"_internal", true},
		{"v1.2.3_rc.1", true},

		{"", false},
		{".hidden", false},
		{"-leading", false},
		{"with/slash", false},
		{"with:colon", false},
		{"with space", false},
		{strings.Repeat("a", MaxTagLength+1), false},

		// A tag may begin with an underscore, so this is the one name that could
		// collide with the digest store beside it.
		{ManifestsDirectory, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.tag, func(t *testing.T) {
			if got := IsValidTag(testCase.tag); got != testCase.valid {
				t.Fatalf("IsValidTag(%q) = %v, want %v", testCase.tag, got, testCase.valid)
			}
		})
	}
}

func TestParseDigest(t *testing.T) {
	sha256Hex := strings.Repeat("ab", 32)
	sha512Hex := strings.Repeat("cd", 64)

	cases := []struct {
		raw       string
		algorithm string
		hex       string
		ok        bool
	}{
		{"sha256:" + sha256Hex, "sha256", sha256Hex, true},
		{"sha512:" + sha512Hex, "sha512", sha512Hex, true},

		{"", "", "", false},
		{sha256Hex, "", "", false},
		{"sha256:", "", "", false},
		{"sha256:" + sha256Hex[:63], "", "", false},
		{"sha256:" + sha256Hex + "a", "", "", false},
		{"sha256:" + strings.Repeat("AB", 32), "", "", false},
		{"sha256:" + strings.Repeat("zz", 32), "", "", false},
		{"md5:" + strings.Repeat("ab", 16), "", "", false},
		{"sha256:" + strings.Repeat("ab", 31) + "../", "", "", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.raw, func(t *testing.T) {
			digest, ok := ParseDigest(testCase.raw)
			if ok != testCase.ok {
				t.Fatalf("ParseDigest(%q) ok = %v, want %v", testCase.raw, ok, testCase.ok)
			}
			if !ok {
				return
			}
			if digest.Algorithm != testCase.algorithm || digest.Hex != testCase.hex {
				t.Fatalf("ParseDigest(%q) = %+v, want %s:%s", testCase.raw, digest, testCase.algorithm, testCase.hex)
			}
			if digest.String() != testCase.raw {
				t.Fatalf("String() = %q, want %q", digest.String(), testCase.raw)
			}
		})
	}
}

func TestDigestShort(t *testing.T) {
	cases := []struct {
		hex  string
		want string
	}{
		{strings.Repeat("a", 64), strings.Repeat("a", 12)},
		{"abc", "abc"},
		{strings.Repeat("b", 12), strings.Repeat("b", 12)},
	}

	for _, testCase := range cases {
		t.Run(testCase.hex, func(t *testing.T) {
			if got := SHA256(testCase.hex).Short(); got != testCase.want {
				t.Fatalf("Short() = %q, want %q", got, testCase.want)
			}
		})
	}
}

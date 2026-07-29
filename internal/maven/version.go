package maven

import (
	"sort"
	"strings"
)

var qualifierOrder = []string{"alpha", "beta", "milestone", "rc", "snapshot", "", "sp"}

var qualifierAliases = map[string]string{
	"a":       "alpha",
	"b":       "beta",
	"m":       "milestone",
	"cr":      "rc",
	"ga":      "",
	"final":   "",
	"release": "",
}

func normalizeQualifier(token string) string {
	if alias, ok := qualifierAliases[token]; ok {
		return alias
	}
	return token
}

func qualifierRank(token string) int {
	for index, candidate := range qualifierOrder {
		if candidate == token {
			return index
		}
	}
	return len(qualifierOrder)
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' }

func isNumeric(token string) bool {
	if token == "" {
		return false
	}
	for index := 0; index < len(token); index++ {
		if !isDigit(token[index]) {
			return false
		}
	}
	return true
}

func TokenizeVersion(version string) []string {
	lowered := strings.ToLower(version)
	lowered = strings.NewReplacer("-", ".", "_", ".").Replace(lowered)

	var separated strings.Builder
	for index := 0; index < len(lowered); index++ {
		current := lowered[index]
		if index > 0 {
			previous := lowered[index-1]
			if (isDigit(previous) && isLetter(current)) || (isLetter(previous) && isDigit(current)) {
				separated.WriteByte('.')
			}
		}
		separated.WriteByte(current)
	}

	tokens := make([]string, 0, 8)
	for _, token := range strings.Split(separated.String(), ".") {
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func compareNumeric(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if len(left) != len(right) {
		return len(left) - len(right)
	}
	return strings.Compare(left, right)
}

func compareTokens(left, right string, leftPresent, rightPresent bool) int {
	if !leftPresent {
		if rightPresent && isNumeric(right) {
			left = "0"
		} else {
			left = ""
		}
	}
	if !rightPresent {
		if isNumeric(left) {
			right = "0"
		} else {
			right = ""
		}
	}

	leftNumeric := isNumeric(left)
	rightNumeric := isNumeric(right)

	switch {
	case leftNumeric && rightNumeric:
		return compareNumeric(left, right)
	case leftNumeric:
		return 1
	case rightNumeric:
		return -1
	}

	leftQualifier := normalizeQualifier(left)
	rightQualifier := normalizeQualifier(right)
	if rank := qualifierRank(leftQualifier) - qualifierRank(rightQualifier); rank != 0 {
		return rank
	}
	return strings.Compare(leftQualifier, rightQualifier)
}

func CompareVersions(a, b string) int {
	left := TokenizeVersion(a)
	right := TokenizeVersion(b)

	length := max(len(left), len(right))
	for index := range length {
		leftToken, leftPresent := "", index < len(left)
		if leftPresent {
			leftToken = left[index]
		}
		rightToken, rightPresent := "", index < len(right)
		if rightPresent {
			rightToken = right[index]
		}
		if result := compareTokens(leftToken, rightToken, leftPresent, rightPresent); result != 0 {
			return result
		}
	}
	return 0
}

func SortVersions(versions []string) []string {
	sorted := append([]string(nil), versions...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return CompareVersions(sorted[i], sorted[j]) < 0
	})
	return sorted
}

func LatestAndRelease(versions []string) (latest string, release string) {
	sorted := SortVersions(versions)
	if len(sorted) > 0 {
		latest = sorted[len(sorted)-1]
	}
	for _, version := range sorted {
		if !strings.HasSuffix(version, SnapshotSuffix) {
			release = version
		}
	}
	return latest, release
}

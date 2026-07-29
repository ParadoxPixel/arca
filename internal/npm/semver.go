package npm

import (
	"strconv"
	"strings"
)

type version struct {
	core       []int64
	prerelease []string
}

func isNumeric(token string) bool {
	if token == "" {
		return false
	}
	for index := 0; index < len(token); index++ {
		if token[index] < '0' || token[index] > '9' {
			return false
		}
	}
	return true
}

func parse(raw string) version {
	trimmed := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if index := strings.IndexByte(trimmed, '+'); index >= 0 {
		trimmed = trimmed[:index]
	}

	var prerelease []string
	if index := strings.IndexByte(trimmed, '-'); index >= 0 {
		prerelease = strings.Split(trimmed[index+1:], ".")
		trimmed = trimmed[:index]
	}

	parsed := version{prerelease: prerelease}
	for _, token := range strings.Split(trimmed, ".") {
		number, err := strconv.ParseInt(token, 10, 64)
		if err != nil {
			number = 0
		}
		parsed.core = append(parsed.core, number)
	}
	return parsed
}

func comparePrerelease(left, right []string) int {
	switch {
	case len(left) == 0 && len(right) == 0:
		return 0
	// A version without a prerelease outranks the same version with one.
	case len(left) == 0:
		return 1
	case len(right) == 0:
		return -1
	}

	for index := 0; index < len(left) && index < len(right); index++ {
		leftToken, rightToken := left[index], right[index]
		leftNumeric, rightNumeric := isNumeric(leftToken), isNumeric(rightToken)

		switch {
		case leftNumeric && rightNumeric:
			leftNumber, _ := strconv.ParseInt(leftToken, 10, 64)
			rightNumber, _ := strconv.ParseInt(rightToken, 10, 64)
			if leftNumber != rightNumber {
				return int(leftNumber - rightNumber)
			}
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		default:
			if result := strings.Compare(leftToken, rightToken); result != 0 {
				return result
			}
		}
	}

	return len(left) - len(right)
}

func Compare(a, b string) int {
	left, right := parse(a), parse(b)

	for index := 0; index < len(left.core) || index < len(right.core); index++ {
		var leftPart, rightPart int64
		if index < len(left.core) {
			leftPart = left.core[index]
		}
		if index < len(right.core) {
			rightPart = right.core[index]
		}
		if leftPart != rightPart {
			if leftPart < rightPart {
				return -1
			}
			return 1
		}
	}

	return comparePrerelease(left.prerelease, right.prerelease)
}

func IsPrerelease(raw string) bool { return len(parse(raw).prerelease) > 0 }

func Core(raw string) string {
	parsed := parse(raw)

	parts := make([]string, 0, len(parsed.core))
	for _, part := range parsed.core {
		parts = append(parts, strconv.FormatInt(part, 10))
	}
	return strings.Join(parts, ".")
}

func IsValidVersion(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed != raw || len(raw) > 200 {
		return false
	}

	body := trimmed
	if index := strings.IndexByte(body, '+'); index >= 0 {
		body = body[:index]
	}
	if index := strings.IndexByte(body, '-'); index >= 0 {
		body = body[:index]
	}

	parts := strings.Split(body, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if !isNumeric(part) {
			return false
		}
	}
	return true
}

package store

type PathRange struct {
	Lower string
	Upper string
}

func RangeForPrefix(prefix string) (PathRange, bool) {
	if prefix == "" {
		return PathRange{}, false
	}

	runes := []rune(prefix)
	last := runes[len(runes)-1]

	return PathRange{Lower: prefix, Upper: string(runes[:len(runes)-1]) + string(last+1)}, true
}

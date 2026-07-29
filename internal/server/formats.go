package server

import (
	"arca/internal/format"
	"arca/internal/maven"
	"arca/internal/npm"
	"arca/internal/store/models"
)

func layoutFor(repository *models.Repository) format.Layout {
	return layoutForFormat(repository.Format)
}

// layoutForFormat serves the cross-repository listings, whose records carry a
// format but not the repository they came from.
func layoutForFormat(name string) format.Layout {
	switch name {
	case format.NPM:
		return npm.Layout{}
	default:
		return maven.Layout{}
	}
}

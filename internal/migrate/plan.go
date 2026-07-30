// Package migrate reads a Nexus instance and reproduces its repositories in
// arca. It never writes to Nexus.
package migrate

import (
	"fmt"
	"sort"
	"strings"

	"arca/internal/format"
	"arca/internal/nexus"
	"arca/internal/store"
	"arca/internal/store/models"
)

const (
	ActionCreate = "create"
	ActionSkip   = "skip"
)

// Decision is what the migration intends to do with one Nexus repository.
type Decision struct {
	Source nexus.Repository
	// Target is the arca repository name, which is the Nexus name lowercased
	// because arca names are.
	Target     string
	Action     string
	Format     string
	Type       string
	Policy     string
	RemoteURL  string
	CopyAssets bool
	Reason     string
}

// TranslatePath maps a source path onto the layout this server stores. Most formats
// store what Nexus stored, so the default is the path unchanged; docker addresses its
// content entirely differently at both ends.
func (d Decision) TranslatePath(path string) (string, bool) {
	if d.Format == format.Docker {
		return DockerPath(path)
	}
	return path, true
}

// CopyComponents reports whether the source's component listing has to be walked as
// well as its assets. Only docker needs it, and it needs it badly: the asset list names
// no tags at all, so assets alone copy every byte of every image and leave none of them
// pullable.
func (d Decision) CopyComponents() bool {
	return d.CopyAssets && d.Format == format.Docker
}

func (d Decision) Name() string       { return d.Target }
func (d Decision) SourceName() string { return d.Source.Name }

type Options struct {
	// RawFormat is what a Nexus raw repository becomes. Their contents are
	// opaque to Nexus, and in practice they hold p2 update sites, but a raw
	// repository holding something else can be pointed elsewhere.
	RawFormat string
	// Visibility is applied to every created repository.
	Visibility string
	// Only limits the migration to these repository names when non-empty.
	Only []string
}

func (o Options) rawFormat() string {
	if o.RawFormat == "" {
		return format.P2
	}
	return o.RawFormat
}

func (o Options) visibility() string {
	if o.Visibility == "" {
		return models.VisibilityPrivate
	}
	return o.Visibility
}

// Plan decides what to do with every repository, in a stable order, without
// contacting anything. Skips carry a reason so the report explains itself.
func Plan(repositories []nexus.Repository, options Options) []Decision {
	selected := make([]nexus.Repository, 0, len(repositories))
	for _, repository := range repositories {
		if len(options.Only) == 0 || contains(options.Only, repository.Name) {
			selected = append(selected, repository)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })

	decisions := make([]Decision, 0, len(selected))
	for _, repository := range selected {
		decisions = append(decisions, decide(repository, options))
	}
	return decisions
}

func decide(repository nexus.Repository, options Options) Decision {
	decision := Decision{
		Source: repository,
		Target: strings.ToLower(strings.TrimSpace(repository.Name)),
		Action: ActionSkip,
	}

	if !store.IsValidRepositoryName(decision.Target) {
		decision.Reason = "the name is not valid for an arca repository, so it needs creating by hand"
		return decision
	}

	// Membership is not migrated: arca groups exist only for p2, and which
	// members a group should carry is a decision worth making by hand.
	if repository.IsGroup() {
		decision.Reason = groupReason(repository)
		return decision
	}

	target, ok := formatFor(repository.Format, options)
	if !ok {
		decision.Reason = fmt.Sprintf("arca has no %s format", repository.Format)
		return decision
	}

	decision.Action = ActionCreate
	decision.Format = target
	decision.Policy = policyFor(repository)

	if repository.IsProxy() {
		decision.Type = models.TypeProxy
		decision.RemoteURL = repository.RemoteURL()
		if decision.RemoteURL == "" {
			decision.Action = ActionSkip
			decision.Reason = "the proxy has no remote URL to point at"
			decision.Format, decision.Type, decision.RemoteURL = "", "", ""
			return decision
		}
		// A proxy holds nothing that cannot be fetched again, so its cache is
		// left behind and refills itself on first use.
		decision.CopyAssets = false
		return decision
	}

	decision.Type = models.TypeHosted
	decision.CopyAssets = true
	return decision
}

// groupReason explains a skipped group. The member list is often unavailable: the
// repository endpoint returns empty attributes on some Nexus versions, so claiming a
// group has no members would be worse than saying they could not be read.
func groupReason(repository nexus.Repository) string {
	members := repository.Members()
	if len(members) == 0 {
		return "groups are not migrated, and this Nexus did not report which repositories this one " +
			"contains; recreate it by hand, or point clients at the members directly"
	}
	return fmt.Sprintf(
		"groups are not migrated; recreate it as a p2 group, or point clients at the members directly (%s)",
		strings.Join(members, ", "),
	)
}

func formatFor(nexusFormat string, options Options) (string, bool) {
	switch nexusFormat {
	case "maven2":
		return format.Maven2, true
	case "npm":
		return format.NPM, true
	case "docker":
		return format.Docker, true
	case "raw":
		return options.rawFormat(), true
	default:
		return "", false
	}
}

// policyFor carries a Maven repository's version policy across. Nexus has no
// equivalent for the other formats, which take arca's default.
//
// Docker is the exception: its tags carry arbitrary suffixes, and real ones like
// 0.1.9-swaggerui-staging are neither a release nor a prerelease by any rule worth
// writing, so a mixed policy is the only honest choice.
func policyFor(repository nexus.Repository) string {
	if repository.Format == "docker" {
		return models.PolicyMixed
	}

	switch strings.ToUpper(repository.VersionPolicy()) {
	case "SNAPSHOT":
		return models.PolicySnapshot
	case "MIXED":
		return models.PolicyMixed
	case "RELEASE":
		return models.PolicyRelease
	default:
		return ""
	}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

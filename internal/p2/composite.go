package p2

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

const (
	CompositeContentFilename   = "compositeContent.xml"
	CompositeArtifactsFilename = "compositeArtifacts.xml"

	metadataInstruction = "compositeMetadataRepository"
	artifactInstruction = "compositeArtifactRepository"

	metadataType = "org.eclipse.equinox.internal.p2.metadata.repository.CompositeMetadataRepository"
	artifactType = "org.eclipse.equinox.internal.p2.artifact.repository.CompositeArtifactRepository"
)

// Composite is a repository that has no content of its own, only children. p2
// merges them client side, which is why a group of update sites needs no
// metadata merging here.
type Composite struct {
	Name      string
	Timestamp int64
	// Children are locations relative to the composite's own URL.
	Children []string
}

func RenderCompositeContent(composite Composite) string {
	return render(composite, metadataInstruction, metadataType)
}

func RenderCompositeArtifacts(composite Composite) string {
	return render(composite, artifactInstruction, artifactType)
}

// RenderIndex names the factories a composite is read through, so a client asks
// for the composite documents rather than probing for content that is not here.
func RenderIndex() string {
	return "version=1\n" +
		"metadata.repository.factory.order=" + CompositeContentFilename + "\n" +
		"artifact.repository.factory.order=" + CompositeArtifactsFilename + "\n"
}

func render(composite Composite, instruction, repositoryType string) string {
	var out strings.Builder

	fmt.Fprintf(&out, "<?xml version='1.0' encoding='UTF-8'?>\n")
	fmt.Fprintf(&out, "<?%s version='1.0.0'?>\n", instruction)
	fmt.Fprintf(&out, "<repository name='%s' type='%s' version='1.0.0'>\n",
		escapeAttribute(composite.Name), repositoryType)

	// Atomic loading makes a child that cannot be read fail the whole
	// composite rather than silently serve less than it claims to.
	fmt.Fprintf(&out, "  <properties size='2'>\n")
	fmt.Fprintf(&out, "    <property name='p2.timestamp' value='%d'/>\n", composite.Timestamp)
	fmt.Fprintf(&out, "    <property name='p2.atomic.composite.loading' value='true'/>\n")
	fmt.Fprintf(&out, "  </properties>\n")

	fmt.Fprintf(&out, "  <children size='%d'>\n", len(composite.Children))
	for _, child := range composite.Children {
		fmt.Fprintf(&out, "    <child location='%s'/>\n", escapeAttribute(child))
	}
	fmt.Fprintf(&out, "  </children>\n")
	fmt.Fprintf(&out, "</repository>\n")

	return out.String()
}

// ChildLocation addresses a sibling repository of this server from inside a
// group. p2 resolves it against the composite's own URL, so stepping up one
// level lands on the repository mount both share.
func ChildLocation(target string) string {
	return "../" + strings.Trim(target, "/") + "/"
}

func escapeAttribute(value string) string {
	var escaped bytes.Buffer
	xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}

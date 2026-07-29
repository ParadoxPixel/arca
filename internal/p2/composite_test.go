package p2

import (
	"strings"
	"testing"
)

func TestRenderComposite(t *testing.T) {
	composite := Composite{
		Name:      "Be Informed AMS",
		Timestamp: 1732778582020,
		Children:  []string{ChildLocation("eclipse/releases/2024-12"), ChildLocation("p2-releases")},
	}

	cases := []struct {
		name        string
		document    string
		instruction string
		kind        string
	}{
		{
			name:        "content",
			document:    RenderCompositeContent(composite),
			instruction: "<?compositeMetadataRepository version='1.0.0'?>",
			kind:        "org.eclipse.equinox.internal.p2.metadata.repository.CompositeMetadataRepository",
		},
		{
			name:        "artifacts",
			document:    RenderCompositeArtifacts(composite),
			instruction: "<?compositeArtifactRepository version='1.0.0'?>",
			kind:        "org.eclipse.equinox.internal.p2.artifact.repository.CompositeArtifactRepository",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for _, expected := range []string{
				test.instruction,
				"type='" + test.kind + "'",
				"name='Be Informed AMS'",
				"<property name='p2.timestamp' value='1732778582020'/>",
				"<property name='p2.atomic.composite.loading' value='true'/>",
				"<children size='2'>",
				"<child location='../eclipse/releases/2024-12/'/>",
				"<child location='../p2-releases/'/>",
			} {
				if !strings.Contains(test.document, expected) {
					t.Errorf("missing %q in:\n%s", expected, test.document)
				}
			}
		})
	}
}

func TestChildLocationStepsUpToTheRepositoryMount(t *testing.T) {
	cases := []struct {
		target string
		want   string
	}{
		{"p2-releases", "../p2-releases/"},
		{"eclipse/releases/2024-12", "../eclipse/releases/2024-12/"},
		{"/p2-releases/", "../p2-releases/"},
		{"a/b/c/d", "../a/b/c/d/"},
	}

	for _, test := range cases {
		t.Run(test.target, func(t *testing.T) {
			if got := ChildLocation(test.target); got != test.want {
				t.Fatalf("ChildLocation(%q) = %q, want %q", test.target, got, test.want)
			}
		})
	}
}

func TestCompositeEscapesNames(t *testing.T) {
	document := RenderCompositeContent(Composite{
		Name:     `Ben & Jerry's <sites>`,
		Children: []string{ChildLocation("a&b")},
	})

	if strings.Contains(document, "name='Ben & Jerry's <sites>'") {
		t.Fatalf("the name was not escaped:\n%s", document)
	}
	for _, expected := range []string{"&amp;", "&#39;", "&lt;"} {
		if !strings.Contains(document, expected) {
			t.Errorf("missing escape %q in:\n%s", expected, document)
		}
	}
}

func TestRenderIndexNamesTheCompositeFactories(t *testing.T) {
	index := RenderIndex()

	for _, expected := range []string{
		"metadata.repository.factory.order=compositeContent.xml",
		"artifact.repository.factory.order=compositeArtifacts.xml",
	} {
		if !strings.Contains(index, expected) {
			t.Errorf("missing %q in:\n%s", expected, index)
		}
	}
}

func TestEmptyCompositeIsStillWellFormed(t *testing.T) {
	document := RenderCompositeContent(Composite{Name: "empty"})

	if !strings.Contains(document, "<children size='0'>") {
		t.Fatalf("an empty composite is malformed:\n%s", document)
	}
}

package build

import (
	"testing"

	"github.com/supermodular/atlas/internal/model"
)

func TestDetectPackageNameCollision(t *testing.T) {
	got := DetectCollisions([]model.Package{
		{Name: "dup", Source: "a", Primitives: []model.Primitive{}},
		{Name: "dup", Source: "b", Primitives: []model.Primitive{}},
		{Name: "solo", Source: "a", Primitives: []model.Primitive{}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d collisions, want 1: %+v", len(got), got)
	}
	if got[0].Kind != "package-name" || got[0].Name != "dup" {
		t.Errorf("collision = %+v", got[0])
	}
	if len(got[0].Sources) != 2 {
		t.Errorf("Sources = %v, want two", got[0].Sources)
	}
}

func TestDetectPrimitiveNameCollisionAcrossPackages(t *testing.T) {
	got := DetectCollisions([]model.Package{
		{Name: "p1", Source: "a", Primitives: []model.Primitive{{Type: "skill", Name: "shared"}}},
		{Name: "p2", Source: "a", Primitives: []model.Primitive{{Type: "skill", Name: "shared"}}},
	})
	if len(got) != 1 || got[0].Kind != "primitive-name" {
		t.Fatalf("got %+v, want one primitive-name collision", got)
	}
}

func TestNoCollisionWithinOnePackage(t *testing.T) {
	// The same name at two types in one package is not a clash a consumer hits.
	got := DetectCollisions([]model.Package{
		{Name: "p", Source: "a", Primitives: []model.Primitive{
			{Type: "skill", Name: "x"},
			{Type: "hook", Name: "x"},
		}},
	})
	if len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}

func TestCollisionsIgnoreWithheldPackages(t *testing.T) {
	// A withheld package has no primitives to clash, and its name appearing
	// twice is still a real package-name clash — but nil primitives must not
	// panic or invent a primitive collision.
	got := DetectCollisions([]model.Package{
		{Name: "p", Source: "a", Access: model.AccessExcluded, Primitives: nil},
		{Name: "q", Source: "b", Access: model.AccessRestricted, Primitives: nil},
	})
	if len(got) != 0 {
		t.Fatalf("got %+v, want none", got)
	}
}

package semantic

import (
	"testing"
)

func TestEffectiveObjectTypes_emptyMeansAll(t *testing.T) {
	got := EffectiveObjectTypes(nil)
	if len(got) != 3 {
		t.Fatalf("want 3 defaults, got %v", got)
	}
	got2 := EffectiveObjectTypes([]string{})
	if len(got2) != 3 {
		t.Fatalf("want 3 defaults for empty slice, got %v", got2)
	}
}

func TestEffectiveObjectTypes_explicitSubset(t *testing.T) {
	got := EffectiveObjectTypes([]string{"memory"})
	if len(got) != 1 || got[0] != "memory" {
		t.Fatalf("want [memory], got %v", got)
	}
}

func TestEffectiveObjectTypes_unknownOnlyFallsBackToAll(t *testing.T) {
	got := EffectiveObjectTypes([]string{"unknown", "also_bad"})
	if len(got) != 3 {
		t.Fatalf("want fallback to all types, got %v", got)
	}
}

func TestFilterObjectIDsByTypes_fullAllowNoAllocPath(t *testing.T) {
	ids := []string{"mem_a", "state_b", "art_c"}
	allowed := []string{"memory", "state", "artifact"}
	out := FilterObjectIDsByTypes(ids, allowed)
	if &out[0] != &ids[0] {
		t.Fatal("expected same backing array when filter is full triple")
	}
}

func TestFilterObjectIDsByTypes_memoryOnly(t *testing.T) {
	ids := []string{"mem_1", "state_1", "art_1", "opaque_x"}
	out := FilterObjectIDsByTypes(ids, []string{"memory"})
	if len(out) != 2 {
		t.Fatalf("want mem + opaque, got %v", out)
	}
	if out[0] != "mem_1" || out[1] != "opaque_x" {
		t.Fatalf("unexpected order/content: %v", out)
	}
}

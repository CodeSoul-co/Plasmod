package schemas

import "testing"

func TestNormalizeWarmIndexType(t *testing.T) {
	got, err := NormalizeWarmIndexType("")
	if err != nil || got != WarmIndexHNSW {
		t.Fatalf("empty: got %q err %v", got, err)
	}
	got, err = NormalizeWarmIndexType("ivf_flat")
	if err != nil || got != WarmIndexIVFFlat {
		t.Fatalf("ivf_flat: got %q err %v", got, err)
	}
	_, err = NormalizeWarmIndexType("FOO")
	if err == nil {
		t.Fatal("expected error for unknown index_type")
	}
}

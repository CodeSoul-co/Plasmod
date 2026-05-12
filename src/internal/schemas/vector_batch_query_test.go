package schemas

import (
	"testing"
)

func TestResolveWarmVectorBatchLineage_singleSynthetic(t *testing.T) {
	sources, lineage, err := ResolveWarmVectorBatchLineage(AgentModeSingleAgent, 3, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 || sources[0] != "c_0" || sources[2] != "c_2" {
		t.Fatalf("sources=%v", sources)
	}
	if len(lineage) != 3 || len(lineage[1]) != 1 || lineage[1][0] != 1 {
		t.Fatalf("lineage=%v", lineage)
	}
}

func TestResolveWarmVectorBatchLineage_identity(t *testing.T) {
	src := []string{"a", "b"}
	sources, lineage, err := ResolveWarmVectorBatchLineage(AgentModeMultiAgent, 2, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || lineage[0][0] != 0 || lineage[1][0] != 1 {
		t.Fatalf("sources=%v lineage=%v", sources, lineage)
	}
}

func TestResolveWarmVectorBatchLineage_multiMerge(t *testing.T) {
	src := []string{"q1", "q2", "q3"}
	lin := [][]int{{0, 2}, {1}}
	sources, lineage, err := ResolveWarmVectorBatchLineage(AgentModeMultiAgent, 2, src, lin)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 {
		t.Fatal(sources)
	}
	if len(lineage) != 2 || lineage[0][0] != 0 || lineage[0][1] != 2 {
		t.Fatalf("lineage=%v", lineage)
	}
}

func TestMergeWarmBatchLineage_sharedRow(t *testing.T) {
	rowHits := [][]string{
		{"x", "y"},
		{"y", "z"},
	}
	lineage := [][]int{{0, 2}, {1, 2}}
	merged := MergeWarmBatchLineage(rowHits, lineage, 3)
	if len(merged[0]) != 2 || merged[0][0] != "x" || merged[0][1] != "y" {
		t.Fatalf("source0=%v", merged[0])
	}
	if len(merged[1]) != 2 || merged[1][0] != "y" || merged[1][1] != "z" {
		t.Fatalf("source1=%v want [y z]", merged[1])
	}
	// source2 gets x,y from row0 and y,z from row1 -> x,y,z order
	if len(merged[2]) != 3 {
		t.Fatalf("source2=%v want 3 unique in order", merged[2])
	}
}

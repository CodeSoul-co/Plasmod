package schemas

import "testing"

func TestMemoryDatasetMatch_structuredExact(t *testing.T) {
	m := Memory{
		Scope:          "w1",
		DatasetName:    "deep1B",
		SourceFileName: "base.fbin",
		Content:        "ignored",
	}
	if !MemoryDatasetMatch(m, "w1", "base.fbin", "deep1B", "") {
		t.Fatal("structured file_name + dataset_name should match")
	}
	if MemoryDatasetMatch(m, "w1", "other.fbin", "deep1B", "") {
		t.Fatal("wrong file_name should not match")
	}
	if MemoryDatasetMatch(m, "w2", "base.fbin", "deep1B", "") {
		t.Fatal("wrong workspace should not match")
	}
}

func TestMemoryDatasetMatch_contentFallback_datasetNameBoundary(t *testing.T) {
	m := Memory{
		Scope:   "w1",
		Content: "dataset=a.bin dataset_name:foobar row:1 dim:10",
	}
	if MemoryDatasetMatch(m, "w1", "", "foo", "") {
		t.Fatal("dataset_name:foo must not match inside dataset_name:foobar")
	}
	if !MemoryDatasetMatch(m, "w1", "", "foobar", "") {
		t.Fatal("dataset_name:foobar should match")
	}
}

func TestMemoryDatasetMatch_contentFallback_fileToken(t *testing.T) {
	m := Memory{
		Scope:   "w1",
		Content: "dataset=base.10M.fbin dataset_name:deep1B row:0",
	}
	if !MemoryDatasetMatch(m, "w1", "base.10M.fbin", "", "") {
		t.Fatal("exact file token should match")
	}
	if MemoryDatasetMatch(m, "w1", "base.10M", "", "") {
		t.Fatal("must not match shorter token as exact file name")
	}
}

func TestMemoryDatasetMatch_prefixOnStructuredFile(t *testing.T) {
	m := Memory{
		Scope:          "w1",
		SourceFileName: "base.10M.fbin",
		Content:        "x",
	}
	if !MemoryDatasetMatch(m, "w1", "", "", "base.") {
		t.Fatal("prefix on SourceFileName")
	}
	if MemoryDatasetMatch(m, "w1", "", "", "nomatch") {
		t.Fatal("bad prefix")
	}
}

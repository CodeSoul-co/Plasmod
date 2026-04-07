package schemas

import "testing"

// helpers
func memWith(scope, fileName, datasetName, content string) Memory {
	return Memory{
		Scope:          scope,
		SourceFileName: fileName,
		DatasetName:    datasetName,
		Content:        content,
	}
}

func TestMemoryDatasetMatch_EmptyWorkspaceAlwaysFalse(t *testing.T) {
	m := memWith("ws1", "base.fbin", "deep1B", "")
	if MemoryDatasetMatch(m, "", "base.fbin", "deep1B", "") {
		t.Error("empty workspaceID should always return false")
	}
}

func TestMemoryDatasetMatch_WrongWorkspaceFalse(t *testing.T) {
	m := memWith("ws1", "base.fbin", "deep1B", "")
	if MemoryDatasetMatch(m, "ws2", "base.fbin", "deep1B", "") {
		t.Error("wrong workspace should return false")
	}
}

func TestMemoryDatasetMatch_AllSelectorsEmpty_MatchesAnyInWorkspace(t *testing.T) {
	m := memWith("ws1", "anything.fbin", "anydataset", "some content")
	if !MemoryDatasetMatch(m, "ws1", "", "", "") {
		t.Error("all selectors empty should match any memory in workspace")
	}
}

func TestMemoryDatasetMatch_ANDSemantics_BothMustMatch(t *testing.T) {
	m := memWith("ws1", "base.fbin", "deep1B", "")
	// fileName matches but datasetName does not
	if MemoryDatasetMatch(m, "ws1", "base.fbin", "other", "") {
		t.Error("AND semantics: both selectors must match")
	}
	// datasetName matches but fileName does not
	if MemoryDatasetMatch(m, "ws1", "other.fbin", "deep1B", "") {
		t.Error("AND semantics: both selectors must match")
	}
	// both match
	if !MemoryDatasetMatch(m, "ws1", "base.fbin", "deep1B", "") {
		t.Error("AND semantics: both matching should return true")
	}
}

func TestMemoryDatasetMatch_StructuredFileNameExact(t *testing.T) {
	m := memWith("ws1", "base.10M.fbin", "", "")
	if !MemoryDatasetMatch(m, "ws1", "base.10M.fbin", "", "") {
		t.Error("exact file name match should return true")
	}
	if MemoryDatasetMatch(m, "ws1", "base.10M", "", "") {
		t.Error("partial file name should not match structured SourceFileName")
	}
}

func TestMemoryDatasetMatch_StructuredDatasetNameExact(t *testing.T) {
	m := memWith("ws1", "", "deep1B", "")
	if !MemoryDatasetMatch(m, "ws1", "", "deep1B", "") {
		t.Error("exact dataset name should match")
	}
	if MemoryDatasetMatch(m, "ws1", "", "deep", "") {
		t.Error("prefix of dataset name should not match structured DatasetName")
	}
}

func TestMemoryDatasetMatch_PrefixOnStructuredFileName(t *testing.T) {
	m := memWith("ws1", "base.10M.fbin", "", "")
	if !MemoryDatasetMatch(m, "ws1", "", "", "base") {
		t.Error("prefix 'base' should match SourceFileName 'base.10M.fbin'")
	}
	if MemoryDatasetMatch(m, "ws1", "", "", "query") {
		t.Error("non-matching prefix should return false")
	}
}

func TestMemoryDatasetMatch_ContentFallback_FileToken(t *testing.T) {
	m := memWith("ws1", "", "", "dataset=base.fbin dataset_name:deep1B row:0")
	if !MemoryDatasetMatch(m, "ws1", "base.fbin", "", "") {
		t.Error("content fallback: file token match")
	}
	if MemoryDatasetMatch(m, "ws1", "base", "", "") {
		t.Error("content fallback: file token must be exact, not prefix")
	}
}

func TestMemoryDatasetMatch_ContentFallback_DatasetNameBoundary(t *testing.T) {
	m := memWith("ws1", "", "", "dataset=x.fbin dataset_name:deep1B row:0")
	// "deep1B" matches (token boundary is "row:")
	if !MemoryDatasetMatch(m, "ws1", "", "deep1B", "") {
		t.Error("content fallback: dataset_name token boundary via 'row:'")
	}
	// "deep1" should NOT match (would be a prefix of deep1B without boundary)
	if MemoryDatasetMatch(m, "ws1", "", "deep1", "") {
		t.Error("content fallback: dataset_name prefix without boundary should not match")
	}
}

func TestMemoryDatasetMatch_ContentFallback_PrefixMatch(t *testing.T) {
	m := memWith("ws1", "", "", "dataset=base.10M.fbin info")
	if !MemoryDatasetMatch(m, "ws1", "", "", "base.10") {
		t.Error("content fallback: prefix should match file token")
	}
	if MemoryDatasetMatch(m, "ws1", "", "", "query.") {
		t.Error("content fallback: non-matching prefix should return false")
	}
}

func TestMemoryDatasetMatch_ContentFallback_FileTokenSpaceTerminator(t *testing.T) {
	m := memWith("ws1", "", "", "dataset=myfile.fbin some other text")
	if !MemoryDatasetMatch(m, "ws1", "myfile.fbin", "", "") {
		t.Error("file token terminated by space should match exactly")
	}
}

func TestMemoryDatasetMatch_StructuredBeatsContentFallback(t *testing.T) {
	// SourceFileName is set, so content token is ignored for fileName matching
	m := memWith("ws1", "actual.fbin", "", "dataset=other.fbin")
	if !MemoryDatasetMatch(m, "ws1", "actual.fbin", "", "") {
		t.Error("structured SourceFileName should be used instead of content fallback")
	}
	if MemoryDatasetMatch(m, "ws1", "other.fbin", "", "") {
		t.Error("content token should be ignored when SourceFileName is set")
	}
}

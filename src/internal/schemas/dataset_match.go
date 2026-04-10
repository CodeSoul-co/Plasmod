package schemas

import "strings"

// MemoryDatasetMatch reports whether m matches admin dataset delete/purge selectors.
// workspaceID must equal m.Scope. Selectors use AND semantics: empty string means that constraint is disabled.
// If all selectors are empty, it matches every memory in the workspace.
//
// When Memory.DatasetName / Memory.SourceFileName are set (from ingest payload), matching uses exact
// string equality (and HasPrefix for prefix on SourceFileName). Otherwise matching falls back to
// Content with token-boundary rules to avoid substring false positives (e.g. dataset_name:foo vs foobar).
func MemoryDatasetMatch(m Memory, workspaceID string, fileName, datasetName, prefix string) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	fileName = strings.TrimSpace(fileName)
	datasetName = strings.TrimSpace(datasetName)
	prefix = strings.TrimSpace(prefix)

	if workspaceID == "" || m.Scope != workspaceID {
		return false
	}
	if fileName == "" && datasetName == "" && prefix == "" {
		return true
	}
	if fileName != "" {
		if m.SourceFileName != "" {
			if m.SourceFileName != fileName {
				return false
			}
		} else if !contentDatasetFileEquals(m.Content, fileName) {
			return false
		}
	}
	if datasetName != "" {
		if m.DatasetName != "" {
			if m.DatasetName != datasetName {
				return false
			}
		} else if !contentDatasetNameLabelEquals(m.Content, datasetName) {
			return false
		}
	}
	if prefix != "" {
		if m.SourceFileName != "" {
			if !strings.HasPrefix(m.SourceFileName, prefix) {
				return false
			}
		} else if !contentDatasetFileHasPrefix(m.Content, prefix) {
			return false
		}
	}
	return true
}

func extractDatasetFileToken(content string) (string, bool) {
	const marker = "dataset="
	i := strings.Index(content, marker)
	if i < 0 {
		return "", false
	}
	rest := content[i+len(marker):]
	end := len(rest)
	for j := 0; j < len(rest); j++ {
		if rest[j] == ' ' || rest[j] == '\t' || rest[j] == '\n' || rest[j] == '\r' {
			end = j
			break
		}
	}
	return rest[:end], true
}

func contentDatasetFileEquals(content, fileName string) bool {
	tok, ok := extractDatasetFileToken(content)
	if !ok {
		return false
	}
	return tok == fileName
}

func contentDatasetFileHasPrefix(content, prefix string) bool {
	tok, ok := extractDatasetFileToken(content)
	if !ok {
		return false
	}
	return strings.HasPrefix(tok, prefix)
}

func contentDatasetNameLabelEquals(content, name string) bool {
	needle := "dataset_name:" + name
	i := strings.Index(content, needle)
	if i < 0 {
		return false
	}
	after := i + len(needle)
	if after >= len(content) {
		return true
	}
	// Reject if "name" is only a prefix of a longer label (e.g. foo vs foobar).
	c := content[after]
	if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == ';' {
		return true
	}
	if strings.HasPrefix(content[after:], "row:") {
		return true
	}
	return false
}

//go:build extended
// +build extended

package metautil

import (
	"path"

	"andb/platformpkg/pkg/common"
)

func BuildSegmentIndexFilePath(rootPath string, buildID, indexVersion, partID, segID int64, fileKey string) string {
	k := JoinIDPath(buildID, indexVersion, partID, segID)
	return path.Join(rootPath, common.SegmentIndexPath, k, fileKey)
}

func BuildSegmentIndexFilePaths(rootPath string, buildID, indexVersion, partID, segID int64, fileKeys []string) []string {
	paths := make([]string, 0, len(fileKeys))
	for _, fileKey := range fileKeys {
		path := BuildSegmentIndexFilePath(rootPath, buildID, indexVersion, partID, segID, fileKey)
		paths = append(paths, path)
	}
	return paths
}

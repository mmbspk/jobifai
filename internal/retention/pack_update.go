package retention

import (
	"strings"

	"github.com/user/jobifai/internal/documents"
)

func clearExportPathInPack(packJSON, kind, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return packJSON
	}
	pack := documents.ParseApplicationPackJSON(packJSON)
	changed := false
	switch kind {
	case "resume":
		if strings.TrimSpace(pack.Resume.LocalPath) == path {
			pack.Resume.LocalPath = ""
			changed = true
		}
	case "cover":
		if strings.TrimSpace(pack.Cover.LocalPath) == path {
			pack.Cover.LocalPath = ""
			changed = true
		}
	}
	for i := range pack.Refs {
		if strings.TrimSpace(pack.Refs[i].LocalPath) == path {
			pack.Refs[i].LocalPath = ""
			changed = true
		}
	}
	if !changed {
		return packJSON
	}
	return documents.WriteApplicationPackJSON(pack)
}

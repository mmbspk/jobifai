package retention

import "context"

func protectedPaths(ctx context.Context, db DB, userID string, retainLatestN int) (map[string]struct{}, error) {
	idx, err := buildProtectionIndex(ctx, db, userID, retainLatestN)
	if err != nil {
		return nil, err
	}
	return idx.ExportPaths, nil
}

func pathProtected(protected map[string]struct{}, path string) bool {
	idx := ProtectionIndex{ExportPaths: protected}
	return idx.exportProtected(path)
}

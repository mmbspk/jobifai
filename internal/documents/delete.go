package documents

import "context"

// DeleteDocument removes a document from the user's saved library. Immutable
// versions remain addressable for application history and frozen review packs.
// Active lists (and any limit based on them) exclude these documents immediately.
func (s *Service) DeleteDocument(ctx context.Context, userID, documentID string) error {
	return s.withWork(ctx, userID, func() error {
		return s.Store.DeleteDocument(ctx, userID, documentID)
	})
}

func (s *Store) DeleteDocument(ctx context.Context, userID, documentID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Taking the write lock first and checking the owner in this UPDATE prevents
	// cross-user deletion and makes retries safe without resetting the timestamp.
	result, err := tx.ExecContext(ctx, `UPDATE user_documents SET deleted_at = COALESCE(deleted_at, datetime('now')) WHERE id = ? AND user_id = ?`, documentID, userID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM user_document_defaults WHERE user_id = ? AND content_version_id IN
		(SELECT id FROM document_content_versions WHERE document_id = ? AND user_id = ?)`, userID, documentID, userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

package retention

import (
	"context"
	"database/sql"
	"errors"

	"github.com/user/jobifai/internal/config"
	"github.com/user/jobifai/internal/domain"
)

const keyDocumentRetentionDefaults = "document_retention_defaults"

type ConfigStore interface {
	Get(userID, key string, dst any) error
	Set(userID, key string, src any) error
}

func LoadDefaults(store ConfigStore) domain.DocumentRetentionDefaults {
	var d domain.DocumentRetentionDefaults
	if store == nil {
		return domain.DocumentRetentionDefaults{LatestSubmittedApplications: domain.DocumentRetentionDefault}.Normalized()
	}
	if err := store.Get(domain.SystemUserID, keyDocumentRetentionDefaults, &d); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.DocumentRetentionDefaults{LatestSubmittedApplications: domain.DocumentRetentionDefault}.Normalized()
		}
		return domain.DocumentRetentionDefaults{LatestSubmittedApplications: domain.DocumentRetentionDefault}.Normalized()
	}
	return d.Normalized()
}

func SaveDefaults(store ConfigStore, d domain.DocumentRetentionDefaults) error {
	return store.Set(domain.SystemUserID, keyDocumentRetentionDefaults, d.Normalized())
}

// SaveDefaultsAudited validates and atomically persists policy + audit when limit changes.
func SaveDefaultsAudited(ctx context.Context, store ConfigStore, sqlDB *sql.DB, adminUserID string, incoming domain.DocumentRetentionDefaults) (domain.DocumentRetentionDefaults, error) {
	prev := LoadDefaults(store)
	if err := incoming.Validate(); err != nil {
		return prev, err
	}
	next := incoming.Normalized()
	if sqlDB == nil {
		return prev, errors.New("retention: database required for audited save")
	}
	cfgStore, ok := store.(*config.Store)
	if !ok {
		if prev.LatestSubmittedApplications != next.LatestSubmittedApplications {
			return prev, errors.New("retention: config store must support transactions")
		}
		return next, SaveDefaults(store, next)
	}
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return prev, err
	}
	defer func() { _ = tx.Rollback() }()
	if prev.LatestSubmittedApplications != next.LatestSubmittedApplications {
		if err := insertAuditTx(ctx, tx, adminUserID, prev.LatestSubmittedApplications, next.LatestSubmittedApplications); err != nil {
			return prev, err
		}
	}
	if err := cfgStore.SetTx(ctx, tx, domain.SystemUserID, keyDocumentRetentionDefaults, next); err != nil {
		return prev, err
	}
	if err := tx.Commit(); err != nil {
		return prev, err
	}
	return next, nil
}

func insertAuditTx(ctx context.Context, tx *sql.Tx, adminUserID string, prev, next int) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO document_retention_audit (id, admin_user_id, previous_limit, new_limit) VALUES (lower(hex(randomblob(16))), ?, ?, ?)`,
		adminUserID, prev, next)
	return err
}

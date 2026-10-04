package retention

import (
	"context"
	"errors"

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

// SaveDefaultsAudited validates, records audit when the limit changes, then persists policy.
func SaveDefaultsAudited(ctx context.Context, store ConfigStore, db DB, adminUserID string, incoming domain.DocumentRetentionDefaults) (domain.DocumentRetentionDefaults, error) {
	prev := LoadDefaults(store)
	if err := incoming.Validate(); err != nil {
		return prev, err
	}
	next := incoming.Normalized()
	if db != nil && prev.LatestSubmittedApplications != next.LatestSubmittedApplications {
		if err := insertAudit(ctx, db, adminUserID, prev.LatestSubmittedApplications, next.LatestSubmittedApplications); err != nil {
			return prev, err
		}
	}
	if err := SaveDefaults(store, next); err != nil {
		return prev, err
	}
	return next, nil
}

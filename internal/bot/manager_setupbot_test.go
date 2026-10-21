package bot

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/browser"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/documents"
	"github.com/user/jobifai/internal/domain"
)

// TestSetupBot_DocumentsServicePropagated ensures setupBot assigns the manager's
// document service to the bot config. Regresses the defect where setupBot omitted
// Documents: m.documents, causing PrepareReviewDocuments to always return
// "document policies are not enabled for this user" even when the service was wired.
func TestSetupBot_DocumentsServicePropagated(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key")
	sessions := browser.NewSessionStore(db, secrets)

	const userID = "setupbot-docs-test"
	require.NoError(t, cfgStore.Set(userID, "resume_profile", domain.ResumeProfile{
		Summary: "Regression test profile.",
		PersonalInformation: domain.PersonalInformation{
			Name:  "Test User",
			Email: "test@example.com",
		},
	}))

	raw, err := browser.MarshalCookies([]browser.Cookie{
		{Name: "li_at", Value: "stub-token", Domain: ".linkedin.com", Path: "/"},
	})
	require.NoError(t, err)
	require.NoError(t, sessions.Save(userID, string(domain.PlatformLinkedIn), string(domain.LoginMethodManual), raw))

	ctx := context.Background()
	mgr := NewManager(ctx, db, cfgStore, secrets, sessions, nil, nil, nil, nil, "")
	mgr.SetDocuments(&documents.Service{})

	b, _, err := mgr.setupBot(userID, domain.PlatformLinkedIn, "")
	require.NoError(t, err)
	require.NotNil(t, b.cfg.Documents,
		"setupBot must propagate m.documents to the bot config; "+
			"absence causes PrepareReviewDocuments to always fail with "+
			"\"document policies are not enabled for this user\"")
}

// TestSetupBot_NilDocumentsServiceProducesNilConfig verifies that when no document
// service is registered on the manager, the bot config also has nil Documents.
// This is the expected state before SetDocuments is called (not a wiring defect).
func TestSetupBot_NilDocumentsServiceProducesNilConfig(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := appdb.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cfgStore := config.NewStore(db)
	secrets := config.NewSecretsStore(db, "test-key")
	sessions := browser.NewSessionStore(db, secrets)

	const userID = "setupbot-nil-docs-test"
	require.NoError(t, cfgStore.Set(userID, "resume_profile", domain.ResumeProfile{
		Summary: "Regression test profile.",
		PersonalInformation: domain.PersonalInformation{
			Name:  "Test User",
			Email: "test@example.com",
		},
	}))

	raw, err := browser.MarshalCookies([]browser.Cookie{
		{Name: "li_at", Value: "stub-token", Domain: ".linkedin.com", Path: "/"},
	})
	require.NoError(t, err)
	require.NoError(t, sessions.Save(userID, string(domain.PlatformLinkedIn), string(domain.LoginMethodManual), raw))

	ctx := context.Background()
	mgr := NewManager(ctx, db, cfgStore, secrets, sessions, nil, nil, nil, nil, "")
	// No SetDocuments call — m.documents remains nil.

	b, _, err := mgr.setupBot(userID, domain.PlatformLinkedIn, "")
	require.NoError(t, err)
	require.Nil(t, b.cfg.Documents)
}

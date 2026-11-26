package bot

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/documents"
	"testing"
	"time"
)

func TestReadBrowserSetupFailureDoesNotChargeIncrement(t *testing.T) {
	for _, operation := range []string{"read", "submit", "prepare"} {
		t.Run(operation, func(t *testing.T) {
			database := pageHookDB(t)
			manager := pageHookManager(t, database)
			service := &pageQuotaMeter{}
			manager.SetBrowserMeter(service)
			var err error
			switch operation {
			case "read":
				_, err = manager.ReadJobPage(t.Context(), pageTestUser, "https://au.seek.com/job/22345678")
			case "submit":
				err = manager.runSubmit(t.Context(), pageTestUser, SubmitRequest{JobID: "failed-browser", Platform: "seek", Link: "https://au.seek.com/job/22345678", Role: "Engineer", Company: "Acme"})
			case "prepare":
				manager.SetDocuments(&documents.Service{Store: documents.NewStore(database)})
				_, insertErr := database.Exec("INSERT INTO jobs_pending_review(job_id,user_id,company,role,platform,link,created_at,prepare_status) VALUES('failed-browser',?,'Acme','Engineer','seek','https://au.seek.com/job/22345678',CURRENT_TIMESTAMP,'running:failed-prep')", pageTestUser)
				require.NoError(t, insertErr)
				err = manager.PrepareReviewDocuments(t.Context(), pageTestUser, "failed-browser", "failed-prep")
			}
			require.Error(t, err)
			require.ErrorContains(t, err, "saved Seek session")
			require.Equal(t, int32(1), service.before.Load(), "quota is checked before setup")
			require.Zero(t, service.records.Load(), "a failed setup must not prepay a bucket")
			var applied int
			require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM jobs_applied").Scan(&applied))
			require.Zero(t, applied)
		})
	}
}

func TestAlignReadRunMeterStopsPreviousActivation(t *testing.T) {
	service := &pageQuotaMeter{}
	old := NewLiveBrowserMeter(service, pageTestUser, "old-run", time.Minute, nil)
	require.NoError(t, old.Activate(t.Context()))
	oldContext := old.BillingCtx()
	bot := New(Config{UserID: pageTestUser, AutomationRunID: "new-run", BrowserMeter: old})
	manager := &Manager{browserMeterSvc: service}
	manager.alignReadRunMeter(bot)
	defer bot.cfg.BrowserMeter.Stop()
	require.ErrorIs(t, oldContext.Err(), context.Canceled)
	require.False(t, bot.cfg.BrowserMeter.WasActivated())
	require.Equal(t, int32(1), service.records.Load())
}

func TestLiveReadMeterPreflightChecksWithoutCharging(t *testing.T) {
	service := &pageQuotaMeter{}
	meter := NewLiveBrowserMeter(service, pageTestUser, "reader", time.Minute, nil)
	defer meter.Stop()
	require.NoError(t, preflightReadMeter(t.Context(), meter))
	require.Zero(t, service.records.Load())
	require.False(t, meter.WasActivated())
	ctx, err := activateReadMeter(t.Context(), meter, "job")
	require.NoError(t, err)
	require.NoError(t, ctx.Err())
	require.True(t, meter.WasActivated())
	require.Equal(t, int32(1), service.records.Load())
}

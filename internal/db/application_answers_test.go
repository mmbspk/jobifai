package db_test

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/user/jobifai/internal/config"
	appdb "github.com/user/jobifai/internal/db"
	"github.com/user/jobifai/internal/domain"
)

func TestApplicationAnswersMigrationPreservesFactsMappingsAndAttempts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers-upgrade.db")
	database, err := appdb.Open(path)
	require.NoError(t, err)
	require.NoError(t, goose.DownTo(database, "migrations", 60))
	_, err = database.Exec(`INSERT INTO users(id,email,password_hash) VALUES('candidate','migration@fixture.test','fixture')`)
	require.NoError(t, err)
	no := false
	salary := 100000.0
	profile := domain.ResumeProfile{ApplicationDefaults: domain.ApplicationDefaults{RequiresSponsorship: &no, NoticePeriod: "Two weeks", FormAnswers: map[string]string{"mapping_old": "Legacy reviewed answer"}, WorkRights: []domain.ApplicationWorkRight{{Country: "Australia", Status: "Permanent resident", RequiresSponsorship: &no}, {Country: "Germany", Status: "Work visa", VisaExpiryDate: "2025-01-01"}}, SalaryRange: &domain.ApplicationSalaryRange{Minimum: &salary, Currency: "AUD", PayPeriod: "year", Basis: "base", IncludesBenefits: &no}, CustomAnswers: []domain.ApplicationAnswer{{Question: "Travel?", Answer: "Occasional", Country: "Australia"}}}}
	require.NoError(t, config.NewStore(database).Set("candidate", "resume_profile", profile))
	_, err = database.Exec(`INSERT INTO form_field_mappings(id,field_sig,user_id,schema_version,provenance,binding_key,verified_by) VALUES('old-manual','manual-sig','candidate',4,'confirmed_by_reviewer','application_defaults.form_answers.mapping_old','candidate'),('old-model','model-sig','candidate',4,'verified_by_model','application_defaults.notice_period','grounded-form-answer-v1')`)
	require.NoError(t, err)
	_, err = database.Exec(`INSERT INTO apply_attempts(id,user_id,platform,outcome,external_identity_json) VALUES('old-attempt','candidate','seek','submission_unknown','{"tenant":"fixture"}')`)
	require.NoError(t, err)
	require.NoError(t, database.Close())
	database, err = appdb.Open(path)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	verify := func() {
		var got domain.ResumeProfile
		require.NoError(t, config.NewStore(database).Get("candidate", "resume_profile", &got))
		require.Equal(t, profile, got)
		var count int
		require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM form_field_mappings WHERE user_id='candidate' AND provenance IN ('confirmed_by_reviewer','verified_by_model')`).Scan(&count))
		require.Equal(t, 2, count)
		var outcome string
		require.NoError(t, database.QueryRow(`SELECT outcome FROM apply_attempts WHERE id='old-attempt'`).Scan(&outcome))
		require.Equal(t, "submission_unknown", outcome)
	}
	verify()
	var table string
	require.NoError(t, database.QueryRow(`SELECT name FROM sqlite_master WHERE name='application_answer_encounters'`).Scan(&table))
	require.NoError(t, goose.DownTo(database, "migrations", 60))
	verify()
}

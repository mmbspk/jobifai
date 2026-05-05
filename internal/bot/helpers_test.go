package bot

// White-box tests for pure helper functions that don't require a browser.
// Uses package bot (not bot_test) to access unexported functions.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
)

// ── deduplicateTitle ─────────────────────────────────────────────────────────

func TestDeduplicateTitle(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"no newline", "Senior Analyst", "Senior Analyst"},
		{"duplicated lines", "Senior Analyst\nSenior Analyst", "Senior Analyst"},
		{"different lines", "Senior Analyst\nJunior Analyst", "Senior Analyst\nJunior Analyst"},
		{"empty", "", ""},
		{"leading space", "  Manager\n  Manager", "Manager"},
		{"three lines no duplicate", "a\nb\nc", "a\nb\nc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, deduplicateTitle(tc.input))
		})
	}
}

// ── parseEmploymentYears ──────────────────────────────────────────────────────

func TestParseEmploymentYears(t *testing.T) {
	currentYear := time.Now().Year()
	cases := []struct {
		name        string
		period      string
		wantStart   int
		wantEnd     int
	}{
		{"empty", "", 0, 0},
		{"full range", "Jan 2019 - Mar 2023", 2019, 2023},
		{"present", "2020 - Present", 2020, currentYear},
		{"current", "2018 - current", 2018, currentYear},
		{"now", "2017 - now", 2017, currentYear},
		{"single year only", "2021", 2021, 2021},
		{"single year present", "2021 - present", 2021, currentYear},
		{"year range no month", "2015 - 2019", 2015, 2019},
		{"no years in string", "Not applicable", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := parseEmploymentYears(tc.period, currentYear)
			assert.Equal(t, tc.wantStart, start, "start year")
			assert.Equal(t, tc.wantEnd, end, "end year")
		})
	}
}

// ── totalExperienceYears ──────────────────────────────────────────────────────

func TestTotalExperienceYears(t *testing.T) {
	currentYear := time.Now().Year()

	t.Run("nil profile", func(t *testing.T) {
		assert.Equal(t, 0, totalExperienceYears(nil))
	})

	t.Run("two non-overlapping roles", func(t *testing.T) {
		p := &domain.ResumeProfile{
			ExperienceDetails: []domain.ExperienceDetail{
				{EmploymentPeriod: "2015 - 2018"}, // 3 years
				{EmploymentPeriod: "2019 - 2022"}, // 3 years
			},
		}
		assert.Equal(t, 6, totalExperienceYears(p))
	})

	t.Run("one role to present", func(t *testing.T) {
		p := &domain.ResumeProfile{
			ExperienceDetails: []domain.ExperienceDetail{
				{EmploymentPeriod: "2020 - Present"},
			},
		}
		want := currentYear - 2020
		assert.Equal(t, want, totalExperienceYears(p))
	})

	t.Run("empty experience", func(t *testing.T) {
		p := &domain.ResumeProfile{}
		assert.Equal(t, 0, totalExperienceYears(p))
	})
}

// ── extractFirstNumber ────────────────────────────────────────────────────────

func TestExtractFirstNumber(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"14 years", "14"},
		{"3.5 years experience", "3.5"},
		{"No number here", "No number here"},
		{"42", "42"},
		{"about 7 to 10 years", "7"},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, extractFirstNumber(tc.input))
		})
	}
}

// ── containsAny ───────────────────────────────────────────────────────────────

func TestContainsAny(t *testing.T) {
	assert.True(t, containsAny("hello world", "world", "foo"))
	assert.True(t, containsAny("hello world", "hello"))
	assert.False(t, containsAny("hello world", "xyz", "abc"))
	assert.False(t, containsAny("hello", /* no subs */))
}

// ── pickFormOption ────────────────────────────────────────────────────────────

func TestPickFormOption(t *testing.T) {
	options := []string{"Yes, I require sponsorship", "No sponsorship needed", "Not sure"}

	assert.Equal(t, "No sponsorship needed", pickFormOption(options, "No sponsorship"))
	assert.Equal(t, "Yes, I require sponsorship", pickFormOption(options, "require sponsorship"))
	// No match → fallback to first option
	assert.Equal(t, "Yes, I require sponsorship", pickFormOption(options, "nomatch"))
	// nil options → needle is returned as-is
	assert.Equal(t, "anything", pickFormOption(nil, "anything"))
}

// ── isSeekLoginPage ───────────────────────────────────────────────────────────

func TestIsSeekLoginPage(t *testing.T) {
	assert.True(t, isSeekLoginPage("https://www.seek.com.au/login"))
	assert.True(t, isSeekLoginPage("https://www.seek.com.au/oauth/authorize"))
	assert.True(t, isSeekLoginPage("https://id.seek.com/sign-in"))
	assert.False(t, isSeekLoginPage("https://au.seek.com/jobs?keywords=analyst"))
	assert.False(t, isSeekLoginPage(""))
}

// ── isBlacklisted / isSeekJobBlacklisted ─────────────────────────────────────

func newBotWithPrefs(prefs domain.WorkPreferences) *Bot {
	return &Bot{
		cfg:    Config{Preferences: prefs},
		stopCh: make(chan struct{}),
	}
}

func TestIsBlacklisted_Company(t *testing.T) {
	prefs := domain.WorkPreferences{
		CompanyBlacklist: []string{"BadCorp", "Evil Inc"},
	}
	b := newBotWithPrefs(prefs)

	assert.True(t, b.isBlacklisted(linkedInJob{Company: "BadCorp", Title: "Manager"}))
	assert.True(t, b.isBlacklisted(linkedInJob{Company: "badcorp", Title: "Manager"}), "case-insensitive")
	assert.True(t, b.isBlacklisted(linkedInJob{Company: "The Evil Inc Holdings", Title: "Director"}), "substring match")
	assert.False(t, b.isBlacklisted(linkedInJob{Company: "GoodCorp", Title: "Manager"}))
}

func TestIsBlacklisted_Title(t *testing.T) {
	prefs := domain.WorkPreferences{
		TitleBlacklist: []string{"intern", "trainee"},
	}
	b := newBotWithPrefs(prefs)

	assert.True(t, b.isBlacklisted(linkedInJob{Company: "Acme", Title: "Software Intern"}))
	assert.True(t, b.isBlacklisted(linkedInJob{Company: "Acme", Title: "TRAINEE Analyst"}), "case-insensitive")
	assert.False(t, b.isBlacklisted(linkedInJob{Company: "Acme", Title: "Senior Analyst"}))
}

func TestIsBlacklisted_Empty(t *testing.T) {
	b := newBotWithPrefs(domain.WorkPreferences{})
	assert.False(t, b.isBlacklisted(linkedInJob{Company: "AnyCompany", Title: "Any Role"}))
}

func TestIsSeekJobBlacklisted(t *testing.T) {
	prefs := domain.WorkPreferences{
		CompanyBlacklist: []string{"ToxicCo"},
		TitleBlacklist:   []string{"unpaid"},
	}
	b := newBotWithPrefs(prefs)

	assert.True(t, b.isSeekJobBlacklisted(seekJob{Company: "ToxicCo", Title: "Analyst"}))
	assert.True(t, b.isSeekJobBlacklisted(seekJob{Company: "Good", Title: "Unpaid Research Assistant"}))
	assert.False(t, b.isSeekJobBlacklisted(seekJob{Company: "GoodCo", Title: "Senior Analyst"}))
}

// ── unmarshalHalalVerdict ─────────────────────────────────────────────────────

func TestUnmarshalHalalVerdict(t *testing.T) {
	t.Run("nil on empty", func(t *testing.T) {
		assert.Nil(t, unmarshalHalalVerdict(nil))
		assert.Nil(t, unmarshalHalalVerdict([]byte("")))
	})

	t.Run("valid verdict", func(t *testing.T) {
		raw := []byte(`{"verdict":"HALAL","confidence":"HIGH","summary":"All good","reasons":["no interest"]}`)
		v := unmarshalHalalVerdict(raw)
		assert.NotNil(t, v)
		assert.Equal(t, "HALAL", v.Verdict)
		assert.Equal(t, "HIGH", v.Confidence)
		assert.Equal(t, []string{"no interest"}, v.Reasons)
	})

	t.Run("nil on invalid JSON", func(t *testing.T) {
		assert.Nil(t, unmarshalHalalVerdict([]byte("not json")))
	})
}

// ── seekLocationPath ─────────────────────────────────────────────────────────

func TestSeekLocationPath(t *testing.T) {
	// Known slug
	path := seekLocationPath("Sydney")
	assert.Contains(t, path, "/jobs/in-")
	assert.NotEmpty(t, path)

	// Unknown location → path-escaped fallback
	custom := seekLocationPath("Wollongong")
	assert.Equal(t, "/jobs/in-Wollongong", custom)

	// Empty → base path
	assert.Equal(t, "/jobs", seekLocationPath(""))
	assert.Equal(t, "/jobs", seekLocationPath("All Australia"))
}

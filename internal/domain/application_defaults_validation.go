package domain

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

var applicationCurrencyCode = regexp.MustCompile(`^[A-Za-z]{3}$`)

// Validate checks supplied application facts without completing missing facts.
// Optional unknown booleans and legacy free-text answers remain valid.
func (a ApplicationDefaults) Validate() error {
	if a.EarliestStartDate != "" {
		if _, err := time.Parse("2006-01-02", a.EarliestStartDate); err != nil {
			return fmt.Errorf("earliest start date must use a valid YYYY-MM-DD date")
		}
	}
	if s := a.SalaryRange; s != nil {
		for _, value := range []*float64{s.Minimum, s.Maximum} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
				return fmt.Errorf("salary amounts must be finite numbers of zero or more")
			}
		}
		if s.Minimum != nil && s.Maximum != nil && *s.Minimum > *s.Maximum {
			return fmt.Errorf("salary minimum must not exceed the maximum")
		}
		if s.Minimum != nil || s.Maximum != nil {
			if !applicationCurrencyCode.MatchString(strings.TrimSpace(s.Currency)) {
				return fmt.Errorf("salary currency must be a three-letter currency code")
			}
			switch s.PayPeriod {
			case "hour", "week", "month", "year":
			default:
				return fmt.Errorf("choose the salary pay period")
			}
			switch s.Basis {
			case "base", "total":
			default:
				return fmt.Errorf("choose whether salary is base pay or total compensation")
			}
		}
	}
	for i, right := range a.WorkRights {
		if strings.TrimSpace(right.Country) == "" {
			return fmt.Errorf("work rights entry %d requires a country", i+1)
		}
		if right.VisaExpiryDate != "" {
			if _, err := time.Parse("2006-01-02", right.VisaExpiryDate); err != nil {
				return fmt.Errorf("visa expiry date in work rights entry %d must use a valid YYYY-MM-DD date", i+1)
			}
		}
	}
	for i, answer := range a.CustomAnswers {
		if strings.TrimSpace(answer.Question) == "" || strings.TrimSpace(answer.Answer) == "" {
			return fmt.Errorf("custom answer %d requires both a question and an answer", i+1)
		}
	}
	return nil
}

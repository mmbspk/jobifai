package domain

import (
	"strings"
)

// LinkedInExperienceCodes returns f_E codes for the selected experience levels.
// See https://www.linkedin.com/jobs/search/ — 1=internship … 6=executive.
// Returns empty when nothing is selected (caller should omit f_E).
func LinkedInExperienceCodes(e ExperienceLevelConfig) []string {
	var codes []string
	if e.Internship {
		codes = append(codes, "1")
	}
	if e.Entry {
		codes = append(codes, "2")
	}
	if e.Associate {
		codes = append(codes, "3")
	}
	if e.MidSenior || e.Senior {
		codes = append(codes, "4")
	}
	if e.Director {
		codes = append(codes, "5")
	}
	if e.Executive {
		codes = append(codes, "6")
	}
	return uniqueStrings(codes)
}

// MatchesExperienceTitle checks whether a job title fits the selected experience
// bands. Used to post-filter Seek results (Seek search URLs have no experience param).
func (e ExperienceLevelConfig) MatchesExperienceTitle(title string) bool {
	if !e.anyExperienceSelected() {
		return true
	}
	levels := inferTitleExperienceLevels(title)
	if len(levels) == 0 {
		// Generic titles ("Software Engineer") — allow when broad bands are selected.
		return e.MidSenior || e.Senior || e.Associate || e.Entry
	}
	for _, l := range levels {
		if e.levelSelected(l) {
			return true
		}
	}
	return false
}

func (e ExperienceLevelConfig) anyExperienceSelected() bool {
	return e.Internship || e.Entry || e.Associate || e.MidSenior || e.Senior || e.Director || e.Executive
}

func (e ExperienceLevelConfig) levelSelected(l string) bool {
	switch l {
	case "internship":
		return e.Internship
	case "entry":
		return e.Entry
	case "associate":
		return e.Associate
	case "mid_senior":
		return e.MidSenior || e.Senior
	case "director":
		return e.Director
	case "executive":
		return e.Executive
	default:
		return false
	}
}

func inferTitleExperienceLevels(title string) []string {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return nil
	}
	var levels []string
	if containsAny(t, "graduate program", "student placement") || strings.Contains(t, "intern") {
		levels = append(levels, "internship")
	}
	if containsAny(t, "junior", "entry level", "entry-level", "graduate", " grad ") {
		levels = append(levels, "entry")
	}
	if strings.Contains(t, "associate") && !strings.Contains(t, "senior associate") {
		levels = append(levels, "associate")
	}
	if containsAny(t, "director", "head of") {
		levels = append(levels, "director")
	}
	if containsAny(t, "executive", "chief ", " ceo", " cto", " cfo", " coo", "vice president", " vp ") {
		levels = append(levels, "executive")
	}
	if containsAny(t, "senior", "principal", "staff engineer", "lead engineer", "lead developer") {
		levels = append(levels, "mid_senior")
	}
	return uniqueStrings(levels)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// LocationMatchesBlacklist returns true when loc matches any blacklisted location string.
func LocationMatchesBlacklist(loc string, blacklist []string) bool {
	locLower := strings.ToLower(strings.TrimSpace(loc))
	if locLower == "" {
		return false
	}
	for _, b := range blacklist {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		bl := strings.ToLower(b)
		if strings.Contains(locLower, bl) || strings.Contains(bl, locLower) {
			return true
		}
	}
	return false
}

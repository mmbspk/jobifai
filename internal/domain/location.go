package domain

import (
	"strings"
)

type locationProfile struct {
	seekWhere string
	seekSlug  string
	linkedIn  string
}

// canonicalLocations maps normalized aliases to platform-specific search strings.
var canonicalLocations = map[string]locationProfile{
	"adelaide":            {seekWhere: "All Adelaide SA", seekSlug: "Adelaide-SA", linkedIn: "Adelaide, South Australia, Australia"},
	"adelaide sa":         {seekWhere: "All Adelaide SA", seekSlug: "Adelaide-SA", linkedIn: "Adelaide, South Australia, Australia"},
	"all adelaide sa":     {seekWhere: "All Adelaide SA", seekSlug: "Adelaide-SA", linkedIn: "Adelaide, South Australia, Australia"},
	"melbourne":           {seekWhere: "All Melbourne VIC", seekSlug: "Melbourne-VIC", linkedIn: "Melbourne, Victoria, Australia"},
	"melbourne vic":       {seekWhere: "All Melbourne VIC", seekSlug: "Melbourne-VIC", linkedIn: "Melbourne, Victoria, Australia"},
	"all melbourne vic":   {seekWhere: "All Melbourne VIC", seekSlug: "Melbourne-VIC", linkedIn: "Melbourne, Victoria, Australia"},
	"sydney":              {seekWhere: "All Sydney NSW", seekSlug: "Sydney-NSW", linkedIn: "Sydney, New South Wales, Australia"},
	"sydney nsw":          {seekWhere: "All Sydney NSW", seekSlug: "Sydney-NSW", linkedIn: "Sydney, New South Wales, Australia"},
	"all sydney nsw":      {seekWhere: "All Sydney NSW", seekSlug: "Sydney-NSW", linkedIn: "Sydney, New South Wales, Australia"},
	"brisbane":            {seekWhere: "All Brisbane QLD", seekSlug: "Brisbane-QLD", linkedIn: "Brisbane, Queensland, Australia"},
	"brisbane qld":        {seekWhere: "All Brisbane QLD", seekSlug: "Brisbane-QLD", linkedIn: "Brisbane, Queensland, Australia"},
	"all brisbane qld":    {seekWhere: "All Brisbane QLD", seekSlug: "Brisbane-QLD", linkedIn: "Brisbane, Queensland, Australia"},
	"perth":               {seekWhere: "All Perth WA", seekSlug: "Perth-WA", linkedIn: "Perth, Western Australia, Australia"},
	"perth wa":            {seekWhere: "All Perth WA", seekSlug: "Perth-WA", linkedIn: "Perth, Western Australia, Australia"},
	"all perth wa":        {seekWhere: "All Perth WA", seekSlug: "Perth-WA", linkedIn: "Perth, Western Australia, Australia"},
	"canberra":            {seekWhere: "All Canberra ACT", seekSlug: "Canberra-ACT", linkedIn: "Canberra, Australian Capital Territory, Australia"},
	"all canberra act":    {seekWhere: "All Canberra ACT", seekSlug: "Canberra-ACT", linkedIn: "Canberra, Australian Capital Territory, Australia"},
	"hobart":              {seekWhere: "All Hobart TAS", seekSlug: "Hobart-TAS", linkedIn: "Hobart, Tasmania, Australia"},
	"all hobart tas":      {seekWhere: "All Hobart TAS", seekSlug: "Hobart-TAS", linkedIn: "Hobart, Tasmania, Australia"},
	"darwin":              {seekWhere: "All Darwin NT", seekSlug: "Darwin-NT", linkedIn: "Darwin, Northern Territory, Australia"},
	"all darwin nt":       {seekWhere: "All Darwin NT", seekSlug: "Darwin-NT", linkedIn: "Darwin, Northern Territory, Australia"},
	"victoria":            {seekWhere: "Victoria VIC", seekSlug: "Victoria VIC", linkedIn: "Victoria, Australia"},
	"vic":                 {seekWhere: "Victoria VIC", seekSlug: "Victoria VIC", linkedIn: "Victoria, Australia"},
	"victoria vic":        {seekWhere: "Victoria VIC", seekSlug: "Victoria VIC", linkedIn: "Victoria, Australia"},
	"victoria, australia": {seekWhere: "Victoria VIC", seekSlug: "Victoria VIC", linkedIn: "Victoria, Australia"},
	"new south wales":     {seekWhere: "New South Wales NSW", seekSlug: "New-South-Wales", linkedIn: "New South Wales, Australia"},
	"nsw":                 {seekWhere: "New South Wales NSW", seekSlug: "New-South-Wales", linkedIn: "New South Wales, Australia"},
	"queensland":          {seekWhere: "Queensland QLD", seekSlug: "Queensland", linkedIn: "Queensland, Australia"},
	"qld":                 {seekWhere: "Queensland QLD", seekSlug: "Queensland", linkedIn: "Queensland, Australia"},
	"south australia":     {seekWhere: "South Australia SA", seekSlug: "South-Australia", linkedIn: "South Australia, Australia"},
	"sa":                  {seekWhere: "South Australia SA", seekSlug: "South-Australia", linkedIn: "South Australia, Australia"},
	"western australia":   {seekWhere: "Western Australia WA", seekSlug: "Western-Australia", linkedIn: "Western Australia, Australia"},
	"wa":                  {seekWhere: "Western Australia WA", seekSlug: "Western-Australia", linkedIn: "Western Australia, Australia"},
	"tasmania":            {seekWhere: "Tasmania TAS", seekSlug: "Tasmania", linkedIn: "Tasmania, Australia"},
	"tas":                 {seekWhere: "Tasmania TAS", seekSlug: "Tasmania", linkedIn: "Tasmania, Australia"},
	"northern territory":  {seekWhere: "Northern Territory NT", seekSlug: "Northern-Territory", linkedIn: "Northern Territory, Australia"},
	"nt":                  {seekWhere: "Northern Territory NT", seekSlug: "Northern-Territory", linkedIn: "Northern Territory, Australia"},
	"act":                 {seekWhere: "Australian Capital Territory ACT", seekSlug: "Australian-Capital-Territory", linkedIn: "Australian Capital Territory, Australia"},
	"australian capital territory": {seekWhere: "Australian Capital Territory ACT", seekSlug: "Australian-Capital-Territory", linkedIn: "Australian Capital Territory, Australia"},
	// LinkedIn-style labels from location suggest
	"adelaide, south australia, australia":              {seekWhere: "All Adelaide SA", seekSlug: "Adelaide-SA", linkedIn: "Adelaide, South Australia, Australia"},
	"melbourne, victoria, australia":                    {seekWhere: "All Melbourne VIC", seekSlug: "Melbourne-VIC", linkedIn: "Melbourne, Victoria, Australia"},
	"sydney, new south wales, australia":                {seekWhere: "All Sydney NSW", seekSlug: "Sydney-NSW", linkedIn: "Sydney, New South Wales, Australia"},
	"brisbane, queensland, australia":                   {seekWhere: "All Brisbane QLD", seekSlug: "Brisbane-QLD", linkedIn: "Brisbane, Queensland, Australia"},
	"perth, western australia, australia":               {seekWhere: "All Perth WA", seekSlug: "Perth-WA", linkedIn: "Perth, Western Australia, Australia"},
	"canberra, australian capital territory, australia": {seekWhere: "All Canberra ACT", seekSlug: "Canberra-ACT", linkedIn: "Canberra, Australian Capital Territory, Australia"},
	"hobart, tasmania, australia":                       {seekWhere: "All Hobart TAS", seekSlug: "Hobart-TAS", linkedIn: "Hobart, Tasmania, Australia"},
	"darwin, northern territory, australia":             {seekWhere: "All Darwin NT", seekSlug: "Darwin-NT", linkedIn: "Darwin, Northern Territory, Australia"},
}

var auStateAbbrev = map[string]string{
	"vic": "Victoria",
	"nsw": "New South Wales",
	"qld": "Queensland",
	"sa":  "South Australia",
	"wa":  "Western Australia",
	"tas": "Tasmania",
	"nt":  "Northern Territory",
	"act": "Australian Capital Territory",
}

// NormalizeLocationKey lowercases and collapses whitespace for alias lookup.
func NormalizeLocationKey(loc string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(loc))), " ")
}

// SeekSearchLocation resolves a preference location for Seek ?where= search.
// When resolved is true, the value is ready without browser autocomplete.
func SeekSearchLocation(loc string) (where string, resolved bool) {
	loc = strings.TrimSpace(loc)
	if loc == "" || isNationwideLocation(loc) {
		return "", true
	}
	key := NormalizeLocationKey(loc)
	if profile, ok := canonicalLocations[key]; ok {
		return profile.seekWhere, true
	}
	if strings.HasPrefix(key, "all ") {
		return loc, true
	}
	formatted := formatSeekFallback(loc)
	if formatted != "" && formatted != loc {
		return formatted, true
	}
	return "", false
}

// FormatSearchLocation converts a preference location into the best search string
// for the given platform. Returns empty for nationwide / unset locations.
func FormatSearchLocation(loc string, platform Platform) string {
	loc = strings.TrimSpace(loc)
	if loc == "" || isNationwideLocation(loc) {
		return ""
	}

	if profile, ok := canonicalLocations[NormalizeLocationKey(loc)]; ok {
		return profileForPlatform(profile, platform)
	}

	switch platform {
	case PlatformSeek:
		return formatSeekFallback(loc)
	case PlatformLinkedIn:
		return formatLinkedInFallback(loc)
	default:
		return loc
	}
}

func profileForPlatform(p locationProfile, platform Platform) string {
	switch platform {
	case PlatformSeek:
		return p.seekWhere
	case PlatformLinkedIn:
		return p.linkedIn
	default:
		return ""
	}
}

func isNationwideLocation(loc string) bool {
	switch NormalizeLocationKey(loc) {
	case "australia", "all australia", "nationwide", "remote", "anywhere":
		return true
	}
	return false
}

func formatSeekFallback(loc string) string {
	key := NormalizeLocationKey(loc)
	if strings.HasPrefix(key, "all ") {
		// Already Seek-style ("All Melbourne VIC").
		return strings.TrimSpace(loc)
	}

	parts := splitLocationParts(loc)
	if len(parts) >= 3 && strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "Australia") {
		cityKey := NormalizeLocationKey(parts[0])
		if profile, ok := canonicalLocations[cityKey]; ok {
			return profile.seekWhere
		}
	}

	if len(parts) == 2 {
		cityKey := NormalizeLocationKey(parts[0])
		if profile, ok := canonicalLocations[cityKey]; ok {
			return profile.seekWhere
		}
	}

	if len(parts) >= 2 {
		city := strings.TrimSpace(parts[0])
		stateToken := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
		if abbrev, ok := auStateNameToAbbrev(stateToken); ok {
			return "All " + city + " " + abbrev
		}
		if len(stateToken) <= 3 {
			return "All " + city + " " + strings.ToUpper(stateToken)
		}
	}

	return strings.TrimSpace(loc)
}

func formatLinkedInFallback(loc string) string {
	key := NormalizeLocationKey(loc)
	if strings.HasPrefix(key, "all ") {
		rest := strings.TrimSpace(loc[4:])
		fields := strings.Fields(rest)
		if len(fields) >= 2 {
			city := fields[0]
			abbrev := strings.ToLower(fields[len(fields)-1])
			if state, ok := auStateAbbrev[abbrev]; ok {
				return city + ", " + state + ", Australia"
			}
		}
	}

	parts := splitLocationParts(loc)
	if len(parts) >= 2 && !strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "Australia") {
		last := strings.TrimSpace(parts[len(parts)-1])
		if state, ok := auStateAbbrev[strings.ToLower(last)]; ok {
			return strings.TrimSpace(parts[0]) + ", " + state + ", Australia"
		}
	}

	if len(parts) >= 3 {
		return strings.TrimSpace(loc)
	}

	return strings.TrimSpace(loc)
}

// SeekLocationPath returns Seek's /jobs/in-{slug} path for loc, or /jobs when nationwide.
func SeekLocationPath(loc string) string {
	loc = strings.TrimSpace(loc)
	if loc == "" || isNationwideLocation(loc) {
		return "/jobs"
	}

	if profile, ok := canonicalLocations[NormalizeLocationKey(loc)]; ok && profile.seekSlug != "" {
		return "/jobs/in-" + profile.seekSlug
	}

	seekWhere := FormatSearchLocation(loc, PlatformSeek)
	if seekWhere != "" {
		if profile, ok := canonicalLocations[NormalizeLocationKey(seekWhere)]; ok && profile.seekSlug != "" {
			return "/jobs/in-" + profile.seekSlug
		}
		key := NormalizeLocationKey(seekWhere)
		if strings.HasPrefix(key, "all ") {
			rest := strings.TrimSpace(seekWhere[4:])
			fields := strings.Fields(rest)
			if len(fields) >= 2 {
				return "/jobs/in-" + fields[0] + "-" + fields[len(fields)-1]
			}
		}
	}

	if loc != "" {
		return "/jobs/in-" + strings.ReplaceAll(loc, " ", "-")
	}
	return "/jobs"
}

func splitLocationParts(loc string) []string {
	var parts []string
	for _, p := range strings.Split(loc, ",") {
		if s := strings.TrimSpace(p); s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}

func auStateNameToAbbrev(state string) (string, bool) {
	switch NormalizeLocationKey(state) {
	case "victoria":
		return "VIC", true
	case "new south wales":
		return "NSW", true
	case "queensland":
		return "QLD", true
	case "south australia":
		return "SA", true
	case "western australia":
		return "WA", true
	case "tasmania":
		return "TAS", true
	case "northern territory":
		return "NT", true
	case "australian capital territory":
		return "ACT", true
	default:
		if len(state) <= 3 {
			return strings.ToUpper(state), true
		}
		return "", false
	}
}

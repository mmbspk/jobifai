package domain

// Normalize migrates legacy flat locations + global work-type flags into
// SearchTargets and keeps deprecated fields in sync for older call sites.
func (p *WorkPreferences) Normalize() {
	if p == nil {
		return
	}

	if len(p.SearchTargets) == 0 {
		if len(p.Locations) > 0 {
			for _, loc := range p.Locations {
				p.SearchTargets = append(p.SearchTargets, SearchTarget{
					Location: loc,
					Remote:   p.Remote,
					Hybrid:   p.Hybrid,
					Onsite:   p.Onsite,
				})
			}
		} else {
			p.SearchTargets = []SearchTarget{{
				Remote: p.Remote,
				Hybrid: p.Hybrid,
				Onsite: p.Onsite,
			}}
		}
	}

	p.Locations = nil
	for _, t := range p.SearchTargets {
		p.Locations = append(p.Locations, t.Location)
	}

	if len(p.SearchTargets) > 0 {
		first := p.SearchTargets[0]
		p.Remote = first.Remote
		p.Hybrid = first.Hybrid
		p.Onsite = first.Onsite
	}

	p.normalizeDateFilter()
}

// normalizeDateFilter keeps a single date window active. Bot search URLs use one
// filter only (hours_24 > week > month > all_time); multiple UI selections were misleading.
func (p *WorkPreferences) normalizeDateFilter() {
	var pick string
	switch {
	case p.Date.Hours24:
		pick = "hours_24"
	case p.Date.Week:
		pick = "week"
	case p.Date.Month:
		pick = "month"
	case p.Date.AllTime:
		pick = "all_time"
	default:
		pick = "week"
	}
	p.Date = DateFilterConfig{}
	switch pick {
	case "hours_24":
		p.Date.Hours24 = true
	case "week":
		p.Date.Week = true
	case "month":
		p.Date.Month = true
	case "all_time":
		p.Date.AllTime = true
	}
}

// EffectiveSearchTargets returns normalized search targets, always at least one
// entry (nationwide when no location is configured).
func (p WorkPreferences) EffectiveSearchTargets() []SearchTarget {
	cp := p
	cp.Normalize()
	return cp.SearchTargets
}

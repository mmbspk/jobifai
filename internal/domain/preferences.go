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
}

// EffectiveSearchTargets returns normalized search targets, always at least one
// entry (nationwide when no location is configured).
func (p WorkPreferences) EffectiveSearchTargets() []SearchTarget {
	cp := p
	cp.Normalize()
	return cp.SearchTargets
}

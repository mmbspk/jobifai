package jobpage

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func fixtureSnapshot() Snapshot {
	target, scope, _ := NormalizeTarget(Target{URL: "https://www.linkedin.com/jobs/view/123/?session=private", Title: "Engineer", Company: "Fixture", UserID: "private-candidate"})
	s := Snapshot{Version: 1, ID: "epoch-1", Scope: scope, Target: target, Identity: Identity{JobID: "123", Title: "Engineer", Company: "Fixture", Verified: true, HeaderRefs: []string{"n0"}, Evidence: []string{"subject_attribute", "primary_header"}}, Nodes: []Node{
		{Ref: "n0", Tag: "h1", Text: "Engineer", Context: ContextHeader, Owned: true, OwnerJobID: "123", Visible: true},
		{Ref: "n1", Tag: "section", Text: strings.Repeat("A supported current job description. ", 6), Attributes: map[string]string{"data-testid": "job-description", "data-job-id": "123"}, Context: ContextCandidate, Owned: true, OwnerJobID: "123", Visible: true},
	}}
	s.LayoutSignature = StructuralSignature(s)
	return s
}

func TestJobPageContractRejectsWrongOriginsAndDeclaredIdentity(t *testing.T) {
	for _, raw := range []string{"https://linkedin.com.attacker.test/jobs/view/123", "https://user:secret@www.linkedin.com/jobs/view/123", "http://www.linkedin.com/jobs/view/123", "https://www.linkedin.com:9443/jobs/view/123", "https://www.linkedin.com/in/candidate"} {
		_, _, err := NormalizeTarget(Target{URL: raw})
		require.Error(t, err, raw)
	}
	_, _, err := NormalizeTarget(Target{URL: "https://www.seek.com.au/job/456", JobID: "123"})
	require.Equal(t, FailureIdentity, ErrorCode(err))
	s := fixtureSnapshot()
	require.NoError(t, ValidateSnapshot(s, DefaultLimits()))
	s.Identity.Evidence = []string{"canonical_jobid"}
	require.Equal(t, FailureIdentity, ErrorCode(ValidateSnapshot(s, DefaultLimits())), "URL matching alone cannot establish ownership")
}

func TestJobPageContractNormalizesSupportedBoardAliases(t *testing.T) {
	for _, pair := range [][2]string{{"https://au.seek.com/job/123", "https://www.seek.com.au/job/123"}, {"https://seek.com.au/job/123", "https://www.seek.com.au/job/123"}, {"https://nz.seek.com/job/123", "https://www.seek.co.nz/job/123"}, {"https://linkedin.com/jobs/view/123", "https://www.linkedin.com/jobs/view/123/"}} {
		actual, scope, err := NormalizeTarget(Target{URL: pair[0]})
		require.NoError(t, err)
		expected, want, err := NormalizeTarget(Target{URL: pair[1]})
		require.NoError(t, err)
		require.Equal(t, expected, actual)
		require.Equal(t, want, scope)
	}
	_, _, err := NormalizeTarget(Target{URL: "https://au.seek.com.attacker.test/job/123"})
	require.Equal(t, FailureOrigin, ErrorCode(err))
}

func TestJobPagePlanSelectsOnlyCurrentOwnedDescriptionNodes(t *testing.T) {
	s := fixtureSnapshot()
	p := Plan{Version: 1, SnapshotID: s.ID, DescriptionRefs: []string{"n1"}}
	require.NoError(t, ValidatePlan(s, p, DefaultLimits()))
	for _, change := range []func(*Plan){
		func(p *Plan) { p.SnapshotID = "older-epoch" },
		func(p *Plan) { p.DescriptionRefs = []string{"n999"} },
		func(p *Plan) { p.DescriptionRefs = []string{"n0"} },
		func(p *Plan) { p.DescriptionRefs = []string{"n1", "n1"} },
		func(p *Plan) { p.Actions = []Action{{Kind: "submit", Ref: "n1"}} },
	} {
		copy := p
		change(&copy)
		require.Error(t, ValidatePlan(s, copy, DefaultLimits()))
	}
	s.Nodes[1].OwnerJobID = "another-job"
	require.Equal(t, FailureIdentity, ErrorCode(ValidatePlan(s, p, DefaultLimits())))
}

func TestJobPagePlanCannotContainInventedContentOrExecutableCode(t *testing.T) {
	encoded, _ := json.Marshal(Plan{Version: 1, SnapshotID: "epoch-1", DescriptionRefs: []string{"n1"}})
	for _, key := range []string{"description", "javascript", "selectors", "upload"} {
		var value map[string]any
		require.NoError(t, json.Unmarshal(encoded, &value))
		value[key] = "unobserved or executable output"
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		_, err = DecodePlan(string(raw))
		require.Equal(t, FailurePlan, ErrorCode(err))
	}
	_, err := DecodePlan(string(encoded) + " {}")
	require.Equal(t, FailurePlan, ErrorCode(err))
}

func TestJobPageVerificationMustCoverExactCurrentSelection(t *testing.T) {
	s := fixtureSnapshot()
	p := Plan{Version: 1, SnapshotID: s.ID, DescriptionRefs: []string{"n1"}}
	v := Verification{SnapshotID: s.ID, Verdicts: []Verdict{{Ref: "n1", IsDescription: true, Supported: true, Complete: true}}}
	require.NoError(t, ValidateVerification(s, p, v))
	v.Verdicts[0].Complete = false
	require.Equal(t, FailureVerification, ErrorCode(ValidateVerification(s, p, v)))
	v.Verdicts[0].Complete = true
	v.Verdicts[0].Ref = "n0"
	require.Equal(t, FailureVerification, ErrorCode(ValidateVerification(s, p, v)))
}

func TestJobPageSignatureIgnoresVacancyTextAndPrivateAttribution(t *testing.T) {
	first := fixtureSnapshot()
	second := fixtureSnapshot()
	second.Target.JobID = "456"
	second.Target.Title = "Designer"
	second.Target.Company = "Other"
	second.Identity.JobID = "456"
	second.Nodes[0].Text = "Designer"
	second.Nodes[0].OwnerJobID = "456"
	second.Nodes[1].OwnerJobID = "456"
	second.Nodes[1].Text = "Another vacancy's current content"
	second.Nodes[1].Attributes["data-job-id"] = "456"
	require.Equal(t, StructuralSignature(first), StructuralSignature(second))
	second.Nodes[1].Attributes["data-testid"] = "different-layout"
	require.NotEqual(t, StructuralSignature(first), StructuralSignature(second))
	raw, err := json.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-candidate")
	require.NotContains(t, string(raw), "session=private")
	first.Nodes[1].Attributes["cookie"] = "secret"
	require.Error(t, ValidateSnapshot(first, DefaultLimits()))
}

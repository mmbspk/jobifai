package resume_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/resume"
)

func makeTestProfile() *domain.ResumeProfile {
	exp := make([]domain.ExperienceDetail, 5)
	for i := range exp {
		exp[i] = domain.ExperienceDetail{
			Position: "Analyst",
			Company:  "Corp",
			KeyResponsibilities: []string{"task a", "task b", "task c", "task d", "task e"},
		}
	}
	return &domain.ResumeProfile{
		PersonalInformation: domain.PersonalInformation{Name: "Alice", Email: "alice@example.com"},
		Summary:             "experienced professional",
		Skills:              []string{"Go", "Python"},
		ExperienceDetails:   exp,
		Interests:           []string{"reading"},
		Certifications:      []domain.Certification{{Name: "AWS"}},
	}
}

func TestForScoring_CapsExperience(t *testing.T) {
	p := makeTestProfile()
	out := resume.ForScoring(p)
	assert.LessOrEqual(t, len(out.ExperienceDetails), 3, "caps to 3 roles")
	for _, e := range out.ExperienceDetails {
		assert.LessOrEqual(t, len(e.KeyResponsibilities), 3, "caps bullets to 3")
	}
}

func TestForScoring_RemovesContactInfo(t *testing.T) {
	p := makeTestProfile()
	out := resume.ForScoring(p)
	assert.Empty(t, out.PersonalInformation.Email, "ForScoring omits personal information")
	assert.Empty(t, out.PersonalInformation.Name)
	assert.Empty(t, out.Interests)
	assert.Empty(t, out.Certifications)
}

func TestForScoring_KeepsSummaryAndSkills(t *testing.T) {
	p := makeTestProfile()
	out := resume.ForScoring(p)
	assert.Equal(t, "experienced professional", out.Summary)
	assert.Equal(t, []string{"Go", "Python"}, out.Skills)
}

func TestForTailoring_IncludesFullDetails(t *testing.T) {
	p := makeTestProfile()
	out := resume.ForTailoring(p)
	assert.Len(t, out.ExperienceDetails, len(p.ExperienceDetails), "keeps all roles")
	assert.Nil(t, out.Interests, "strips interests")
	assert.NotEmpty(t, out.Certifications, "preserves certifications")
}

func TestForCoverLetter_KeepsKeyFields(t *testing.T) {
	p := makeTestProfile()
	out := resume.ForCoverLetter(p)
	assert.Equal(t, "experienced professional", out.Summary)
	assert.LessOrEqual(t, len(out.ExperienceDetails), 3, "caps roles to 3")
	for _, e := range out.ExperienceDetails {
		assert.LessOrEqual(t, len(e.KeyResponsibilities), 5, "caps bullets to 5")
	}
}

func TestForFormFilling_KeepsContactInfo(t *testing.T) {
	p := makeTestProfile()
	p.Skills = make([]string, 15)
	for i := range p.Skills {
		p.Skills[i] = "skill"
	}
	out := resume.ForFormFilling(p)
	assert.Equal(t, "alice@example.com", out.PersonalInformation.Email)
	assert.LessOrEqual(t, len(out.Skills), 10, "caps skills to 10")
	for _, e := range out.ExperienceDetails {
		assert.Empty(t, e.KeyResponsibilities, "strips bullet points")
	}
}

func TestForScoring_NilProfile(t *testing.T) {
	out := resume.ForScoring(nil)
	assert.Empty(t, out.Skills)
}

func TestForTailoring_NilProfile(t *testing.T) {
	out := resume.ForTailoring(nil)
	assert.Empty(t, out.Skills)
}

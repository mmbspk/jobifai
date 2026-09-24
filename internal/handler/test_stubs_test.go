package handler_test

import (
	"context"
	"io"

	"github.com/user/jobifai/internal/bot"
	"github.com/user/jobifai/internal/domain"
	"github.com/user/jobifai/internal/handler"
)

type stubRenderer struct{}

func (stubRenderer) RenderResume(_ context.Context, _ *domain.ResumeProfile, _, _ string) ([]byte, error) {
	return []byte("%PDF-resume-stub"), nil
}

func (stubRenderer) RenderCoverLetter(_ context.Context, _ string, _, _ string) ([]byte, error) {
	return []byte("%PDF-cover-stub"), nil
}

type stubTailor struct{}

func (stubTailor) TailorProfile(_ context.Context, profile *domain.ResumeProfile, _ string) (*domain.ResumeProfile, error) {
	return profile, nil
}

func (stubTailor) WriteCoverLetter(_ context.Context, _ *domain.ResumeProfile, _ string) (string, error) {
	return "Dear hiring team,\n", nil
}

type stubExtractor struct{}

func (stubExtractor) ExtractFromText(_ context.Context, _ string) (*domain.ResumeProfile, error) {
	return &domain.ResumeProfile{Summary: "extracted"}, nil
}

type stubEvaluator struct{}

func (stubEvaluator) EvaluateJob(_ context.Context, _ *domain.ResumeProfile, _ string) (domain.JobScore, error) {
	return domain.JobScore{Score: 8, Reasoning: "Strong alignment with role requirements."}, nil
}

type stubHalalChecker struct{}

func (stubHalalChecker) CheckHalal(_ context.Context, _, _, _ string) (domain.HalalVerdict, error) {
	return domain.HalalVerdict{Verdict: "HALAL", Confidence: "HIGH", Summary: "Permissible role."}, nil
}

type stubQuestionAnswerer struct{}

func (stubQuestionAnswerer) AnswerQuestions(_ context.Context, _ *domain.ResumeProfile, _ string, questions []string) ([]domain.QuestionAnswer, error) {
	out := make([]domain.QuestionAnswer, len(questions))
	for i, q := range questions {
		out[i] = domain.QuestionAnswer{Question: q, Answer: "Sample answer."}
	}
	return out, nil
}

func stubLLMFactories() (func(string) (handler.ResumeExtractor, handler.ResumeTailor), func(string) handler.JobEvaluator, func(string) handler.JobHalalChecker, func(string) handler.JobQuestionAnswerer) {
	ext := stubExtractor{}
	tailor := stubTailor{}
	evaluator := stubEvaluator{}
	halal := stubHalalChecker{}
	answerer := stubQuestionAnswerer{}
	return func(string) (handler.ResumeExtractor, handler.ResumeTailor) { return ext, tailor },
		func(string) handler.JobEvaluator { return evaluator },
		func(string) handler.JobHalalChecker { return halal },
		func(string) handler.JobQuestionAnswerer { return answerer }
}

func sampleResumeProfile() domain.ResumeProfile {
	return domain.ResumeProfile{
		Summary: "Experienced operations professional.",
		PersonalInformation: domain.PersonalInformation{
			Name:  "Alex Example",
			Email: "alex@example.com",
		},
		Skills: []string{"planning", "client communication"},
	}
}

type recordingBot struct {
	state   domain.BotState
	started []domain.Platform
}

func (b *recordingBot) Start(_ context.Context, _ string, platform domain.Platform) error {
	b.started = append(b.started, platform)
	b.state = domain.BotStateRunning
	return nil
}

func (b *recordingBot) Stop(_ string) { b.state = domain.BotStateStopped }

func (b *recordingBot) Pause(_ string) {
	if b.state == domain.BotStateRunning {
		b.state = domain.BotStatePaused
	}
}

func (b *recordingBot) Resume(_ string) {
	if b.state == domain.BotStatePaused {
		b.state = domain.BotStateRunning
	}
}

func (b *recordingBot) Status(_ string) domain.BotStatus {
	return domain.BotStatus{State: b.state}
}

func (b *recordingBot) SubmitNow(_ string, _ bot.SubmitRequest) {}

func (b *recordingBot) SubmitSync(_ context.Context, _ string, _ bot.SubmitRequest) error {
	return nil
}

func (b *recordingBot) ApplyFromURL(_ context.Context, _, _, _ string, _ bool) (bot.ApplyFromURLResult, error) {
	return bot.ApplyFromURLResult{}, nil
}

func (b *recordingBot) InvalidateSeekBrowser(_ string)     {}
func (b *recordingBot) InvalidateLinkedInBrowser(_ string) {}

func noopFileToText(r io.Reader, _ string) (string, error) {
	_, _ = io.Copy(io.Discard, r)
	return "resume text", nil
}

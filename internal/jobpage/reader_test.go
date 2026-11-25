package jobpage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

type orchestrationPage struct {
	snapshot Snapshot
	known    *Result
	knownErr error
	readErr  error
	onRead   func(*Result)
	onAct    func()
	capture  chan<- string
	reads    atomic.Int32
	acts     atomic.Int32
	captures atomic.Int32
}

func (p *orchestrationPage) Known(ctx context.Context, _ Target, _ Limits) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if p.known != nil {
		return *p.known, nil
	}
	if p.knownErr != nil {
		return Result{}, p.knownErr
	}
	return Result{}, &ReadError{Code: FailureMissing}
}
func (p *orchestrationPage) Capture(ctx context.Context, _ Target, _ Limits) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if p.captures.Add(1) == 1 && p.capture != nil {
		p.capture <- p.snapshot.Target.JobID
	}
	return p.snapshot, nil
}
func (p *orchestrationPage) Read(ctx context.Context, target Target, snap Snapshot, plan Plan, limits Limits) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	p.reads.Add(1)
	if p.readErr != nil {
		return Result{}, p.readErr
	}
	if err := ValidatePlan(snap, plan, limits); err != nil {
		return Result{}, err
	}
	var texts []string
	for _, ref := range plan.DescriptionRefs {
		n, _ := snap.Node(ref)
		texts = append(texts, n.Text)
	}
	r := Result{Target: target, Identity: snap.Identity, Description: strings.Join(texts, "\n\n"), LayoutSignature: snap.LayoutSignature}
	if p.onRead != nil {
		p.onRead(&r)
	}
	return r, nil
}
func (p *orchestrationPage) Act(ctx context.Context, _ Target, _ Snapshot, _ Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.acts.Add(1)
	if p.onAct != nil {
		p.onAct()
	}
	return nil
}

type orchestrationModel struct {
	onPropose func(context.Context, Snapshot) (Plan, error)
	onVerify  func(context.Context, Snapshot, Plan) (Verification, error)
	proposes  int
	verifies  int
}

func (m *orchestrationModel) Propose(ctx context.Context, s Snapshot) (Plan, error) {
	m.proposes++
	if m.onPropose != nil {
		return m.onPropose(ctx, s)
	}
	return Plan{Version: SnapshotVersion, SnapshotID: s.ID, DescriptionRefs: []string{s.Candidates[0]}}, nil
}
func (m *orchestrationModel) Verify(ctx context.Context, s Snapshot, p Plan) (Verification, error) {
	m.verifies++
	if m.onVerify != nil {
		return m.onVerify(ctx, s, p)
	}
	v := Verification{SnapshotID: s.ID}
	for _, ref := range p.DescriptionRefs {
		v.Verdicts = append(v.Verdicts, Verdict{Ref: ref, IsDescription: true, Supported: true, Complete: true})
	}
	return v, nil
}
func (m *orchestrationModel) Metrics() Metrics {
	return Metrics{ProposalRequests: m.proposes, VerificationRequests: m.verifies, HTTPAttempts: m.proposes + m.verifies, CostKnown: true}
}

func readSnapshot(id, content string) Snapshot {
	s := fixtureSnapshot()
	s.ID = "current-epoch-" + id
	s.Target, s.Scope, _ = NormalizeTarget(Target{URL: "https://www.linkedin.com/jobs/view/" + id + "/", Title: "Engineer", Company: "Fixture", UserID: "reader-user"})
	s.Identity.JobID = id
	s.Candidates = []string{"n1"}
	for i := range s.Nodes {
		s.Nodes[i].OwnerJobID = id
	}
	s.Nodes[1].Text = strings.Repeat(content+" ", 6)
	s.Nodes[1].TextLength = utf8.RuneCountInString(s.Nodes[1].Text)
	s.Nodes[1].Attributes = map[string]string{"data-testid": "job-description", "data-job-id": id}
	s.LayoutSignature = StructuralSignature(s)
	return s
}
func enabledReaderConfig(context.Context) (Config, error) {
	return Config{Recovery: ModeOn, Reuse: ModeOn, Limits: DefaultLimits()}, nil
}

type changingReaderConfig struct {
	mu     sync.Mutex
	config Config
}

func (c *changingReaderConfig) get(context.Context) (Config, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.config, nil
}
func (c *changingReaderConfig) set(config Config) { c.mu.Lock(); c.config = config; c.mu.Unlock() }

func TestReaderKnownPagesAvoidCaptureAndModel(t *testing.T) {
	s := readSnapshot("123", "A complete supported job description with current responsibilities.")
	live := Result{Target: s.Target, Identity: s.Identity, Description: s.Nodes[1].Text}
	page := &orchestrationPage{snapshot: s, known: &live}
	reader := NewReader(nil, enabledReaderConfig, func(context.Context, Limits) (Model, error) {
		t.Fatal("known page must not create interpretation session")
		return nil, nil
	})
	r, err := reader.Read(context.Background(), page, s.Target)
	require.NoError(t, err)
	require.Equal(t, SourceDeterministic, r.Source)
	require.Equal(t, int32(0), page.captures.Load())
	require.Zero(t, r.Metrics.HTTPAttempts)
}

func TestReaderColdWarmExtractCurrentTextAndPersistStructure(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	var sessions atomic.Int32
	reader := NewReader(store, enabledReaderConfig, func(context.Context, Limits) (Model, error) { sessions.Add(1); return &orchestrationModel{}, nil })
	first := readSnapshot("123", "First employer expects service development and on call operations.")
	r, err := reader.Read(context.Background(), &orchestrationPage{snapshot: first}, first.Target)
	require.NoError(t, err)
	require.Equal(t, SourceModel, r.Source)
	require.NotEmpty(t, r.RecipeID)
	require.Equal(t, 2, r.Metrics.HTTPAttempts)
	second := readSnapshot("456", "Second employer expects secure platform automation and peer review.")
	require.Equal(t, first.LayoutSignature, second.LayoutSignature)
	warm, err := reader.Read(context.Background(), &orchestrationPage{snapshot: second}, second.Target)
	require.NoError(t, err)
	require.Equal(t, SourceRecipe, warm.Source)
	require.Equal(t, second.Nodes[1].Text, warm.Description)
	require.NotContains(t, warm.Description, "First employer")
	require.Equal(t, int32(1), sessions.Load())
	require.Zero(t, warm.Metrics.HTTPAttempts)
	require.Equal(t, 1, warm.Metrics.RecipeHits)
	require.Equal(t, r.RecipeID, warm.RecipeID)
	rows, err := store.ListRecipes(context.Background(), RecipeActive, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestReaderConcurrentColdMissesShareLearningWithoutSharingVacancyText(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	var sessions atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	reader := NewReader(store, enabledReaderConfig, func(context.Context, Limits) (Model, error) {
		if sessions.Add(1) == 1 {
			return &orchestrationModel{onPropose: func(ctx context.Context, s Snapshot) (Plan, error) {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return Plan{}, ctx.Err()
				}
				return Plan{Version: 1, SnapshotID: s.ID, DescriptionRefs: []string{"n1"}}, nil
			}}, nil
		}
		return &orchestrationModel{}, nil
	})
	const count = 8
	captured := make(chan string, count)
	type observed struct {
		result Result
		err    error
		text   string
	}
	results := make(chan observed, count)
	launch := func(i int) {
		s := readSnapshot(fmt.Sprint(1000+i), fmt.Sprintf("Employer %d requires unique service development and reliable operational practices.", i))
		go func() {
			r, e := reader.Read(context.Background(), &orchestrationPage{snapshot: s, capture: captured}, s.Target)
			results <- observed{r, e, s.Nodes[1].Text}
		}()
	}
	launch(0)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("cold leader did not start")
	}
	for i := 1; i < count; i++ {
		launch(i)
	}
	for i := 0; i < count; i++ {
		select {
		case <-captured:
		case <-time.After(3 * time.Second):
			t.Fatal("follower did not capture its own page")
		}
	}
	close(release)
	modelReads, warmReads := 0, 0
	for i := 0; i < count; i++ {
		select {
		case o := <-results:
			require.NoError(t, o.err)
			require.Equal(t, o.text, o.result.Description)
			if o.result.Source == SourceModel {
				modelReads++
			} else if o.result.Source == SourceRecipe {
				warmReads++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("coalesced read did not finish")
		}
	}
	require.Equal(t, 1, modelReads)
	require.Equal(t, count-1, warmReads)
	require.Equal(t, int32(1), sessions.Load())
	reader.mu.Lock()
	defer reader.mu.Unlock()
	require.Empty(t, reader.flights, "completed flight keys must not grow indefinitely")
}

func TestReaderCanceledFollowerDoesNotCancelLeaderOrLearnItsPage(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	var sessions atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	reader := NewReader(store, enabledReaderConfig, func(context.Context, Limits) (Model, error) {
		sessions.Add(1)
		return &orchestrationModel{onPropose: func(ctx context.Context, s Snapshot) (Plan, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return Plan{}, ctx.Err()
			}
			return Plan{Version: 1, SnapshotID: s.ID, DescriptionRefs: []string{"n1"}}, nil
		}}, nil
	})
	leader := readSnapshot("123", "Leader listing requires supported and complete service development.")
	finished := make(chan error, 1)
	go func() {
		_, err := reader.Read(context.Background(), &orchestrationPage{snapshot: leader}, leader.Target)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("leader did not start")
	}
	follower := readSnapshot("456", "Canceled follower has different current employer responsibilities.")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := reader.Read(ctx, &orchestrationPage{snapshot: follower}, follower.Target)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, int32(1), sessions.Load())
	close(release)
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("leader canceled with its follower")
	}
	rows, err := store.ListRecipes(context.Background(), RecipeActive, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestReaderHotRollbackStopsReuseAndRecoveryAtBoundaries(t *testing.T) {
	t.Run("reuse changes during live read", func(t *testing.T) {
		store, _ := recipeStoreFixture(t)
		s, p, live, v, _ := recipeFixture(t)
		s.Candidates = []string{"n1"}
		recipe, err := BuildVerifiedRecipe(context.Background(), s, p, live, v, DefaultLimits())
		require.NoError(t, err)
		_, err = store.SaveVerified(context.Background(), recipe)
		require.NoError(t, err)
		c := &changingReaderConfig{config: Config{Reuse: ModeOn, Recovery: ModeOff, Limits: DefaultLimits()}}
		page := &orchestrationPage{snapshot: s, onRead: func(*Result) { c.set(Config{Reuse: ModeOff, Recovery: ModeOff, Limits: DefaultLimits()}) }}
		reader := NewReader(store, c.get, func(context.Context, Limits) (Model, error) {
			t.Fatal("rollback must not start a model")
			return nil, nil
		})
		r, err := reader.Read(context.Background(), page, s.Target)
		require.Equal(t, FailureMissing, ErrorCode(err))
		require.Zero(t, r.Metrics.RecipeHits)
	})
	t.Run("recovery disabled after first action", func(t *testing.T) {
		s := readSnapshot("123", "Loading")
		s.Nodes[1].Text = "Loading"
		s.Nodes[1].TextLength = 7
		s.Loading = true
		c := &changingReaderConfig{config: Config{Recovery: ModeOn, Reuse: ModeOff, Limits: DefaultLimits()}}
		page := &orchestrationPage{snapshot: s, onAct: func() { c.set(Config{Recovery: ModeOff, Reuse: ModeOff, Limits: DefaultLimits()}) }}
		reader := NewReader(nil, c.get, func(context.Context, Limits) (Model, error) {
			t.Fatal("rollback must not start a model")
			return nil, nil
		})
		r, err := reader.Read(context.Background(), page, s.Target)
		require.Equal(t, FailureMissing, ErrorCode(err))
		require.Equal(t, int32(1), page.acts.Load())
		require.Equal(t, 1, r.Metrics.RecoveryActions, "a rollout guard refusal is not a browser action")
	})
	t.Run("recovery disabled during proposal", func(t *testing.T) {
		s := readSnapshot("123", "Complete current job duties supported in the candidate region.")
		c := &changingReaderConfig{config: Config{Recovery: ModeOn, Reuse: ModeOff, Limits: DefaultLimits()}}
		page := &orchestrationPage{snapshot: s}
		model := &orchestrationModel{onPropose: func(context.Context, Snapshot) (Plan, error) {
			c.set(Config{Recovery: ModeOff, Reuse: ModeOff, Limits: DefaultLimits()})
			return Plan{Version: 1, SnapshotID: s.ID, DescriptionRefs: []string{"n1"}, Actions: []Action{{Kind: ActionWait, Amount: 1}}}, nil
		}}
		reader := NewReader(nil, c.get, func(context.Context, Limits) (Model, error) { return model, nil })
		_, err := reader.Read(context.Background(), page, s.Target)
		require.Equal(t, FailureMissing, ErrorCode(err))
		require.Zero(t, page.acts.Load())
		require.Zero(t, model.verifies)
	})
}

type failingRecipeStore struct {
	RecipeStore
	saveErr error
	saves   int
}

func (s *failingRecipeStore) SaveVerified(ctx context.Context, r Recipe) (Recipe, error) {
	s.saves++
	if s.saveErr != nil {
		return Recipe{}, s.saveErr
	}
	return s.RecipeStore.SaveVerified(ctx, r)
}

func TestReaderRefusesUnverifiedAndFailedLiveProofWithoutPromotion(t *testing.T) {
	for _, kind := range []string{"verification", "foreign live identity", "changed live text", "partial observed content", "canceled live read"} {
		t.Run(kind, func(t *testing.T) {
			store, _ := recipeStoreFixture(t)
			counting := &failingRecipeStore{RecipeStore: store}
			s := readSnapshot("123", "A supported job requires complete current technical responsibilities.")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			page := &orchestrationPage{snapshot: s}
			model := &orchestrationModel{}
			switch kind {
			case "verification":
				model.onVerify = func(_ context.Context, s Snapshot, _ Plan) (Verification, error) {
					return Verification{SnapshotID: s.ID, Verdicts: []Verdict{{Ref: "n1", IsDescription: true, Supported: true, Complete: false}}}, nil
				}
			case "foreign live identity":
				page.onRead = func(r *Result) { r.Identity.JobID = "999" }
			case "changed live text":
				page.onRead = func(r *Result) { r.Description = strings.Repeat("Unsupported employer replacement information. ", 6) }
			case "partial observed content":
				s.Nodes[1].TextLength += 20
				page.snapshot = s
			case "canceled live read":
				page.onRead = func(*Result) { cancel() }
			}
			reader := NewReader(counting, enabledReaderConfig, func(context.Context, Limits) (Model, error) { return model, nil })
			_, err := reader.Read(ctx, page, s.Target)
			require.Error(t, err)
			require.Zero(t, counting.saves)
			rows, e := store.ListRecipes(context.Background(), RecipeActive, 100)
			require.NoError(t, e)
			require.Empty(t, rows)
		})
	}
}

func TestReaderVerifiedNonReusableReadSucceedsWithoutPromotion(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	counting := &failingRecipeStore{RecipeStore: store}
	s := readSnapshot("123", "A current job description with complete supported role requirements.")
	duplicate := s.Nodes[1]
	duplicate.Ref = "n2"
	duplicate.Text = strings.Repeat("Unrelated but owned employer information. ", 5)
	duplicate.TextLength = utf8.RuneCountInString(duplicate.Text)
	s.Nodes = append(s.Nodes, duplicate)
	s.Candidates = append(s.Candidates, "n2")
	s.LayoutSignature = StructuralSignature(s)
	reader := NewReader(counting, enabledReaderConfig, func(context.Context, Limits) (Model, error) { return &orchestrationModel{}, nil })
	r, err := reader.Read(context.Background(), &orchestrationPage{snapshot: s}, s.Target)
	require.NoError(t, err)
	require.Equal(t, SourceModel, r.Source)
	require.Equal(t, s.Nodes[1].Text, r.Description)
	require.Empty(t, r.RecipeID)
	require.Zero(t, counting.saves)
}

func TestReaderRecipePersistenceFailureIsNotReportedAsSuccess(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	fault := &failingRecipeStore{RecipeStore: store, saveErr: errors.New("fixture database write refused")}
	s := readSnapshot("123", "A supported full listing requires reliable platform service development.")
	reader := NewReader(fault, enabledReaderConfig, func(context.Context, Limits) (Model, error) { return &orchestrationModel{}, nil })
	_, err := reader.Read(context.Background(), &orchestrationPage{snapshot: s}, s.Target)
	require.Equal(t, FailurePersistence, ErrorCode(err))
	require.Equal(t, 1, fault.saves)
}

func TestReaderNoStoreStillRequiresExactVerifiedLiveSelection(t *testing.T) {
	s := readSnapshot("123", "Supported current requirements include secure service ownership and delivery.")
	page := &orchestrationPage{snapshot: s, onRead: func(r *Result) { r.Description = strings.Repeat("Unsupported replacement vacancy text. ", 6) }}
	reader := NewReader(nil, enabledReaderConfig, func(context.Context, Limits) (Model, error) { return &orchestrationModel{}, nil })
	_, err := reader.Read(context.Background(), page, s.Target)
	require.Equal(t, FailureVerification, ErrorCode(err))
}

func TestReaderTerminalFailuresDoNotReachModelOrRecovery(t *testing.T) {
	for _, code := range []FailureCode{FailureIdentity, FailureDrift, FailureOrigin, FailureAuth, FailureExpired, FailureBrowser} {
		t.Run(string(code), func(t *testing.T) {
			s := readSnapshot("123", "A supported description with complete role information and requirements.")
			page := &orchestrationPage{snapshot: s, knownErr: &ReadError{Code: code}}
			reader := NewReader(nil, enabledReaderConfig, func(context.Context, Limits) (Model, error) {
				t.Fatal("terminal failures must not reach interpretation")
				return nil, nil
			})
			_, err := reader.Read(context.Background(), page, s.Target)
			require.Equal(t, code, ErrorCode(err))
			require.Zero(t, page.captures.Load())
			require.Zero(t, page.acts.Load())
		})
	}
}

func TestReaderChangedAssumptionsInvalidateRecipeAndDoNotPromoteAmbiguity(t *testing.T) {
	store, _ := recipeStoreFixture(t)
	var sessions atomic.Int32
	reader := NewReader(store, enabledReaderConfig, func(context.Context, Limits) (Model, error) {
		sessions.Add(1)
		return &orchestrationModel{}, nil
	})
	s := readSnapshot("123", "Current supported role responsibilities include safe service development.")
	cold, err := reader.Read(context.Background(), &orchestrationPage{snapshot: s}, s.Target)
	require.NoError(t, err)
	changed := readSnapshot("456", "Changed employer role responsibilities include reliable platform operations.")
	duplicate := changed.Nodes[1]
	duplicate.Ref = "n2"
	duplicate.Text = strings.Repeat("Another employer content section within the current listing. ", 4)
	duplicate.TextLength = utf8.RuneCountInString(duplicate.Text)
	changed.Nodes = append(changed.Nodes, duplicate)
	changed.Candidates = append(changed.Candidates, "n2")
	changed.LayoutSignature = StructuralSignature(changed)
	require.Equal(t, s.LayoutSignature, changed.LayoutSignature, "duplicate shape does not justify selecting by ordinal")
	r, err := reader.Read(context.Background(), &orchestrationPage{snapshot: changed}, changed.Target)
	require.NoError(t, err)
	require.Equal(t, SourceModel, r.Source)
	require.Empty(t, r.RecipeID)
	require.Equal(t, changed.Nodes[1].Text, r.Description)
	require.Equal(t, int32(2), sessions.Load())
	retained, err := store.GetRecipe(context.Background(), cold.RecipeID)
	require.NoError(t, err)
	require.Equal(t, RecipeInvalidated, retained.State)
	require.Equal(t, "ambiguous", retained.StateReason)
}

func TestReaderLogicalRequestBudgetIncludesIndependentVerification(t *testing.T) {
	s := readSnapshot("123", "Supported complete role requirements include service design and delivery.")
	limits := DefaultLimits()
	limits.MaxRequests = 1
	config := func(context.Context) (Config, error) {
		return Config{Recovery: ModeOn, Reuse: ModeOff, Limits: limits}, nil
	}
	model := &orchestrationModel{}
	page := &orchestrationPage{snapshot: s}
	reader := NewReader(nil, config, func(context.Context, Limits) (Model, error) { return model, nil })
	r, err := reader.Read(context.Background(), page, s.Target)
	require.Equal(t, FailureBudget, ErrorCode(err))
	require.Equal(t, 1, model.proposes)
	require.Zero(t, model.verifies)
	require.Zero(t, page.reads.Load())
	require.Equal(t, 1, r.Metrics.HTTPAttempts)
}

func TestReaderProviderFailuresRemainDistinctAndPreserveTypedFailures(t *testing.T) {
	for _, stage := range []string{"factory", "proposal", "verification", "nil model"} {
		t.Run(stage, func(t *testing.T) {
			s := readSnapshot("123", "A complete supported role requires production service operations.")
			failure := errors.New("configured provider failed in fixture")
			model := &orchestrationModel{}
			factory := func(context.Context, Limits) (Model, error) { return model, nil }
			switch stage {
			case "factory":
				factory = func(context.Context, Limits) (Model, error) { return nil, failure }
			case "proposal":
				model.onPropose = func(context.Context, Snapshot) (Plan, error) { return Plan{}, failure }
			case "verification":
				model.onVerify = func(context.Context, Snapshot, Plan) (Verification, error) { return Verification{}, failure }
			case "nil model":
				factory = func(context.Context, Limits) (Model, error) { return nil, nil }
			}
			reader := NewReader(nil, enabledReaderConfig, factory)
			_, err := reader.Read(context.Background(), &orchestrationPage{snapshot: s}, s.Target)
			require.Equal(t, FailureProvider, ErrorCode(err))
			if stage != "nil model" {
				require.ErrorIs(t, err, failure)
			}
		})
	}
	s := readSnapshot("123", "A current supported description includes complete technical duties.")
	failure := &ReadError{Code: FailurePlan}
	model := &orchestrationModel{onPropose: func(context.Context, Snapshot) (Plan, error) { return Plan{}, failure }}
	reader := NewReader(nil, enabledReaderConfig, func(context.Context, Limits) (Model, error) { return model, nil })
	_, err := reader.Read(context.Background(), &orchestrationPage{snapshot: s}, s.Target)
	require.Same(t, failure, err)
}

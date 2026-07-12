package workers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/models"
	"github.com/spozhydaiev/lead-log/internal/services"
)

type fakeStore struct {
	notes                []store.NoteForEnrichment
	scheduled, permanent int
}

func (f *fakeStore) ClaimNextNotesForEnrichment(ctx context.Context, limit, maxAttempts int, staleAfter time.Duration) ([]store.NoteForEnrichment, error) {
	if len(f.notes) > limit {
		return f.notes[:limit], nil
	}
	return f.notes, nil
}
func (f *fakeStore) ScheduleNoteEnrichmentRetry(ctx context.Context, userID, noteID int64, startedAt time.Time, nextAt time.Time, cause error) error {
	f.scheduled++
	return nil
}
func (f *fakeStore) MarkNoteEnrichmentPermanentlyFailed(ctx context.Context, userID, noteID int64, startedAt time.Time, cause error) error {
	f.permanent++
	return nil
}

type fakeSvc struct {
	mu                           sync.Mutex
	calls, inFlight, maxInFlight int
	fail                         map[int64]bool
}

func (f *fakeSvc) EnrichClaimedNote(ctx context.Context, claim store.NoteForEnrichment) (services.NoteEnrichmentResult, error) {
	f.mu.Lock()
	f.calls++
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	f.mu.Unlock()
	time.Sleep(10 * time.Millisecond)
	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	if f.fail != nil && f.fail[claim.ID] {
		return services.NoteEnrichmentResult{}, errors.New("llm timeout")
	}
	return services.NoteEnrichmentResult{NoteID: claim.ID, Parsed: models.ParsedNote{Summary: "ok"}, Attempt: claim.ProcessingAttempts, Model: "mock", PromptVersion: services.NoteEnrichmentPromptVersion}, nil
}

func TestWorkerLimitsConcurrencyAndContinuesAfterFailure(t *testing.T) {
	st := &fakeStore{notes: []store.NoteForEnrichment{{ID: 1, UserID: 1, ProcessingAttempts: 1, ProcessingStartedAt: time.Now()}, {ID: 2, UserID: 1, ProcessingAttempts: 1, ProcessingStartedAt: time.Now()}, {ID: 3, UserID: 1, ProcessingAttempts: 1, ProcessingStartedAt: time.Now()}}}
	sv := &fakeSvc{fail: map[int64]bool{2: true}}
	w := NewNoteEnrichment(st, sv, NoteEnrichmentConfig{BatchSize: 3, Concurrency: 2, MaxAttempts: 3, PollInterval: time.Hour}, nil)
	w.poll(context.Background())
	if sv.calls != 3 {
		t.Fatalf("calls=%d, want 3", sv.calls)
	}
	if sv.maxInFlight > 2 {
		t.Fatalf("max in flight=%d, want <=2", sv.maxInFlight)
	}
	if st.scheduled != 1 {
		t.Fatalf("scheduled=%d, want 1", st.scheduled)
	}
}

func TestWorkerMarksPermanentAtMaxAttempts(t *testing.T) {
	st := &fakeStore{notes: []store.NoteForEnrichment{{ID: 1, UserID: 1, ProcessingAttempts: 3, ProcessingStartedAt: time.Now()}}}
	sv := &fakeSvc{fail: map[int64]bool{1: true}}
	w := NewNoteEnrichment(st, sv, NoteEnrichmentConfig{BatchSize: 1, Concurrency: 1, MaxAttempts: 3, PollInterval: time.Hour}, nil)
	w.poll(context.Background())
	if st.permanent != 1 || st.scheduled != 0 {
		t.Fatalf("permanent/scheduled=%d/%d, want 1/0", st.permanent, st.scheduled)
	}
}

package workers

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/logging"
	"github.com/spozhydaiev/lead-log/internal/services"
)

type NoteEnrichmentStore interface {
	ClaimNextNotesForEnrichment(ctx context.Context, limit, maxAttempts int, staleAfter time.Duration) ([]store.NoteForEnrichment, error)
	ScheduleNoteEnrichmentRetry(ctx context.Context, userID, noteID int64, startedAt time.Time, nextAt time.Time, cause error) error
	MarkNoteEnrichmentPermanentlyFailed(ctx context.Context, userID, noteID int64, startedAt time.Time, cause error) error
}

type NoteEnrichmentService interface {
	EnrichClaimedNote(ctx context.Context, claim store.NoteForEnrichment) (services.NoteEnrichmentResult, error)
}

type NoteEnrichmentConfig struct {
	PollInterval time.Duration
	BatchSize    int
	Concurrency  int
	MaxAttempts  int
	StaleTimeout time.Duration
}

type NoteEnrichment struct {
	store  NoteEnrichmentStore
	svc    NoteEnrichmentService
	cfg    NoteEnrichmentConfig
	logger *slog.Logger
}

func NewNoteEnrichment(st NoteEnrichmentStore, svc NoteEnrichmentService, cfg NoteEnrichmentConfig, logger *slog.Logger) *NoteEnrichment {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.StaleTimeout <= 0 {
		cfg.StaleTimeout = services.DefaultNoteEnrichmentStaleTimeout
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &NoteEnrichment{store: st, svc: svc, cfg: cfg, logger: logger}
}

func (w *NoteEnrichment) Run(ctx context.Context) {
	w.logger.Info("note enrichment worker started", "operation", "worker.note_enrichment.started", "poll_interval", w.cfg.PollInterval.String(), "batch_size", w.cfg.BatchSize, "concurrency", w.cfg.Concurrency, "max_attempts", w.cfg.MaxAttempts, "stale_timeout", w.cfg.StaleTimeout.String())
	defer w.logger.Info("note enrichment worker stopped", "operation", "worker.note_enrichment.stopped")
	w.poll(ctx)
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *NoteEnrichment) poll(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	notes, err := w.store.ClaimNextNotesForEnrichment(ctx, w.cfg.BatchSize, w.cfg.MaxAttempts, w.cfg.StaleTimeout)
	if err != nil {
		w.logger.Error("note enrichment poll failed", logging.WithSafeError([]any{"operation", "worker.note_enrichment.poll_failed"}, err)...)
		return
	}
	if len(notes) == 0 {
		return
	}
	w.logger.Info("note enrichment batch claimed", "operation", "worker.note_enrichment.batch", "claimed", len(notes), "batch_size", w.cfg.BatchSize)
	sem := make(chan struct{}, w.cfg.Concurrency)
	var wg sync.WaitGroup
	for _, note := range notes {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		note := note
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("note enrichment job panic", "operation", "worker.note_enrichment.panic", "operation_id", logging.OperationID(ctx), "error_class", "panic")
				}
			}()
			w.process(logging.ContextWithOperationID(ctx, logging.NewOperationID()), note)
		}()
	}
	wg.Wait()
}

func (w *NoteEnrichment) process(ctx context.Context, note store.NoteForEnrichment) {
	started := time.Now()
	w.logger.Info("note enrichment job claimed", "operation", "worker.note_enrichment.claimed", "operation_id", logging.OperationID(ctx), "attempt", note.ProcessingAttempts, "stale_reclaimed", note.StaleReclaimed)
	result, err := w.svc.EnrichClaimedNote(ctx, note)
	if err == nil {
		w.logger.Info("note enrichment job processed", "operation", "worker.note_enrichment.processed", "operation_id", logging.OperationID(ctx), "attempt", result.Attempt, "model", result.Model, "prompt_version", result.PromptVersion, "duration_ms", time.Since(started).Milliseconds())
		return
	}
	if note.ProcessingAttempts >= w.cfg.MaxAttempts {
		_ = w.store.MarkNoteEnrichmentPermanentlyFailed(context.Background(), note.UserID, note.ID, note.ProcessingStartedAt, err)
		w.logger.Error("note enrichment permanently failed", logging.WithSafeError([]any{"operation", "worker.note_enrichment.permanently_failed", "operation_id", logging.OperationID(ctx), "attempt", note.ProcessingAttempts, "max_attempts", w.cfg.MaxAttempts, "duration_ms", time.Since(started).Milliseconds()}, err)...)
		return
	}
	next := time.Now().Add(backoff(note.ProcessingAttempts))
	_ = w.store.ScheduleNoteEnrichmentRetry(context.Background(), note.UserID, note.ID, note.ProcessingStartedAt, next, err)
	w.logger.Warn("note enrichment retry scheduled", logging.WithSafeError([]any{"operation", "worker.note_enrichment.retry_scheduled", "operation_id", logging.OperationID(ctx), "attempt", note.ProcessingAttempts, "retry_after_seconds", int(backoff(note.ProcessingAttempts).Seconds()), "duration_ms", time.Since(started).Milliseconds()}, err)...)
}

func backoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	default:
		return 30 * time.Minute
	}
}

package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spozhydaiev/lead-log/internal/adapters/store"
	"github.com/spozhydaiev/lead-log/internal/logging"

	"github.com/spozhydaiev/lead-log/internal/models"
)

const (
	DefaultRetrievalLimit = 20
	MaxRetrievalLimit     = 100
)

var ErrInvalidRetrievalQuery = errors.New("invalid retrieval query")

func (s *Service) Retrieve(ctx context.Context, q models.RetrievalQuery) ([]models.RetrievalItem, error) {
	started := time.Now()
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultRetrievalLimit
	}
	if limit > MaxRetrievalLimit {
		limit = MaxRetrievalLimit
	}
	kinds, err := retrievalKinds(q.Kinds)
	if err != nil {
		s.logger.Warn("retrieval validation failed", logging.WithSafeError([]any{"operation", "retrieval.validate", "operation_id", logging.OperationID(ctx)}, err)...)
		return nil, err
	}
	if q.UserID <= 0 {
		return nil, fmt.Errorf("%w: user_id is required", ErrInvalidRetrievalQuery)
	}
	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		return nil, fmt.Errorf("%w: from must be before to", ErrInvalidRetrievalQuery)
	}
	if isEmptyRetrieval(q) {
		s.logger.Warn("retrieval validation failed", "operation", "retrieval.validate", "operation_id", logging.OperationID(ctx))
		return nil, fmt.Errorf("%w: at least one filter is required", ErrInvalidRetrievalQuery)
	}

	f := store.RetrievalFilters{Text: strings.TrimSpace(q.Text), From: q.From, To: q.To, EntityType: strings.ToLower(strings.TrimSpace(q.EntityType)), ActionStatuses: q.ActionStatuses, PeopleNoteTypes: q.PeopleNoteTypes, DecisionStatuses: q.DecisionStatuses, DecisionTopic: strings.TrimSpace(q.DecisionTopic), Limit: min(limit, store.RetrievalPerSourceLimit)}
	if q.PersonID != nil {
		f.PersonID = q.PersonID
	} else if strings.TrimSpace(q.PersonName) != "" {
		p, err := s.store.ResolvePerson(ctx, q.UserID, q.PersonName)
		if err != nil {
			return nil, fmt.Errorf("resolve person: %w", err)
		}
		if !p.Found {
			return []models.RetrievalItem{}, nil
		}
		f.PersonID = &p.ID
	}
	if f.EntityType != "" || strings.TrimSpace(q.EntityValue) != "" {
		norm, err := normalizeRetrievalEntity(f.EntityType, q.EntityValue)
		if err != nil {
			return nil, err
		}
		f.EntityValue = norm
	}
	if err := validateEnums(f); err != nil {
		return nil, err
	}

	log := s.logger.With("operation", "retrieval", "operation_id", logging.OperationID(ctx), "limit", limit, "kinds", kindStrings(kinds), "has_text", f.Text != "", "has_person", f.PersonID != nil, "has_entity", f.EntityType != "", "from", timePtrString(q.From), "to", timePtrString(q.To))
	log.Info("retrieval started")
	var all []models.RetrievalItem
	counts := map[string]int{}
	call := func(kind models.RetrievalKind, fn func(context.Context, int64, store.RetrievalFilters) ([]models.RetrievalItem, error)) error {
		items, err := fn(ctx, q.UserID, f)
		if err != nil {
			log.Error("retrieval store failure", logging.WithSafeError([]any{"kind", kind}, err)...)
			return err
		}
		counts[string(kind)] = len(items)
		all = append(all, items...)
		return nil
	}
	for _, k := range kinds {
		switch k {
		case models.RetrievalKindNote:
			if err := call(k, s.store.SearchNotes); err != nil {
				return nil, err
			}
		case models.RetrievalKindEntityMention:
			if f.EntityType != "" && f.EntityValue != "" {
				if err := call(k, s.store.SearchEntityMentions); err != nil {
					return nil, err
				}
			}
		case models.RetrievalKindAction:
			if err := call(k, s.store.SearchActions); err != nil {
				return nil, err
			}
		case models.RetrievalKindPeopleNote:
			if err := call(k, s.store.SearchPeopleNotes); err != nil {
				return nil, err
			}
		case models.RetrievalKindDecision:
			if err := call(k, s.store.SearchDecisions); err != nil {
				return nil, err
			}
		}
	}
	seen := map[string]bool{}
	dedup := all[:0]
	for _, it := range all {
		key := fmt.Sprintf("%s:%d", it.Kind, it.RecordID)
		if seen[key] {
			continue
		}
		seen[key] = true
		dedup = append(dedup, it)
	}
	sort.SliceStable(dedup, func(i, j int) bool {
		if dedup[i].Score == dedup[j].Score {
			return dedup[i].CreatedAt.After(dedup[j].CreatedAt)
		}
		return dedup[i].Score > dedup[j].Score
	})
	if len(dedup) > limit {
		dedup = dedup[:limit]
	}
	log.Info("retrieval completed", "candidate_counts", counts, "result_count", len(dedup), "duration_ms", time.Since(started).Milliseconds())
	if dedup == nil {
		return []models.RetrievalItem{}, nil
	}
	return dedup, nil
}

func retrievalKinds(in []models.RetrievalKind) ([]models.RetrievalKind, error) {
	if len(in) == 0 {
		return []models.RetrievalKind{models.RetrievalKindNote, models.RetrievalKindAction, models.RetrievalKindPeopleNote, models.RetrievalKindDecision, models.RetrievalKindEntityMention}, nil
	}
	seen := map[models.RetrievalKind]bool{}
	var out []models.RetrievalKind
	for _, k := range in {
		switch k {
		case models.RetrievalKindNote, models.RetrievalKindAction, models.RetrievalKindPeopleNote, models.RetrievalKindDecision, models.RetrievalKindEntityMention:
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		default:
			return nil, fmt.Errorf("%w: unsupported kind %s", ErrInvalidRetrievalQuery, k)
		}
	}
	return out, nil
}
func isEmptyRetrieval(q models.RetrievalQuery) bool {
	return strings.TrimSpace(q.Text) == "" && q.From == nil && q.To == nil && q.PersonID == nil && strings.TrimSpace(q.PersonName) == "" && strings.TrimSpace(q.EntityValue) == "" && len(q.ActionStatuses) == 0 && len(q.PeopleNoteTypes) == 0 && len(q.DecisionStatuses) == 0 && strings.TrimSpace(q.DecisionTopic) == ""
}
func normalizeRetrievalEntity(t, v string) (string, error) {
	if !models.IsAllowedEntityType(t) {
		return "", fmt.Errorf("%w: invalid entity type", ErrInvalidRetrievalQuery)
	}
	if t == models.EntityTypeTicket {
		n, ok := models.NormalizeTicketKey(v)
		if !ok {
			return "", fmt.Errorf("%w: invalid ticket", ErrInvalidRetrievalQuery)
		}
		return n, nil
	}
	n := strings.ToLower(models.NormalizeSpace(v))
	if n == "" {
		return "", fmt.Errorf("%w: empty entity", ErrInvalidRetrievalQuery)
	}
	return n, nil
}
func validateEnums(f store.RetrievalFilters) error {
	for _, s := range f.ActionStatuses {
		if s != "open" && s != "done" {
			return fmt.Errorf("%w: invalid action status", ErrInvalidRetrievalQuery)
		}
	}
	validPN := map[string]bool{"positive_signal": true, "concern": true, "growth_topic": true, "context": true, "follow_up_needed": true, "commitment": true, "decision": true, "risk": true, "blocker": true, "review_evidence": true, "feedback": true}
	for _, t := range f.PeopleNoteTypes {
		if !validPN[t] {
			return fmt.Errorf("%w: invalid people note type", ErrInvalidRetrievalQuery)
		}
	}
	for _, s := range f.DecisionStatuses {
		if s != "active" && s != "superseded" && s != "reversed" {
			return fmt.Errorf("%w: invalid decision status", ErrInvalidRetrievalQuery)
		}
	}
	return nil
}
func kindStrings(k []models.RetrievalKind) []string {
	out := make([]string, len(k))
	for i, v := range k {
		out[i] = string(v)
	}
	return out
}
func timePtrString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

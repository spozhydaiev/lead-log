package services

import (
	"fmt"
	"strings"

	"github.com/spozhydaiev/lead-log/internal/models"
)

func FormatDailyDigest(d models.DailyDigest) string {
	var b strings.Builder
	writeSection := func(title string) { b.WriteString(title + "\n") }
	writeRefs := func(ids []int64) string {
		if len(ids) == 0 {
			return ""
		}
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			if id > 0 {
				parts = append(parts, fmt.Sprintf("#%d", id))
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return " (джерела: " + strings.Join(parts, ", ") + ")"
	}

	if strings.TrimSpace(d.ShortSummary) != "" {
		writeSection("Коротко")
		b.WriteString(strings.TrimSpace(d.ShortSummary) + "\n\n")
	}
	if len(d.OpenLoops) > 0 {
		writeSection("Open loops")
		for _, item := range d.OpenLoops {
			title := strings.TrimSpace(item.Title)
			if title == "" {
				continue
			}
			b.WriteString("- " + title)
			if item.Owner != nil && strings.TrimSpace(*item.Owner) != "" {
				b.WriteString(" — " + strings.TrimSpace(*item.Owner))
			}
			if item.DueHint != nil && strings.TrimSpace(*item.DueHint) != "" {
				b.WriteString("; " + strings.TrimSpace(*item.DueHint))
			}
			b.WriteString(writeRefs(item.SourceNoteIDs) + "\n")
		}
		b.WriteString("\n")
	}
	if len(d.TicketCandidates) > 0 {
		writeSection("Ticket candidates")
		for _, item := range d.TicketCandidates {
			title := strings.TrimSpace(item.Title)
			if title == "" {
				continue
			}
			b.WriteString("- " + title)
			if item.Owner != nil && strings.TrimSpace(*item.Owner) != "" {
				b.WriteString(" — " + strings.TrimSpace(*item.Owner))
			}
			if strings.TrimSpace(item.Context) != "" {
				b.WriteString(": " + strings.TrimSpace(item.Context))
			}
			b.WriteString(writeRefs(item.SourceNoteIDs) + "\n")
		}
		b.WriteString("\n")
	}
	if len(d.PeopleHighlights) > 0 {
		writeSection("People highlights")
		for _, item := range d.PeopleHighlights {
			if strings.TrimSpace(item.PersonName) == "" || strings.TrimSpace(item.Text) == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("- %s: %s / %s — %s%s\n", strings.TrimSpace(item.PersonName), item.Type, item.Theme, strings.TrimSpace(item.Text), writeRefs(item.SourceNoteIDs)))
		}
		b.WriteString("\n")
	}
	writeTextItems(&b, "Decisions / agreements", d.Decisions, writeRefs)
	writeTextItems(&b, "Suggested next steps", d.SuggestedNextSteps, writeRefs)
	writeTextItems(&b, "Unclear items", d.UnclearItems, writeRefs)
	return strings.TrimSpace(b.String())
}

func writeTextItems(b *strings.Builder, title string, items []models.DailyTextItem, refs func([]int64) string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(title + "\n")
	for _, item := range items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		b.WriteString("- " + text + refs(item.SourceNoteIDs) + "\n")
	}
	b.WriteString("\n")
}

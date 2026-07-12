package models

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	MaxDecisionsPerNote      = 20
	MaxEntityMentionsPerNote = 40
	MaxDecisionTextLength    = 1000
	MaxEntityValueLength     = 200
	MaxEntityContextLength   = 500
	MaxDecisionTopicLength   = 120
)

var ticketKeyRe = regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9]+-[0-9]+)\b`)

func NormalizeSpace(s string) string { return strings.Join(strings.Fields(strings.TrimSpace(s)), " ") }

func NormalizeDecisionText(s string) string { return strings.ToLower(NormalizeSpace(s)) }

func NormalizeTicketKey(s string) (string, bool) {
	v := NormalizeSpace(s)
	if !ticketKeyRe.MatchString(v) || ticketKeyRe.FindString(v) != v {
		return "", false
	}
	return strings.ToUpper(v), true
}

func IsAllowedEntityType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case EntityTypeTicket, EntityTypeProject, EntityTypeService, EntityTypeComponent, EntityTypeRepository, EntityTypeDocument, EntityTypeOther:
		return true
	default:
		return false
	}
}

func NormalizeEntityMention(m EntityMention) (EntityMentionRecord, bool) {
	typ := strings.ToLower(strings.TrimSpace(m.Type))
	if !IsAllowedEntityType(typ) {
		return EntityMentionRecord{}, false
	}
	raw := firstNonBlank(m.RawValue, m.Value, m.DisplayValue)
	raw = NormalizeSpace(raw)
	if raw == "" {
		return EntityMentionRecord{}, false
	}
	if len(raw) > MaxEntityValueLength {
		raw = raw[:MaxEntityValueLength]
	}
	display := NormalizeSpace(firstNonBlank(m.DisplayValue, m.Value, raw))
	if len(display) > MaxEntityValueLength {
		display = display[:MaxEntityValueLength]
	}
	var normalized string
	if typ == EntityTypeTicket {
		var ok bool
		normalized, ok = NormalizeTicketKey(raw)
		if !ok {
			return EntityMentionRecord{}, false
		}
		display = normalized
	} else {
		normalized = strings.ToLower(NormalizeSpace(raw))
		if normalized == "" {
			return EntityMentionRecord{}, false
		}
	}
	ctx := NormalizeSpace(m.Context)
	if len(ctx) > MaxEntityContextLength {
		ctx = ctx[:MaxEntityContextLength]
	}
	return EntityMentionRecord{Type: typ, RawValue: raw, NormalizedValue: normalized, DisplayValue: display, Context: ctx}, true
}

func NormalizeDecision(d ParsedDecision) (ParsedDecision, bool) {
	text := NormalizeSpace(d.Text)
	if text == "" {
		return ParsedDecision{}, false
	}
	if len(text) > MaxDecisionTextLength {
		text = text[:MaxDecisionTextLength]
	}
	topic := NormalizeSpace(d.Topic)
	if len(topic) > MaxDecisionTopicLength {
		topic = topic[:MaxDecisionTopicLength]
	}
	return ParsedDecision{Text: text, LinkedPersonName: NormalizeSpace(d.LinkedPersonName), Topic: topic}, true
}

func AddTicketFallbackMentions(parsed ParsedNote, raw string) ParsedNote {
	for _, m := range ticketKeyRe.FindAllString(raw, -1) {
		// Be conservative: require at least one letter in project key and valid whole key.
		keyPart := strings.SplitN(m, "-", 2)[0]
		hasLetter := false
		for _, r := range keyPart {
			if unicode.IsLetter(r) {
				hasLetter = true
				break
			}
		}
		if !hasLetter {
			continue
		}
		if norm, ok := NormalizeTicketKey(m); ok {
			parsed.EntityMentions = append(parsed.EntityMentions, EntityMention{Type: EntityTypeTicket, Value: norm, RawValue: m, DisplayValue: norm})
		}
	}
	return parsed
}

func NormalizeEntityMentionsForNote(in []EntityMention) (out []EntityMentionRecord, skipped int) {
	seen := map[string]bool{}
	for _, m := range in {
		if len(out) >= MaxEntityMentionsPerNote {
			skipped++
			continue
		}
		rec, ok := NormalizeEntityMention(m)
		if !ok {
			skipped++
			continue
		}
		key := rec.Type + "\x00" + rec.NormalizedValue
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rec)
	}
	return out, skipped
}

func NormalizeDecisionsForNote(in []ParsedDecision) (out []ParsedDecision, skipped int) {
	for _, d := range in {
		if len(out) >= MaxDecisionsPerNote {
			skipped++
			continue
		}
		n, ok := NormalizeDecision(d)
		if !ok {
			skipped++
			continue
		}
		out = append(out, n)
	}
	return out, skipped
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

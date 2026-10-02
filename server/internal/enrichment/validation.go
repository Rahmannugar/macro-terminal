package enrichment

import (
	"strings"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
)

const (
	maxEntities      = 10
	maxTopics        = 10
	maxConcepts      = 10
	maxTermLength    = 100
	maxReportedDrops = 10
)

type validationReport struct {
	UnknownEntities []string
	Dropped         int
}

func sanitize(input ai.Enrichment, known map[string]struct{}) (ai.Enrichment, validationReport) {
	report := validationReport{}
	clean := ai.Enrichment{
		Entities: make([]string, 0, len(input.Entities)),
		Topics:   make([]string, 0, len(input.Topics)),
		Concepts: make([]string, 0, len(input.Concepts)),
	}

	seenEntities := make(map[string]struct{}, len(input.Entities))
	for _, value := range input.Entities {
		code := strings.ToUpper(strings.TrimSpace(value))
		if code == "" || len(code) > maxTermLength {
			report.Dropped++
			continue
		}
		if _, ok := known[code]; !ok {
			report.Dropped++
			if len(report.UnknownEntities) < maxReportedDrops {
				report.UnknownEntities = append(report.UnknownEntities, code)
			}
			continue
		}
		if _, duplicate := seenEntities[code]; duplicate {
			report.Dropped++
			continue
		}
		seenEntities[code] = struct{}{}
		if len(clean.Entities) < maxEntities {
			clean.Entities = append(clean.Entities, code)
		} else {
			report.Dropped++
		}
	}

	clean.Topics = sanitizeList(input.Topics, maxTopics, &report)
	clean.Concepts = sanitizeList(input.Concepts, maxConcepts, &report)
	return clean, report
}

func sanitizeList(values []string, limit int, report *validationReport) []string {
	clean := make([]string, 0, limit)
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		term := strings.TrimSpace(value)
		if term == "" || len(term) > maxTermLength {
			report.Dropped++
			continue
		}
		key := strings.ToLower(term)
		if _, duplicate := seen[key]; duplicate {
			report.Dropped++
			continue
		}
		seen[key] = struct{}{}
		if len(clean) < limit {
			clean = append(clean, term)
		} else {
			report.Dropped++
		}
	}
	return clean
}

package enrichment

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
)

func TestSanitizeKeepsOnlyKnownEntitiesAndNormalizesThem(t *testing.T) {
	known := map[string]struct{}{"USD": {}, "EUR": {}}
	clean, report := sanitize(ai.Enrichment{
		Entities: []string{" usd ", "usd", "XYZ", "", strings.Repeat("a", 101)},
		Topics:   []string{" Monetary Policy ", "monetary policy", ""},
		Concepts: []string{"rate decision"},
	}, known)

	if len(clean.Entities) != 1 || clean.Entities[0] != "USD" {
		t.Errorf("entities = %v, want [USD]", clean.Entities)
	}
	if len(report.UnknownEntities) != 1 || report.UnknownEntities[0] != "XYZ" {
		t.Errorf("unknown entities = %v, want [XYZ]", report.UnknownEntities)
	}
	if len(clean.Topics) != 1 || clean.Topics[0] != "Monetary Policy" {
		t.Errorf("topics = %v, want [Monetary Policy]", clean.Topics)
	}
	if len(clean.Concepts) != 1 || clean.Concepts[0] != "rate decision" {
		t.Errorf("concepts = %v, want [rate decision]", clean.Concepts)
	}
	if report.Dropped != 6 {
		t.Errorf("dropped = %d, want 6", report.Dropped)
	}
}

func TestSanitizeCapsEveryList(t *testing.T) {
	known := map[string]struct{}{}
	input := ai.Enrichment{
		Entities: make([]string, 0, 15),
		Topics:   make([]string, 0, 15),
		Concepts: make([]string, 0, 15),
	}
	for index := 0; index < 15; index++ {
		code := fmt.Sprintf("E%02d", index)
		known[code] = struct{}{}
		input.Entities = append(input.Entities, code)
		input.Topics = append(input.Topics, "topic-"+strings.Repeat("x", index+1))
		input.Concepts = append(input.Concepts, "concept-"+strings.Repeat("x", index+1))
	}

	clean, report := sanitize(input, known)

	if len(clean.Entities) != maxEntities {
		t.Errorf("entities = %d, want %d", len(clean.Entities), maxEntities)
	}
	if len(clean.Topics) != maxTopics {
		t.Errorf("topics = %d, want %d", len(clean.Topics), maxTopics)
	}
	if len(clean.Concepts) != maxConcepts {
		t.Errorf("concepts = %d, want %d", len(clean.Concepts), maxConcepts)
	}
	if report.Dropped != 15 {
		t.Errorf("dropped = %d, want 15", report.Dropped)
	}
}

func TestSanitizeAcceptsEmptyModelOutput(t *testing.T) {
	clean, report := sanitize(ai.Enrichment{}, map[string]struct{}{"USD": {}})
	if len(clean.Entities) != 0 || len(clean.Topics) != 0 || len(clean.Concepts) != 0 {
		t.Errorf("clean = %+v, want empty lists", clean)
	}
	if report.Dropped != 0 || len(report.UnknownEntities) != 0 {
		t.Errorf("report = %+v, want no drops", report)
	}
}

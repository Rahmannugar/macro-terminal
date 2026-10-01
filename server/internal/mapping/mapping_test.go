package mapping

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
)

var (
	usdID    = uuid.New()
	eurID    = uuid.New()
	gbpID    = uuid.New()
	xauID    = uuid.New()
	btcID    = uuid.New()
	stoxxID  = uuid.New()
	noEntity = uuid.UUID{}
)

// testDictionary deliberately includes an index whose name contains an
// entity name ("Euro Stoxx 50" / "Euro") to exercise the overlap rule.
func testDictionary() Dictionary {
	entities := []models.Entity{
		{ID: usdID, Code: "USD", Name: "United States Dollar"},
		{ID: eurID, Code: "EUR", Name: "Euro"},
		{ID: gbpID, Code: "GBP", Name: "British Pound"},
		{ID: xauID, Code: "XAU", Name: "Gold"},
		{ID: btcID, Code: "BTC", Name: "Bitcoin"},
		{ID: stoxxID, Code: "STOXX50", Name: "Euro Stoxx 50"},
	}
	terms := []models.KnowledgeTerm{
		{Name: "Fed", Type: "institution", EntityID: usdID},
		{Name: "Federal Reserve", Type: "institution", EntityID: usdID},
		{Name: "Bank of England", Type: "institution", EntityID: gbpID},
		{Name: "rate cut", Type: "topic", EntityID: usdID},
		{Name: "orphan phrase", Type: "topic", EntityID: noEntity},
	}
	pairs := []models.EntityPair{
		{BaseEntityID: eurID, QuoteEntityID: usdID, Symbol: "EUR/USD"},
		{BaseEntityID: gbpID, QuoteEntityID: usdID, Symbol: "GBP/USD"},
		{BaseEntityID: eurID, QuoteEntityID: gbpID, Symbol: "EUR/GBP"},
		{BaseEntityID: btcID, QuoteEntityID: usdID, Symbol: "BTC"},
		{BaseEntityID: xauID, QuoteEntityID: usdID, Symbol: "XAU"},
	}
	return Build(entities, terms, pairs)
}

func TestDictionaryMap(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		content    string
		wantCodes  []string
		wantPairs  []string
		wantMapped bool
	}{
		{
			name:       "institution term maps its entity and every pair it touches",
			title:      "Fed holds rates steady",
			content:    "Policymakers left the benchmark unchanged.",
			wantCodes:  []string{"USD"},
			wantPairs:  []string{"BTC", "EUR/USD", "GBP/USD", "XAU"},
			wantMapped: true,
		},
		{
			name:       "entity code matches case-insensitively",
			title:      "Greenback watch",
			content:    "A usd rebound lifted the session.",
			wantCodes:  []string{"USD"},
			wantPairs:  []string{"BTC", "EUR/USD", "GBP/USD", "XAU"},
			wantMapped: true,
		},
		{
			name:       "entity name maps directly",
			title:      "Gold hits another record",
			content:    "Bullion extended its rally.",
			wantCodes:  []string{"XAU"},
			wantPairs:  []string{"XAU"},
			wantMapped: true,
		},
		{
			name:       "word boundaries block partial matches",
			title:      "USDC and Goldman Sachs",
			content:    "Volumes in the stablecoin rose.",
			wantCodes:  nil,
			wantPairs:  nil,
			wantMapped: false,
		},
		{
			name:       "abbreviation does not fire inside its expansion",
			title:      "Federal agency reorganization",
			content:    "The board published new guidance.",
			wantCodes:  nil,
			wantPairs:  nil,
			wantMapped: false,
		},
		{
			name:       "overlapping phrase maps only the longest match",
			title:      "Euro Stoxx 50 climbs",
			content:    "European equities advanced.",
			wantCodes:  []string{"STOXX50"},
			wantPairs:  nil,
			wantMapped: true,
		},
		{
			name:       "short phrase still matches outside the overlap",
			title:      "The euro strengthens",
			content:    "After Euro Stoxx 50 closed higher.",
			wantCodes:  []string{"EUR", "STOXX50"},
			wantPairs:  []string{"EUR/GBP", "EUR/USD"},
			wantMapped: true,
		},
		{
			name:       "terms match verbatim, without plural stemming",
			title:      "Rate cuts are coming",
			content:    "Traders priced a full easing cycle.",
			wantCodes:  nil,
			wantPairs:  nil,
			wantMapped: false,
		},
		{
			name:       "seeded plural form matches",
			title:      "The first rate cut is priced in",
			content:    "Futures now imply two moves this year.",
			wantCodes:  []string{"USD"},
			wantPairs:  []string{"BTC", "EUR/USD", "GBP/USD", "XAU"},
			wantMapped: true,
		},
		{
			name:       "hyphenated phrase matches its spaced form",
			title:      "The rate-cut cycle begins",
			content:    "Traders priced two moves this year.",
			wantCodes:  []string{"USD"},
			wantPairs:  []string{"BTC", "EUR/USD", "GBP/USD", "XAU"},
			wantMapped: true,
		},
		{
			name:       "whitespace runs collapse before matching",
			title:      "",
			content:    "Policymakers delivered a rate  cut.",
			wantCodes:  []string{"USD"},
			wantPairs:  []string{"BTC", "EUR/USD", "GBP/USD", "XAU"},
			wantMapped: true,
		},
		{
			name:       "curly apostrophes do not block an abbreviation",
			title:      "Fed’s decision landed",
			content:    "",
			wantCodes:  []string{"USD"},
			wantPairs:  []string{"BTC", "EUR/USD", "GBP/USD", "XAU"},
			wantMapped: true,
		},
		{
			name:       "one entity via code and name counts once",
			title:      "The Fed and the Federal Reserve both spoke",
			content:    "Two statements, one message.",
			wantCodes:  []string{"USD"},
			wantPairs:  []string{"BTC", "EUR/USD", "GBP/USD", "XAU"},
			wantMapped: true,
		},
		{
			name:       "both endpoints expose the cross pair",
			title:      "British Pound and Euro",
			content:    "Sterling and the single currency diverged.",
			wantCodes:  []string{"EUR", "GBP"},
			wantPairs:  []string{"EUR/GBP", "EUR/USD", "GBP/USD"},
			wantMapped: true,
		},
		{
			name:       "term without an entity contributes nothing",
			title:      "An orphan phrase appears",
			content:    "Still nothing to map to.",
			wantCodes:  nil,
			wantPairs:  nil,
			wantMapped: false,
		},
		{
			name:       "empty candidate maps nothing",
			title:      "",
			content:    "",
			wantCodes:  nil,
			wantPairs:  nil,
			wantMapped: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dictionary := testDictionary()
			outcome := dictionary.Map(test.title, test.content)

			if outcome.Mapped() != test.wantMapped {
				t.Fatalf("Mapped() = %v, want %v (codes %v)", outcome.Mapped(), test.wantMapped, outcome.EntityCodes)
			}
			if !equalStrings(outcome.EntityCodes, test.wantCodes) {
				t.Errorf("EntityCodes = %v, want %v", outcome.EntityCodes, test.wantCodes)
			}
			if !equalStrings(outcome.PairSymbols, test.wantPairs) {
				t.Errorf("PairSymbols = %v, want %v", outcome.PairSymbols, test.wantPairs)
			}
		})
	}
}

func TestBuildEntityRowWinsPhraseCollision(t *testing.T) {
	first := uuid.New()
	second := uuid.New()
	dictionary := Build(
		[]models.Entity{
			{ID: first, Code: "AAA", Name: "Shared Name"},
			{ID: second, Code: "BBB", Name: "Other"},
		},
		[]models.KnowledgeTerm{{Name: "Shared Name", Type: "alias", EntityID: second}},
		nil,
	)

	outcome := dictionary.Map("Shared Name moves", "")
	if !outcome.Mapped() || outcome.EntityCodes[0] != "AAA" {
		t.Fatalf("duplicate phrase resolved to %v, want the entity row to win (AAA)", outcome.EntityCodes)
	}
	if len(outcome.EntityCodes) != 1 {
		t.Fatalf("EntityCodes = %v, want exactly one entry", outcome.EntityCodes)
	}
}

func TestSummarize(t *testing.T) {
	dictionary := testDictionary()
	outcomes := []Outcome{
		dictionary.Map("Fed holds rates steady", ""),
		dictionary.Map("Gold hits another record", ""),
		dictionary.Map("Nothing here", ""),
	}

	summary := Summarize(outcomes)
	if summary.Mapped != 2 || summary.Unmapped != 1 {
		t.Fatalf("mapped/unmapped = %d/%d, want 2/1", summary.Mapped, summary.Unmapped)
	}
	wantCodes := []string{"USD", "XAU"}
	if !equalStrings(summary.EntityCodes, wantCodes) {
		t.Errorf("EntityCodes = %v, want %v", summary.EntityCodes, wantCodes)
	}
	wantPairs := []string{"BTC", "EUR/USD", "GBP/USD", "XAU"}
	if !equalStrings(summary.PairSymbols, wantPairs) {
		t.Errorf("PairSymbols = %v, want %v", summary.PairSymbols, wantPairs)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	summary := Summarize(nil)
	if summary.Mapped != 0 || summary.Unmapped != 0 {
		t.Fatalf("empty summarize = %+v, want zeros", summary)
	}
	if len(summary.EntityCodes) != 0 || len(summary.PairSymbols) != 0 {
		t.Fatalf("empty summarize returned entries: %+v", summary)
	}
}

type fakeVocabulary struct {
	entities []models.Entity
	terms    []models.KnowledgeTerm
	pairs    []models.EntityPair
	err      error
}

func (vocabulary *fakeVocabulary) ListEntities(context.Context) ([]models.Entity, error) {
	return vocabulary.entities, vocabulary.err
}

func (vocabulary *fakeVocabulary) ListEntityKnowledgeTerms(
	context.Context,
) ([]models.KnowledgeTerm, error) {
	return vocabulary.terms, vocabulary.err
}

func (vocabulary *fakeVocabulary) ListEntityPairs(context.Context) ([]models.EntityPair, error) {
	return vocabulary.pairs, vocabulary.err
}

func TestLoaderBuildsDictionary(t *testing.T) {
	loader := NewLoader(&fakeVocabulary{
		entities: []models.Entity{{ID: usdID, Code: "USD", Name: "United States Dollar"}},
		terms:    []models.KnowledgeTerm{{Name: "Fed", Type: "institution", EntityID: usdID}},
		pairs:    []models.EntityPair{{BaseEntityID: usdID, QuoteEntityID: uuid.New(), Symbol: "USD/JPY"}},
	})

	dictionary, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	outcome := dictionary.Map("Fed speaks", "")
	if !reflect.DeepEqual(outcome.EntityCodes, []string{"USD"}) {
		t.Fatalf("EntityCodes = %v, want [USD]", outcome.EntityCodes)
	}
	if !reflect.DeepEqual(outcome.PairSymbols, []string{"USD/JPY"}) {
		t.Fatalf("PairSymbols = %v, want [USD/JPY]", outcome.PairSymbols)
	}
}

func TestLoaderPropagatesVocabularyFailure(t *testing.T) {
	boom := errors.New("database down")
	_, err := NewLoader(&fakeVocabulary{err: boom}).Load(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Load error = %v, want wrapped vocabulary failure", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) == 0 && len(want) == 0 {
		return true
	}
	return reflect.DeepEqual(got, want)
}

// BenchmarkDictionaryMap sizes a realistic production dictionary (~110
// phrases) against a typical headline and snippet.
func BenchmarkDictionaryMap(b *testing.B) {
	entities := make([]models.Entity, 0, 40)
	for i := 0; i < 40; i++ {
		entities = append(entities, models.Entity{
			ID:   uuid.New(),
			Code: fmt.Sprintf("T%d", i),
			Name: fmt.Sprintf("Test Asset Number %d", i),
		})
	}
	terms := make([]models.KnowledgeTerm, 0, 70)
	for i := 0; i < 70; i++ {
		terms = append(terms, models.KnowledgeTerm{
			Name:     fmt.Sprintf("knowledge phrase %d", i),
			Type:     "topic",
			EntityID: entities[i%len(entities)].ID,
		})
	}
	dictionary := Build(entities, terms, nil)
	title := "Fed holds rates steady as inflation cools and gold hits a record"
	content := "Policymakers left the benchmark unchanged while traders watched the dollar, sterling and the Nikkei."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dictionary.Map(title, content)
	}
}

// BenchmarkDictionaryMap10k answers what happens when the vocabulary grows
// to ten thousand terms.
func BenchmarkDictionaryMap10k(b *testing.B) {
	entities := make([]models.Entity, 0, 500)
	for i := 0; i < 500; i++ {
		entities = append(entities, models.Entity{
			ID:   uuid.New(),
			Code: fmt.Sprintf("E%d", i),
			Name: fmt.Sprintf("Asset Number %d", i),
		})
	}
	terms := make([]models.KnowledgeTerm, 0, 10000)
	for i := 0; i < 10000; i++ {
		terms = append(terms, models.KnowledgeTerm{
			Name:     fmt.Sprintf("knowledge phrase number %d for scaling", i),
			Type:     "topic",
			EntityID: entities[i%len(entities)].ID,
		})
	}
	dictionary := Build(entities, terms, nil)
	title := "Fed holds rates steady as inflation cools and gold hits a record"
	content := "Policymakers left the benchmark unchanged while traders watched the dollar, sterling and the Nikkei."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dictionary.Map(title, content)
	}
}

// BenchmarkDictionaryBuild10k measures what every scheduling pass pays to
// compile the vocabulary from rows.
func BenchmarkDictionaryBuild10k(b *testing.B) {
	entities := make([]models.Entity, 0, 500)
	for i := 0; i < 500; i++ {
		entities = append(entities, models.Entity{
			ID:   uuid.New(),
			Code: fmt.Sprintf("E%d", i),
			Name: fmt.Sprintf("Asset Number %d", i),
		})
	}
	terms := make([]models.KnowledgeTerm, 0, 10000)
	for i := 0; i < 10000; i++ {
		terms = append(terms, models.KnowledgeTerm{
			Name:     fmt.Sprintf("knowledge phrase number %d for scaling", i),
			Type:     "topic",
			EntityID: entities[i%len(entities)].ID,
		})
	}
	pairs := make([]models.EntityPair, 0, 200)
	for i := 0; i < 200; i++ {
		pairs = append(pairs, models.EntityPair{
			BaseEntityID:  entities[i].ID,
			QuoteEntityID: entities[i+1].ID,
			Symbol:        fmt.Sprintf("P%d", i),
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Build(entities, terms, pairs)
	}
}

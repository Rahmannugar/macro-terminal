// Package mapping assigns article candidates to terminal entities through
// deterministic vocabulary matching. Candidates that name nothing stay
// visible as unmapped instead of being guessed.
package mapping

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
)

type Vocabulary interface {
	ListEntities(context.Context) ([]models.Entity, error)
	ListEntityKnowledgeTerms(context.Context) ([]models.KnowledgeTerm, error)
	ListEntityPairs(context.Context) ([]models.EntityPair, error)
	ListIndicators(context.Context) ([]models.Indicator, error)
	ListIndicatorKnowledgeTerms(context.Context) ([]models.IndicatorTerm, error)
}

type Loader struct {
	vocabulary Vocabulary
}

func NewLoader(vocabulary Vocabulary) *Loader {
	return &Loader{vocabulary: vocabulary}
}

func (loader *Loader) Load(ctx context.Context) (Dictionary, error) {
	entities, err := loader.vocabulary.ListEntities(ctx)
	if err != nil {
		return Dictionary{}, fmt.Errorf("list entities: %w", err)
	}
	terms, err := loader.vocabulary.ListEntityKnowledgeTerms(ctx)
	if err != nil {
		return Dictionary{}, fmt.Errorf("list knowledge terms: %w", err)
	}
	pairs, err := loader.vocabulary.ListEntityPairs(ctx)
	if err != nil {
		return Dictionary{}, fmt.Errorf("list entity pairs: %w", err)
	}
	indicators, err := loader.vocabulary.ListIndicators(ctx)
	if err != nil {
		return Dictionary{}, fmt.Errorf("list indicators: %w", err)
	}
	indicatorTerms, err := loader.vocabulary.ListIndicatorKnowledgeTerms(ctx)
	if err != nil {
		return Dictionary{}, fmt.Errorf("list indicator knowledge terms: %w", err)
	}
	return Build(entities, terms, pairs, indicators, indicatorTerms), nil
}

type Dictionary struct {
	phrases  []phrase
	pairs    []pair
	codes    map[uuid.UUID]string
	currency map[string]uuid.UUID

	indicator indicatorIndex
}

type phrase struct {
	text     string // lowercased
	entityID uuid.UUID
}

type pair struct {
	baseID  uuid.UUID
	quoteID uuid.UUID
	symbol  string
}

// Build turns entity codes, entity names, and entity-linked terms into
// phrases. When two rows carry the same phrase, the first one wins.
func Build(
	entities []models.Entity,
	terms []models.KnowledgeTerm,
	pairs []models.EntityPair,
	indicators []models.Indicator,
	indicatorTerms []models.IndicatorTerm,
) Dictionary {
	dictionary := Dictionary{
		codes:    map[uuid.UUID]string{},
		currency: map[string]uuid.UUID{},
	}
	seen := map[string]bool{}
	add := func(text string, entityID uuid.UUID) {
		lower := normalize(text)
		if lower == "" || seen[lower] {
			return
		}
		seen[lower] = true
		dictionary.phrases = append(dictionary.phrases, phrase{text: lower, entityID: entityID})
	}

	for _, entity := range entities {
		dictionary.codes[entity.ID] = entity.Code
		if entity.Type == "currency" {
			dictionary.currency[strings.ToUpper(entity.Code)] = entity.ID
		}
		add(entity.Code, entity.ID)
		add(entity.Name, entity.ID)
	}
	for _, term := range terms {
		if term.EntityID == uuid.Nil {
			continue
		}
		add(term.Name, term.EntityID)
	}

	// Longer phrases claim their text first, so an overlapping shorter
	// phrase cannot double-map the same mention.
	sort.Slice(dictionary.phrases, func(i, j int) bool {
		if len(dictionary.phrases[i].text) != len(dictionary.phrases[j].text) {
			return len(dictionary.phrases[i].text) > len(dictionary.phrases[j].text)
		}
		return dictionary.phrases[i].text < dictionary.phrases[j].text
	})

	for _, entityPair := range pairs {
		dictionary.pairs = append(dictionary.pairs, pair{
			baseID:  entityPair.BaseEntityID,
			quoteID: entityPair.QuoteEntityID,
			symbol:  entityPair.Symbol,
		})
	}
	dictionary.indicator = buildIndicatorIndex(indicators, indicatorTerms)
	return dictionary
}

type Outcome struct {
	EntityIDs   []uuid.UUID
	EntityCodes []string
	PairSymbols []string
}

// EntityForCurrency resolves a currency code (USD, EUR) to the currency
// entity, so calendar events link by the provider's currency rather than
// the classified indicator's home entity.
func (dictionary Dictionary) EntityForCurrency(code string) (uuid.UUID, bool) {
	id, ok := dictionary.currency[strings.ToUpper(strings.TrimSpace(code))]
	return id, ok
}

// indicatorIndex classifies calendar event names to economic indicators.
// Phrases are ordered longest-first so "eurozone flash cpi" wins over the
// US-biased "cpi" inside the same name.
type indicatorIndex struct {
	phrases []indicatorPhrase
	entity  map[uuid.UUID]uuid.UUID // indicator ID -> entity ID
}

type indicatorPhrase struct {
	text        string // lowercased
	indicatorID uuid.UUID
}

func buildIndicatorIndex(
	indicators []models.Indicator,
	terms []models.IndicatorTerm,
) indicatorIndex {
	index := indicatorIndex{entity: map[uuid.UUID]uuid.UUID{}}
	seen := map[string]bool{}
	for _, indicator := range indicators {
		index.entity[indicator.ID] = indicator.EntityID
	}
	for _, term := range terms {
		if _, known := index.entity[term.IndicatorID]; !known {
			continue
		}
		lower := normalize(term.Name)
		if lower == "" || seen[lower] {
			continue
		}
		seen[lower] = true
		index.phrases = append(index.phrases, indicatorPhrase{
			text:        lower,
			indicatorID: term.IndicatorID,
		})
	}
	sort.Slice(index.phrases, func(i, j int) bool {
		if len(index.phrases[i].text) != len(index.phrases[j].text) {
			return len(index.phrases[i].text) > len(index.phrases[j].text)
		}
		return index.phrases[i].text < index.phrases[j].text
	})
	return index
}

// IndicatorMatch is the indicator an event name classifies to, together
// with the entity that indicator belongs to.
type IndicatorMatch struct {
	IndicatorID uuid.UUID
	EntityID    uuid.UUID
}

// ClassifyIndicator matches an event name against the indicator phrases.
// Matching follows the same rules as entity mapping: case-insensitive,
// word-bounded, verbatim, longest phrase first. An event that names
// nothing classifiable is not stored as a calendar event.
func (dictionary Dictionary) ClassifyIndicator(name string) (IndicatorMatch, bool) {
	text := normalize(name)

	var taken []span
	for _, candidate := range dictionary.indicator.phrases {
		start, end, ok := findPhrase(text, candidate.text, taken)
		if !ok {
			continue
		}
		taken = append(taken, span{start: start, end: end})
		entityID, known := dictionary.indicator.entity[candidate.indicatorID]
		if !known {
			continue
		}
		return IndicatorMatch{
			IndicatorID: candidate.indicatorID,
			EntityID:    entityID,
		}, true
	}
	return IndicatorMatch{}, false
}

func (outcome Outcome) Mapped() bool {
	return len(outcome.EntityIDs) > 0
}

type span struct {
	start int
	end   int
}

// Map matches a candidate's title and snippet. Matching is case-insensitive
// and word-bounded ("usd" never fires inside "usdc"), and phrases are
// verbatim: "rate cut" does not match "rate cuts", so the vocabulary seeds
// plural forms explicitly.
func (dictionary Dictionary) Map(title, content string) Outcome {
	text := normalize(title + " " + content)

	var outcome Outcome
	var taken []span
	matched := map[uuid.UUID]bool{}
	for _, candidatePhrase := range dictionary.phrases {
		start, end, ok := findPhrase(text, candidatePhrase.text, taken)
		if !ok {
			continue
		}
		taken = append(taken, span{start: start, end: end})
		if matched[candidatePhrase.entityID] {
			continue
		}
		matched[candidatePhrase.entityID] = true
		outcome.EntityIDs = append(outcome.EntityIDs, candidatePhrase.entityID)
		if code, ok := dictionary.codes[candidatePhrase.entityID]; ok {
			outcome.EntityCodes = append(outcome.EntityCodes, code)
		}
	}

	for _, dictionaryPair := range dictionary.pairs {
		if matched[dictionaryPair.baseID] || matched[dictionaryPair.quoteID] {
			outcome.PairSymbols = append(outcome.PairSymbols, dictionaryPair.symbol)
		}
	}

	sort.Strings(outcome.EntityCodes)
	sort.Strings(outcome.PairSymbols)
	sort.Slice(outcome.EntityIDs, func(i, j int) bool {
		return outcome.EntityIDs[i].String() < outcome.EntityIDs[j].String()
	})
	return outcome
}

// normalize lowercases, turns dash variants into spaces, and collapses
// whitespace runs, so "rate-hike", "rate–cut", and "rate  cut" all match the
// single-spaced vocabulary phrase. Apostrophes are straightened but kept,
// so "Fed's" still matches the abbreviation "Fed".
func normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '-', '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2212':
			return ' '
		case '\u2018', '\u2019':
			return '\''
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func findPhrase(text, phrase string, taken []span) (int, int, bool) {
	if phrase == "" {
		return 0, 0, false
	}
	for from := 0; from <= len(text)-len(phrase); {
		index := strings.Index(text[from:], phrase)
		if index < 0 {
			return 0, 0, false
		}
		start := from + index
		end := start + len(phrase)
		from = start + 1
		if !atBoundary(text, start-1) || !atBoundary(text, end) {
			continue
		}
		if overlaps(taken, start, end) {
			continue
		}
		return start, end, true
	}
	return 0, 0, false
}

func atBoundary(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return true
	}
	return !isWordByte(text[index])
}

func isWordByte(b byte) bool {
	// Multi-byte characters count as word bytes so accents and non-Latin
	// scripts glue to the word around them.
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b >= 0x80
}

func overlaps(taken []span, start, end int) bool {
	for _, claimed := range taken {
		if start < claimed.end && claimed.start < end {
			return true
		}
	}
	return false
}

type Summary struct {
	Mapped      int
	Unmapped    int
	EntityCodes []string // distinct, sorted
	PairSymbols []string // distinct, sorted
}

func Summarize(outcomes []Outcome) Summary {
	var summary Summary
	entityCodes := map[string]bool{}
	pairSymbols := map[string]bool{}
	for _, outcome := range outcomes {
		if outcome.Mapped() {
			summary.Mapped++
		} else {
			summary.Unmapped++
		}
		for _, code := range outcome.EntityCodes {
			entityCodes[code] = true
		}
		for _, symbol := range outcome.PairSymbols {
			pairSymbols[symbol] = true
		}
	}
	summary.EntityCodes = sortedSet(entityCodes)
	summary.PairSymbols = sortedSet(pairSymbols)
	return summary
}

func sortedSet(values map[string]bool) []string {
	sorted := make([]string, 0, len(values))
	for value := range values {
		sorted = append(sorted, value)
	}
	sort.Strings(sorted)
	return sorted
}

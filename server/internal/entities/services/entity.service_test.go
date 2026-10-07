package services

import (
	"context"
	"errors"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	"github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeEntityRepository struct {
	entities   map[string]models.Entity
	pairs      map[string]models.EntityPair
	assets     map[string]models.UserAsset
	terms      map[string]models.KnowledgeTerm
	indicators map[string]models.Indicator
}

func newFakeEntityRepository() *fakeEntityRepository {
	return &fakeEntityRepository{
		entities:   map[string]models.Entity{},
		pairs:      map[string]models.EntityPair{},
		assets:     map[string]models.UserAsset{},
		terms:      map[string]models.KnowledgeTerm{},
		indicators: map[string]models.Indicator{},
	}
}

func knowledgeTermKey(name, termType string) string {
	return name + "\x00" + termType
}

func (repository *fakeEntityRepository) ListEntityKnowledgeTerms(
	context.Context,
) ([]models.KnowledgeTerm, error) {
	terms := make([]models.KnowledgeTerm, 0, len(repository.terms))
	for _, term := range repository.terms {
		terms = append(terms, term)
	}
	return terms, nil
}

func (repository *fakeEntityRepository) UpsertKnowledgeTerm(
	_ context.Context,
	term models.KnowledgeTerm,
) (models.KnowledgeTerm, error) {
	key := knowledgeTermKey(term.Name, term.Type)
	if existing, ok := repository.terms[key]; ok {
		term.ID = existing.ID
		term.CreatedAt = existing.CreatedAt
	}
	repository.terms[key] = term
	return term, nil
}

func (repository *fakeEntityRepository) ListEntities(context.Context) ([]models.Entity, error) {
	return nil, nil
}

func (repository *fakeEntityRepository) ListIndicators(
	context.Context,
) ([]models.Indicator, error) {
	indicators := make([]models.Indicator, 0, len(repository.indicators))
	for _, indicator := range repository.indicators {
		indicators = append(indicators, indicator)
	}
	return indicators, nil
}

func (repository *fakeEntityRepository) ListIndicatorKnowledgeTerms(
	context.Context,
) ([]models.IndicatorTerm, error) {
	var terms []models.IndicatorTerm
	for _, term := range repository.terms {
		if term.IndicatorID != uuid.Nil {
			terms = append(terms, models.IndicatorTerm{
				Name:        term.Name,
				IndicatorID: term.IndicatorID,
			})
		}
	}
	return terms, nil
}

func (repository *fakeEntityRepository) UpsertIndicator(
	_ context.Context,
	indicator models.Indicator,
) (models.Indicator, error) {
	key := indicator.Name + "\x00" + indicator.EntityID.String()
	if existing, ok := repository.indicators[key]; ok {
		indicator.ID = existing.ID
		indicator.CreatedAt = existing.CreatedAt
	}
	repository.indicators[key] = indicator
	return indicator, nil
}

func (repository *fakeEntityRepository) EntityByCode(_ context.Context, code string) (models.Entity, error) {
	entity, ok := repository.entities[code]
	if !ok {
		return models.Entity{}, pgx.ErrNoRows
	}
	return entity, nil
}

func (repository *fakeEntityRepository) CreateEntity(
	_ context.Context,
	entity models.Entity,
) (models.Entity, error) {
	repository.entities[entity.Code] = entity
	return entity, nil
}

func (repository *fakeEntityRepository) UpsertEntity(
	_ context.Context,
	entity models.Entity,
) (models.Entity, error) {
	if existing, ok := repository.entities[entity.Code]; ok {
		entity.ID = existing.ID
		entity.CreatedAt = existing.CreatedAt
	}
	repository.entities[entity.Code] = entity
	return entity, nil
}

func (repository *fakeEntityRepository) ListEntityPairs(context.Context) ([]models.EntityPair, error) {
	return nil, nil
}

func (repository *fakeEntityRepository) EntityPairsContainingEntity(
	context.Context,
	uuid.UUID,
) ([]models.EntityPair, error) {
	return nil, nil
}

func (repository *fakeEntityRepository) EntityPairBySymbol(
	_ context.Context,
	symbol string,
) (models.EntityPair, error) {
	pair, ok := repository.pairs[symbol]
	if !ok {
		return models.EntityPair{}, pgx.ErrNoRows
	}
	return pair, nil
}

func (repository *fakeEntityRepository) CreateEntityPair(
	_ context.Context,
	pair models.EntityPair,
) (models.EntityPair, error) {
	repository.pairs[pair.Symbol] = pair
	return pair, nil
}

func (repository *fakeEntityRepository) UpsertEntityPair(
	_ context.Context,
	pair models.EntityPair,
) (models.EntityPair, error) {
	if existing, ok := repository.pairs[pair.Symbol]; ok {
		pair.ID = existing.ID
		pair.CreatedAt = existing.CreatedAt
	}
	repository.pairs[pair.Symbol] = pair
	return pair, nil
}

func (repository *fakeEntityRepository) EntityPairByID(
	_ context.Context,
	id uuid.UUID,
) (models.EntityPair, error) {
	for _, pair := range repository.pairs {
		if pair.ID == id {
			return pair, nil
		}
	}
	return models.EntityPair{}, pgx.ErrNoRows
}

func (repository *fakeEntityRepository) EntityPairsByUser(context.Context, uuid.UUID) ([]models.EntityPair, error) {
	return nil, nil
}

func (repository *fakeEntityRepository) UserIDsByEntityPair(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (repository *fakeEntityRepository) SubscribeUserAsset(
	_ context.Context,
	userID, entityPairID uuid.UUID,
) error {
	repository.assets[userID.String()+"/"+entityPairID.String()] = models.UserAsset{
		UserID:       userID,
		EntityPairID: entityPairID,
	}
	return nil
}

func (repository *fakeEntityRepository) UnsubscribeUserAsset(
	_ context.Context,
	userID, entityPairID uuid.UUID,
) error {
	delete(repository.assets, userID.String()+"/"+entityPairID.String())
	return nil
}

func (repository *fakeEntityRepository) IndicatorByID(
	_ context.Context,
	id uuid.UUID,
) (models.Indicator, error) {
	for _, indicator := range repository.indicators {
		if indicator.ID == id {
			return indicator, nil
		}
	}
	return models.Indicator{}, pgx.ErrNoRows
}

func (repository *fakeEntityRepository) CreateIndicator(
	_ context.Context,
	indicator models.Indicator,
) (models.Indicator, error) {
	repository.indicators[indicator.Name+"\x00"+indicator.EntityID.String()] = indicator
	return indicator, nil
}

func (repository *fakeEntityRepository) CreateKnowledgeTerm(
	_ context.Context,
	term models.KnowledgeTerm,
) (models.KnowledgeTerm, error) {
	repository.terms[knowledgeTermKey(term.Name, term.Type)] = term
	return term, nil
}

func (repository *fakeEntityRepository) ListEntitiesPage(
	context.Context, *paging.Cursor, int32,
) ([]models.Entity, *paging.Cursor, error) {
	return nil, nil, nil
}

func (repository *fakeEntityRepository) ListEntityPairsPage(
	context.Context, *paging.Cursor, int32,
) ([]models.EntityPair, *paging.Cursor, error) {
	return nil, nil, nil
}

func (repository *fakeEntityRepository) ListIndicatorsPage(
	context.Context, *paging.Cursor, int32,
) ([]models.Indicator, *paging.Cursor, error) {
	return nil, nil, nil
}

func (repository *fakeEntityRepository) ListKnowledgeTermsPage(
	context.Context, *paging.Cursor, int32,
) ([]models.KnowledgeTerm, *paging.Cursor, error) {
	return nil, nil, nil
}

func TestCreateEntityValidation(t *testing.T) {
	service := NewEntityService(newFakeEntityRepository())

	tests := []struct {
		name       string
		code       string
		nameValue  string
		entityType string
		wantErr    error
	}{
		{name: "blank code", code: "  ", nameValue: "Euro", entityType: "currency", wantErr: ErrEntityCodeRequired},
		{name: "blank name", code: "EUR", nameValue: "", entityType: "currency", wantErr: ErrEntityNameRequired},
		{name: "blank type", code: "EUR", nameValue: "Euro", entityType: " ", wantErr: ErrEntityTypeRequired},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.CreateEntity(context.Background(), test.code, test.nameValue, test.entityType)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateEntity() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestCreateEntityRejectsDuplicateCode(t *testing.T) {
	service := NewEntityService(newFakeEntityRepository())

	if _, err := service.CreateEntity(context.Background(), "EUR", "Euro", "currency"); err != nil {
		t.Fatalf("first CreateEntity: %v", err)
	}
	if _, err := service.CreateEntity(context.Background(), "EUR", "Euro alt", "currency"); !errors.Is(
		err, ErrEntityCodeExists,
	) {
		t.Fatalf("duplicate CreateEntity error = %v, want %v", err, ErrEntityCodeExists)
	}
}

func TestEnsureEntityIsIdempotent(t *testing.T) {
	repository := newFakeEntityRepository()
	service := NewEntityService(repository)

	first, err := service.EnsureEntity(context.Background(), "USD", "United States Dollar", "currency")
	if err != nil {
		t.Fatalf("first EnsureEntity: %v", err)
	}
	second, err := service.EnsureEntity(context.Background(), "USD", "US Dollar", "currency")
	if err != nil {
		t.Fatalf("second EnsureEntity: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-seed created a new entity instead of updating in place")
	}
	if second.Name != "US Dollar" {
		t.Fatalf("re-seed did not apply the name change, got %q", second.Name)
	}
	if len(repository.entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(repository.entities))
	}
}

func TestCreateEntityPairValidation(t *testing.T) {
	repository := newFakeEntityRepository()
	service := NewEntityService(repository)

	for _, code := range []string{"USD", "GBP", "EUR"} {
		if _, err := service.EnsureEntity(context.Background(), code, code, "currency"); err != nil {
			t.Fatalf("EnsureEntity(%s): %v", code, err)
		}
	}

	tests := []struct {
		name    string
		base    string
		quote   string
		symbol  string
		wantErr error
	}{
		{name: "blank symbol", base: "GBP", quote: "USD", symbol: "  ", wantErr: ErrPairSymbolRequired},
		{name: "same entity", base: "USD", quote: "USD", symbol: "USD/USD", wantErr: ErrPairEntitiesMustDiffer},
		{
			name:    "unknown base entity",
			base:    "CHF",
			quote:   "USD",
			symbol:  "CHF/USD",
			wantErr: ErrEntityNotFound,
		},
		{
			name:    "unknown quote entity",
			base:    "GBP",
			quote:   "CHF",
			symbol:  "GBP/CHF",
			wantErr: ErrEntityNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.CreateEntityPair(context.Background(), test.base, test.quote, test.symbol)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateEntityPair() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestCreateEntityPairRejectsDuplicateSymbol(t *testing.T) {
	service := NewEntityService(newFakeEntityRepository())

	for _, code := range []string{"USD", "GBP"} {
		if _, err := service.EnsureEntity(context.Background(), code, code, "currency"); err != nil {
			t.Fatalf("EnsureEntity(%s): %v", code, err)
		}
	}
	if _, err := service.CreateEntityPair(context.Background(), "GBP", "USD", "GBP/USD"); err != nil {
		t.Fatalf("first CreateEntityPair: %v", err)
	}
	if _, err := service.CreateEntityPair(
		context.Background(), "USD", "GBP", "GBP/USD",
	); !errors.Is(err, ErrPairSymbolExists) {
		t.Fatalf("duplicate symbol error = %v, want %v", err, ErrPairSymbolExists)
	}
}

func TestEnsureEntityPairIsIdempotent(t *testing.T) {
	repository := newFakeEntityRepository()
	service := NewEntityService(repository)

	for _, code := range []string{"USD", "GBP"} {
		if _, err := service.EnsureEntity(context.Background(), code, code, "currency"); err != nil {
			t.Fatalf("EnsureEntity(%s): %v", code, err)
		}
	}

	first, err := service.EnsureEntityPair(context.Background(), "GBP", "USD", "GBP/USD")
	if err != nil {
		t.Fatalf("first EnsureEntityPair: %v", err)
	}
	second, err := service.EnsureEntityPair(context.Background(), "GBP", "USD", "GBP/USD")
	if err != nil {
		t.Fatalf("second EnsureEntityPair: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-seed created a new pair instead of updating in place")
	}
	if len(repository.pairs) != 1 {
		t.Fatalf("pairs = %d, want 1", len(repository.pairs))
	}
}

func TestUserAssetServiceSubscribe(t *testing.T) {
	repository := newFakeEntityRepository()
	entityService := NewEntityService(repository)
	assetService := NewUserAssetService(repository)

	if _, err := entityService.EnsureEntity(context.Background(), "USD", "USD", "currency"); err != nil {
		t.Fatalf("EnsureEntity: %v", err)
	}
	pair, err := entityService.EnsureEntityPair(context.Background(), "GBP", "USD", "GBP/USD")
	if err == nil {
		t.Fatalf("pair should not exist yet: %v", err)
	}
	if _, err := entityService.EnsureEntity(context.Background(), "GBP", "GBP", "currency"); err != nil {
		t.Fatalf("EnsureEntity GBP: %v", err)
	}
	pair, err = entityService.EnsureEntityPair(context.Background(), "GBP", "USD", "GBP/USD")
	if err != nil {
		t.Fatalf("EnsureEntityPair: %v", err)
	}

	userID := uuid.New()
	if err := assetService.Subscribe(context.Background(), userID, pair.ID); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := assetService.Subscribe(context.Background(), userID, pair.ID); err != nil {
		t.Fatalf("repeated Subscribe: %v", err)
	}
	if len(repository.assets) != 1 {
		t.Fatalf("assets = %d, want 1 (subscribe must be idempotent)", len(repository.assets))
	}

	if err := assetService.Subscribe(context.Background(), uuid.Nil, pair.ID); !errors.Is(
		err, ErrUserIDRequired,
	) {
		t.Fatalf("nil user error = %v, want %v", err, ErrUserIDRequired)
	}
	if err := assetService.Subscribe(context.Background(), userID, uuid.Nil); !errors.Is(
		err, ErrEntityPairIDRequired,
	) {
		t.Fatalf("nil pair error = %v, want %v", err, ErrEntityPairIDRequired)
	}
	if err := assetService.Subscribe(context.Background(), userID, uuid.New()); !errors.Is(
		err, ErrEntityPairNotFound,
	) {
		t.Fatalf("unknown pair error = %v, want %v", err, ErrEntityPairNotFound)
	}

	if err := assetService.Unsubscribe(context.Background(), userID, pair.ID); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}
	if len(repository.assets) != 0 {
		t.Fatalf("assets = %d after unsubscribe, want 0", len(repository.assets))
	}
}

func TestEnsureKnowledgeTermValidation(t *testing.T) {
	repository := newFakeEntityRepository()
	service := NewEntityService(repository)
	if _, err := service.EnsureEntity(context.Background(), "USD", "United States Dollar", "currency"); err != nil {
		t.Fatalf("EnsureEntity: %v", err)
	}

	tests := []struct {
		name       string
		termName   string
		termType   string
		entityCode string
		wantErr    error
	}{
		{name: "blank name", termName: " ", termType: "institution", entityCode: "USD", wantErr: ErrKnowledgeTermNameRequired},
		{name: "blank type", termName: "Fed", termType: "", entityCode: "USD", wantErr: ErrKnowledgeTermTypeRequired},
		{name: "unknown entity", termName: "Fed", termType: "institution", entityCode: "XYZ", wantErr: ErrEntityNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.EnsureKnowledgeTerm(
				context.Background(), test.termName, test.termType, test.entityCode,
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("EnsureKnowledgeTerm() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestEnsureKnowledgeTermIsIdempotent(t *testing.T) {
	repository := newFakeEntityRepository()
	service := NewEntityService(repository)
	if _, err := service.EnsureEntity(context.Background(), "USD", "United States Dollar", "currency"); err != nil {
		t.Fatalf("EnsureEntity USD: %v", err)
	}
	if _, err := service.EnsureEntity(context.Background(), "EUR", "Euro", "currency"); err != nil {
		t.Fatalf("EnsureEntity EUR: %v", err)
	}

	first, err := service.EnsureKnowledgeTerm(context.Background(), "Fed", "institution", "USD")
	if err != nil {
		t.Fatalf("first EnsureKnowledgeTerm: %v", err)
	}
	second, err := service.EnsureKnowledgeTerm(context.Background(), "Fed", "institution", "EUR")
	if err != nil {
		t.Fatalf("second EnsureKnowledgeTerm: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-seed created a new term instead of relinking in place")
	}
	if second.EntityID == first.EntityID {
		t.Fatalf("re-seed did not apply the entity relink")
	}
	if len(repository.terms) != 1 {
		t.Fatalf("terms = %d, want 1 (name and type identify the row)", len(repository.terms))
	}
}

func TestCreateIndicatorValidation(t *testing.T) {
	service := NewEntityService(newFakeEntityRepository())

	if _, err := service.CreateIndicator(context.Background(), "", "labor", "US"); !errors.Is(err, ErrIndicatorNameRequired) {
		t.Fatalf("blank name error = %v, want %v", err, ErrIndicatorNameRequired)
	}
	if _, err := service.CreateIndicator(context.Background(), "NFP", "", "US"); !errors.Is(err, ErrIndicatorTypeRequired) {
		t.Fatalf("blank type error = %v, want %v", err, ErrIndicatorTypeRequired)
	}
	if _, err := service.CreateIndicator(context.Background(), "NFP", "labor", "MISSING"); !errors.Is(err, ErrEntityNotFound) {
		t.Fatalf("unknown entity error = %v, want %v", err, ErrEntityNotFound)
	}
}

func TestCreateIndicatorStoresAndResolves(t *testing.T) {
	service := NewEntityService(newFakeEntityRepository())
	if _, err := service.CreateEntity(context.Background(), "US", "United States", "country"); err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	created, err := service.CreateIndicator(context.Background(), "Nonfarm Payrolls", "labor", "US")
	if err != nil {
		t.Fatalf("CreateIndicator(): %v", err)
	}
	if created.ID == uuid.Nil || created.EntityID == uuid.Nil {
		t.Fatalf("CreateIndicator() = %+v, want an id and entity link", created)
	}

	found, err := service.IndicatorByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("IndicatorByID(): %v", err)
	}
	if found.Name != "Nonfarm Payrolls" {
		t.Fatalf("IndicatorByID() name = %q, want %q", found.Name, "Nonfarm Payrolls")
	}
	if _, err := service.IndicatorByID(context.Background(), uuid.New()); !errors.Is(err, ErrIndicatorNotFound) {
		t.Fatalf("unknown indicator error = %v, want %v", err, ErrIndicatorNotFound)
	}
}

func TestCreateKnowledgeTermLinkRules(t *testing.T) {
	service := NewEntityService(newFakeEntityRepository())
	if _, err := service.CreateEntity(context.Background(), "US", "United States", "country"); err != nil {
		t.Fatalf("seed US: %v", err)
	}
	if _, err := service.CreateEntity(context.Background(), "CA", "Canada", "country"); err != nil {
		t.Fatalf("seed CA: %v", err)
	}
	indicator, err := service.CreateIndicator(context.Background(), "CPI", "inflation", "US")
	if err != nil {
		t.Fatalf("seed indicator: %v", err)
	}

	if _, err := service.CreateKnowledgeTerm(context.Background(), "price index", "alias", "", nil); !errors.Is(err, ErrKnowledgeTermLinkRequired) {
		t.Fatalf("no link error = %v, want %v", err, ErrKnowledgeTermLinkRequired)
	}
	if _, err := service.CreateKnowledgeTerm(context.Background(), "price index", "alias", "MISSING", nil); !errors.Is(err, ErrEntityNotFound) {
		t.Fatalf("unknown entity error = %v, want %v", err, ErrEntityNotFound)
	}

	entityOnly, err := service.CreateKnowledgeTerm(context.Background(), "jobs report", "alias", "US", nil)
	if err != nil {
		t.Fatalf("entity-only term: %v", err)
	}
	if entityOnly.EntityID == uuid.Nil || entityOnly.IndicatorID != uuid.Nil {
		t.Fatalf("entity-only term = %+v, want an entity and no indicator", entityOnly)
	}

	missing := uuid.New()
	if _, err := service.CreateKnowledgeTerm(context.Background(), "price index", "alias", "", &missing); !errors.Is(err, ErrIndicatorNotFound) {
		t.Fatalf("unknown indicator error = %v, want %v", err, ErrIndicatorNotFound)
	}

	indicatorOnly, err := service.CreateKnowledgeTerm(context.Background(), "cpi", "alias", "", &indicator.ID)
	if err != nil {
		t.Fatalf("indicator-only term: %v", err)
	}
	if indicatorOnly.IndicatorID != indicator.ID || indicatorOnly.EntityID != indicator.EntityID {
		t.Fatalf("indicator-only term = %+v, want the indicator's entity link", indicatorOnly)
	}

	if _, err := service.CreateKnowledgeTerm(context.Background(), "headline cpi", "alias", "CA", &indicator.ID); !errors.Is(err, ErrKnowledgeTermEntityMismatch) {
		t.Fatalf("mismatched entity error = %v, want %v", err, ErrKnowledgeTermEntityMismatch)
	}

	both, err := service.CreateKnowledgeTerm(context.Background(), "us cpi", "alias", "US", &indicator.ID)
	if err != nil {
		t.Fatalf("indicator with matching entity term: %v", err)
	}
	if both.EntityID != indicator.EntityID || both.IndicatorID != indicator.ID {
		t.Fatalf("both-links term = %+v, want both links set", both)
	}
}

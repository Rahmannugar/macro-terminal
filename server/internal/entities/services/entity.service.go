package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	"github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrEntityCodeRequired        = errors.New("entity code is required")
	ErrEntityNameRequired        = errors.New("entity name is required")
	ErrEntityTypeRequired        = errors.New("entity type is required")
	ErrEntityCodeExists          = errors.New("entity code already exists")
	ErrEntityNotFound            = errors.New("entity not found")
	ErrPairSymbolRequired        = errors.New("entity pair symbol is required")
	ErrPairEntitiesMustDiffer    = errors.New("entity pair requires two different entities")
	ErrPairSymbolExists          = errors.New("entity pair symbol already exists")
	ErrEntityPairNotFound        = errors.New("entity pair not found")
	ErrKnowledgeTermNameRequired = errors.New("knowledge term name is required")
	ErrKnowledgeTermTypeRequired = errors.New("knowledge term type is required")
	ErrIndicatorNameRequired     = errors.New("indicator name is required")
	ErrIndicatorTypeRequired     = errors.New("indicator type is required")
)

type EntityRepository interface {
	ListEntities(context.Context) ([]models.Entity, error)
	EntityByCode(context.Context, string) (models.Entity, error)
	CreateEntity(context.Context, models.Entity) (models.Entity, error)
	UpsertEntity(context.Context, models.Entity) (models.Entity, error)
	EntityPairBySymbol(context.Context, string) (models.EntityPair, error)
	CreateEntityPair(context.Context, models.EntityPair) (models.EntityPair, error)
	UpsertEntityPair(context.Context, models.EntityPair) (models.EntityPair, error)
	EntityPairsContainingEntity(context.Context, uuid.UUID) ([]models.EntityPair, error)
	ListEntityPairs(context.Context) ([]models.EntityPair, error)
	ListEntityKnowledgeTerms(context.Context) ([]models.KnowledgeTerm, error)
	UpsertKnowledgeTerm(context.Context, models.KnowledgeTerm) (models.KnowledgeTerm, error)
	ListIndicators(context.Context) ([]models.Indicator, error)
	ListIndicatorKnowledgeTerms(context.Context) ([]models.IndicatorTerm, error)
	UpsertIndicator(context.Context, models.Indicator) (models.Indicator, error)
}

type EntityService struct {
	repository EntityRepository
}

func NewEntityService(repository EntityRepository) *EntityService {
	return &EntityService{repository: repository}
}

func (service *EntityService) ListEntities(ctx context.Context) ([]models.Entity, error) {
	entities, err := service.repository.ListEntities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	return entities, nil
}

func (service *EntityService) ListEntityPairs(ctx context.Context) ([]models.EntityPair, error) {
	pairs, err := service.repository.ListEntityPairs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list entity pairs: %w", err)
	}
	return pairs, nil
}

func (service *EntityService) EntityPairsContaining(ctx context.Context, entityID uuid.UUID) ([]models.EntityPair, error) {
	pairs, err := service.repository.EntityPairsContainingEntity(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("entity pairs containing entity: %w", err)
	}
	return pairs, nil
}

// CreateEntity creates an entity; a duplicate code is rejected rather than
// overwriting the existing entity.
func (service *EntityService) CreateEntity(ctx context.Context, code, name, entityType string) (models.Entity, error) {
	entity, err := validateEntity(code, name, entityType)
	if err != nil {
		return models.Entity{}, err
	}

	if _, err := service.repository.EntityByCode(ctx, entity.Code); err == nil {
		return models.Entity{}, fmt.Errorf("%w: %s", ErrEntityCodeExists, entity.Code)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return models.Entity{}, fmt.Errorf("find entity by code: %w", err)
	}

	id, err := ids.New()
	if err != nil {
		return models.Entity{}, fmt.Errorf("generate entity ID: %w", err)
	}
	entity.ID = id

	created, err := service.repository.CreateEntity(ctx, entity)
	if err != nil {
		return models.Entity{}, fmt.Errorf("create entity: %w", err)
	}
	return created, nil
}

// EnsureEntity upserts by code for the seed script: re-seeding is idempotent
// while name/type improvements still propagate.
func (service *EntityService) EnsureEntity(ctx context.Context, code, name, entityType string) (models.Entity, error) {
	entity, err := validateEntity(code, name, entityType)
	if err != nil {
		return models.Entity{}, err
	}

	id, err := ids.New()
	if err != nil {
		return models.Entity{}, fmt.Errorf("generate entity ID: %w", err)
	}
	entity.ID = id

	upserted, err := service.repository.UpsertEntity(ctx, entity)
	if err != nil {
		return models.Entity{}, fmt.Errorf("upsert entity: %w", err)
	}
	return upserted, nil
}

// CreateEntityPair resolves both sides by code; the symbol is the authoritative
// product symbol (NQ stays NQ) and is supplied explicitly.
func (service *EntityService) CreateEntityPair(
	ctx context.Context,
	baseCode, quoteCode, symbol string,
) (models.EntityPair, error) {
	pair, base, quote, err := service.validatePair(ctx, baseCode, quoteCode, symbol)
	if err != nil {
		return models.EntityPair{}, err
	}

	if _, err := service.repository.EntityPairBySymbol(ctx, pair.Symbol); err == nil {
		return models.EntityPair{}, fmt.Errorf("%w: %s", ErrPairSymbolExists, pair.Symbol)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return models.EntityPair{}, fmt.Errorf("find entity pair by symbol: %w", err)
	}

	id, err := ids.New()
	if err != nil {
		return models.EntityPair{}, fmt.Errorf("generate entity pair ID: %w", err)
	}
	pair.ID = id
	pair.BaseEntityID = base.ID
	pair.QuoteEntityID = quote.ID

	created, err := service.repository.CreateEntityPair(ctx, pair)
	if err != nil {
		return models.EntityPair{}, fmt.Errorf("create entity pair: %w", err)
	}
	return created, nil
}

func (service *EntityService) EnsureEntityPair(
	ctx context.Context,
	baseCode, quoteCode, symbol string,
) (models.EntityPair, error) {
	pair, base, quote, err := service.validatePair(ctx, baseCode, quoteCode, symbol)
	if err != nil {
		return models.EntityPair{}, err
	}

	id, err := ids.New()
	if err != nil {
		return models.EntityPair{}, fmt.Errorf("generate entity pair ID: %w", err)
	}
	pair.ID = id
	pair.BaseEntityID = base.ID
	pair.QuoteEntityID = quote.ID

	upserted, err := service.repository.UpsertEntityPair(ctx, pair)
	if err != nil {
		return models.EntityPair{}, fmt.Errorf("upsert entity pair: %w", err)
	}
	return upserted, nil
}

// ListKnowledgeTerms returns only the phrases linked to an entity.
func (service *EntityService) ListKnowledgeTerms(
	ctx context.Context,
) ([]models.KnowledgeTerm, error) {
	terms, err := service.repository.ListEntityKnowledgeTerms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list knowledge terms: %w", err)
	}
	return terms, nil
}

// Re-running with a different entity relinks the phrase; name and type
// together identify the row.
func (service *EntityService) EnsureKnowledgeTerm(
	ctx context.Context,
	name, termType, entityCode string,
) (models.KnowledgeTerm, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.KnowledgeTerm{}, ErrKnowledgeTermNameRequired
	}
	termType = strings.TrimSpace(termType)
	if termType == "" {
		return models.KnowledgeTerm{}, ErrKnowledgeTermTypeRequired
	}

	entity, err := service.repository.EntityByCode(ctx, strings.TrimSpace(entityCode))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.KnowledgeTerm{}, fmt.Errorf("%w: %s", ErrEntityNotFound, entityCode)
	}
	if err != nil {
		return models.KnowledgeTerm{}, fmt.Errorf("find knowledge term entity: %w", err)
	}

	id, err := ids.New()
	if err != nil {
		return models.KnowledgeTerm{}, fmt.Errorf("generate knowledge term ID: %w", err)
	}

	upserted, err := service.repository.UpsertKnowledgeTerm(ctx, models.KnowledgeTerm{
		ID:       id,
		Name:     name,
		Type:     termType,
		EntityID: entity.ID,
	})
	if err != nil {
		return models.KnowledgeTerm{}, fmt.Errorf("upsert knowledge term: %w", err)
	}
	return upserted, nil
}

// EnsureIndicator upserts by name and entity for the seed script:
// re-seeding is idempotent while type improvements still propagate.
func (service *EntityService) EnsureIndicator(
	ctx context.Context,
	name, indicatorType, entityCode string,
) (models.Indicator, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Indicator{}, ErrIndicatorNameRequired
	}
	indicatorType = strings.TrimSpace(indicatorType)
	if indicatorType == "" {
		return models.Indicator{}, ErrIndicatorTypeRequired
	}

	entity, err := service.repository.EntityByCode(ctx, strings.TrimSpace(entityCode))
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Indicator{}, fmt.Errorf("%w: %s", ErrEntityNotFound, entityCode)
	}
	if err != nil {
		return models.Indicator{}, fmt.Errorf("find indicator entity: %w", err)
	}

	id, err := ids.New()
	if err != nil {
		return models.Indicator{}, fmt.Errorf("generate indicator ID: %w", err)
	}

	upserted, err := service.repository.UpsertIndicator(ctx, models.Indicator{
		ID:       id,
		Name:     name,
		EntityID: entity.ID,
		Type:     indicatorType,
	})
	if err != nil {
		return models.Indicator{}, fmt.Errorf("upsert indicator: %w", err)
	}
	return upserted, nil
}

// EnsureIndicatorTerm links a phrase to an indicator under the indicator's
// entity. Name and type together identify the row, so an already-seeded
// phrase gains its indicator link in place.
func (service *EntityService) EnsureIndicatorTerm(
	ctx context.Context,
	name, termType string,
	indicator models.Indicator,
) (models.KnowledgeTerm, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.KnowledgeTerm{}, ErrKnowledgeTermNameRequired
	}
	termType = strings.TrimSpace(termType)
	if termType == "" {
		return models.KnowledgeTerm{}, ErrKnowledgeTermTypeRequired
	}

	id, err := ids.New()
	if err != nil {
		return models.KnowledgeTerm{}, fmt.Errorf("generate knowledge term ID: %w", err)
	}

	upserted, err := service.repository.UpsertKnowledgeTerm(ctx, models.KnowledgeTerm{
		ID:          id,
		Name:        name,
		Type:        termType,
		EntityID:    indicator.EntityID,
		IndicatorID: indicator.ID,
	})
	if err != nil {
		return models.KnowledgeTerm{}, fmt.Errorf("upsert indicator knowledge term: %w", err)
	}
	return upserted, nil
}

func (service *EntityService) validatePair(
	ctx context.Context,
	baseCode, quoteCode, symbol string,
) (models.EntityPair, models.Entity, models.Entity, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return models.EntityPair{}, models.Entity{}, models.Entity{}, ErrPairSymbolRequired
	}

	baseCode = strings.TrimSpace(baseCode)
	quoteCode = strings.TrimSpace(quoteCode)
	if baseCode == quoteCode {
		return models.EntityPair{}, models.Entity{}, models.Entity{}, ErrPairEntitiesMustDiffer
	}

	base, err := service.repository.EntityByCode(ctx, baseCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.EntityPair{}, models.Entity{}, models.Entity{}, fmt.Errorf(
			"%w: %s", ErrEntityNotFound, baseCode,
		)
	}
	if err != nil {
		return models.EntityPair{}, models.Entity{}, models.Entity{}, fmt.Errorf("find base entity: %w", err)
	}

	quote, err := service.repository.EntityByCode(ctx, quoteCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.EntityPair{}, models.Entity{}, models.Entity{}, fmt.Errorf(
			"%w: %s", ErrEntityNotFound, quoteCode,
		)
	}
	if err != nil {
		return models.EntityPair{}, models.Entity{}, models.Entity{}, fmt.Errorf("find quote entity: %w", err)
	}

	return models.EntityPair{Symbol: symbol}, base, quote, nil
}

func validateEntity(code, name, entityType string) (models.Entity, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return models.Entity{}, ErrEntityCodeRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Entity{}, ErrEntityNameRequired
	}
	entityType = strings.TrimSpace(entityType)
	if entityType == "" {
		return models.Entity{}, ErrEntityTypeRequired
	}
	return models.Entity{Code: code, Name: name, Type: entityType}, nil
}

package repositories

import (
	"context"
	"fmt"

	"github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	entitydb "github.com/Rahmannugar/macro-terminal/server/internal/entities/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EntityRepository struct {
	queries *entitydb.Queries
}

func NewEntityRepository(pool *pgxpool.Pool) *EntityRepository {
	return &EntityRepository{queries: entitydb.New(pool)}
}

func (repository *EntityRepository) ListEntities(ctx context.Context) ([]models.Entity, error) {
	rows, err := repository.queries.ListEntities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	entities := make([]models.Entity, 0, len(rows))
	for _, row := range rows {
		entities = append(entities, mapEntity(row))
	}
	return entities, nil
}

func (repository *EntityRepository) EntityByID(ctx context.Context, id uuid.UUID) (models.Entity, error) {
	row, err := repository.queries.GetEntityByID(ctx, id)
	if err != nil {
		return models.Entity{}, fmt.Errorf("get entity by ID: %w", err)
	}
	return mapEntity(row), nil
}

func (repository *EntityRepository) EntityByCode(ctx context.Context, code string) (models.Entity, error) {
	row, err := repository.queries.GetEntityByCode(ctx, code)
	if err != nil {
		return models.Entity{}, fmt.Errorf("get entity by code: %w", err)
	}
	return mapEntity(row), nil
}

func (repository *EntityRepository) CreateEntity(ctx context.Context, entity models.Entity) (models.Entity, error) {
	row, err := repository.queries.CreateEntity(ctx, entitydb.CreateEntityParams{
		ID:   entity.ID,
		Code: entity.Code,
		Name: entity.Name,
		Type: entity.Type,
	})
	if err != nil {
		return models.Entity{}, fmt.Errorf("create entity: %w", err)
	}
	return mapEntity(row), nil
}

func (repository *EntityRepository) UpsertEntity(ctx context.Context, entity models.Entity) (models.Entity, error) {
	row, err := repository.queries.UpsertEntity(ctx, entitydb.UpsertEntityParams{
		ID:   entity.ID,
		Code: entity.Code,
		Name: entity.Name,
		Type: entity.Type,
	})
	if err != nil {
		return models.Entity{}, fmt.Errorf("upsert entity: %w", err)
	}
	return mapEntity(row), nil
}

func (repository *EntityRepository) ListEntityPairs(ctx context.Context) ([]models.EntityPair, error) {
	rows, err := repository.queries.ListEntityPairs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list entity pairs: %w", err)
	}
	pairs := make([]models.EntityPair, 0, len(rows))
	for _, row := range rows {
		pairs = append(pairs, mapEntityPair(row))
	}
	return pairs, nil
}

func (repository *EntityRepository) EntityPairByID(ctx context.Context, id uuid.UUID) (models.EntityPair, error) {
	row, err := repository.queries.GetEntityPairByID(ctx, id)
	if err != nil {
		return models.EntityPair{}, fmt.Errorf("get entity pair by ID: %w", err)
	}
	return mapEntityPair(row), nil
}

func (repository *EntityRepository) EntityPairBySymbol(ctx context.Context, symbol string) (models.EntityPair, error) {
	row, err := repository.queries.GetEntityPairBySymbol(ctx, symbol)
	if err != nil {
		return models.EntityPair{}, fmt.Errorf("get entity pair by symbol: %w", err)
	}
	return mapEntityPair(row), nil
}

func (repository *EntityRepository) EntityPairsContainingEntity(ctx context.Context, entityID uuid.UUID) ([]models.EntityPair, error) {
	rows, err := repository.queries.ListEntityPairsContainingEntity(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("list entity pairs containing entity: %w", err)
	}
	pairs := make([]models.EntityPair, 0, len(rows))
	for _, row := range rows {
		pairs = append(pairs, mapEntityPair(row))
	}
	return pairs, nil
}

func (repository *EntityRepository) CreateEntityPair(ctx context.Context, pair models.EntityPair) (models.EntityPair, error) {
	row, err := repository.queries.CreateEntityPair(ctx, entitydb.CreateEntityPairParams{
		ID:            pair.ID,
		BaseEntityID:  pair.BaseEntityID,
		QuoteEntityID: pair.QuoteEntityID,
		Symbol:        pair.Symbol,
	})
	if err != nil {
		return models.EntityPair{}, fmt.Errorf("create entity pair: %w", err)
	}
	return mapEntityPair(row), nil
}

func (repository *EntityRepository) UpsertEntityPair(ctx context.Context, pair models.EntityPair) (models.EntityPair, error) {
	row, err := repository.queries.UpsertEntityPair(ctx, entitydb.UpsertEntityPairParams{
		ID:            pair.ID,
		BaseEntityID:  pair.BaseEntityID,
		QuoteEntityID: pair.QuoteEntityID,
		Symbol:        pair.Symbol,
	})
	if err != nil {
		return models.EntityPair{}, fmt.Errorf("upsert entity pair: %w", err)
	}
	return mapEntityPair(row), nil
}

func (repository *EntityRepository) EntityPairsByUser(ctx context.Context, userID uuid.UUID) ([]models.EntityPair, error) {
	rows, err := repository.queries.ListEntityPairsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list entity pairs by user: %w", err)
	}
	pairs := make([]models.EntityPair, 0, len(rows))
	for _, row := range rows {
		pairs = append(pairs, mapEntityPair(row))
	}
	return pairs, nil
}

func (repository *EntityRepository) UserIDsByEntityPair(ctx context.Context, entityPairID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := repository.queries.ListUserIDsByEntityPair(ctx, entityPairID)
	if err != nil {
		return nil, fmt.Errorf("list user IDs by entity pair: %w", err)
	}
	return rows, nil
}

func (repository *EntityRepository) SubscribeUserAsset(ctx context.Context, userID, entityPairID uuid.UUID) error {
	if err := repository.queries.SubscribeUserAsset(ctx, entitydb.SubscribeUserAssetParams{
		UserID:       userID,
		EntityPairID: entityPairID,
	}); err != nil {
		return fmt.Errorf("subscribe user asset: %w", err)
	}
	return nil
}

func (repository *EntityRepository) UnsubscribeUserAsset(ctx context.Context, userID, entityPairID uuid.UUID) error {
	if err := repository.queries.UnsubscribeUserAsset(ctx, entitydb.UnsubscribeUserAssetParams{
		UserID:       userID,
		EntityPairID: entityPairID,
	}); err != nil {
		return fmt.Errorf("unsubscribe user asset: %w", err)
	}
	return nil
}

func (repository *EntityRepository) ListEntityKnowledgeTerms(
	ctx context.Context,
) ([]models.KnowledgeTerm, error) {
	rows, err := repository.queries.ListEntityKnowledgeTerms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list entity knowledge terms: %w", err)
	}
	terms := make([]models.KnowledgeTerm, 0, len(rows))
	for _, row := range rows {
		terms = append(terms, models.KnowledgeTerm{
			ID:          row.ID,
			Name:        row.Name,
			Type:        row.Type,
			EntityID:    fromNullableUUID(row.EntityID),
			IndicatorID: fromNullableUUID(row.IndicatorID),
			CreatedAt:   row.CreatedAt.Time,
			UpdatedAt:   row.UpdatedAt.Time,
		})
	}
	return terms, nil
}

func (repository *EntityRepository) UpsertKnowledgeTerm(
	ctx context.Context,
	term models.KnowledgeTerm,
) (models.KnowledgeTerm, error) {
	row, err := repository.queries.UpsertKnowledgeTerm(ctx, entitydb.UpsertKnowledgeTermParams{
		ID:          term.ID,
		Name:        term.Name,
		Type:        term.Type,
		EntityID:    toNullableUUID(term.EntityID),
		IndicatorID: toNullableUUID(term.IndicatorID),
	})
	if err != nil {
		return models.KnowledgeTerm{}, fmt.Errorf("upsert knowledge term: %w", err)
	}
	return models.KnowledgeTerm{
		ID:          row.ID,
		Name:        row.Name,
		Type:        row.Type,
		EntityID:    fromNullableUUID(row.EntityID),
		IndicatorID: fromNullableUUID(row.IndicatorID),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

func (repository *EntityRepository) ListIndicators(ctx context.Context) ([]models.Indicator, error) {
	rows, err := repository.queries.ListIndicators(ctx)
	if err != nil {
		return nil, fmt.Errorf("list indicators: %w", err)
	}
	indicators := make([]models.Indicator, 0, len(rows))
	for _, row := range rows {
		indicators = append(indicators, models.Indicator{
			ID:        row.ID,
			Name:      row.Name,
			EntityID:  row.EntityID,
			Type:      row.Type,
			CreatedAt: row.CreatedAt.Time,
			UpdatedAt: row.UpdatedAt.Time,
		})
	}
	return indicators, nil
}

func (repository *EntityRepository) ListIndicatorKnowledgeTerms(
	ctx context.Context,
) ([]models.IndicatorTerm, error) {
	rows, err := repository.queries.ListIndicatorKnowledgeTerms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list indicator knowledge terms: %w", err)
	}
	terms := make([]models.IndicatorTerm, 0, len(rows))
	for _, row := range rows {
		terms = append(terms, models.IndicatorTerm{
			Name:        row.Name,
			IndicatorID: fromNullableUUID(row.IndicatorID),
		})
	}
	return terms, nil
}

func (repository *EntityRepository) UpsertIndicator(
	ctx context.Context,
	indicator models.Indicator,
) (models.Indicator, error) {
	row, err := repository.queries.UpsertIndicator(ctx, entitydb.UpsertIndicatorParams{
		ID:       indicator.ID,
		Name:     indicator.Name,
		EntityID: indicator.EntityID,
		Type:     indicator.Type,
	})
	if err != nil {
		return models.Indicator{}, fmt.Errorf("upsert indicator: %w", err)
	}
	return models.Indicator{
		ID:        row.ID,
		Name:      row.Name,
		EntityID:  row.EntityID,
		Type:      row.Type,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

// An unlinked column becomes the zero UUID.
func fromNullableUUID(value pgtype.UUID) uuid.UUID {
	if !value.Valid {
		return uuid.Nil
	}
	return uuid.UUID(value.Bytes)
}

func toNullableUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func mapEntity(row entitydb.Entity) models.Entity {
	return models.Entity{
		ID:        row.ID,
		Code:      row.Code,
		Name:      row.Name,
		Type:      row.Type,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func mapEntityPair(row entitydb.EntityPair) models.EntityPair {
	return models.EntityPair{
		ID:            row.ID,
		BaseEntityID:  row.BaseEntityID,
		QuoteEntityID: row.QuoteEntityID,
		Symbol:        row.Symbol,
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
}

package adminconfig

import (
	"net/http"
	"time"

	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type entityJSON struct {
	ID        uuid.UUID `json:"id" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	Code      string    `json:"code" example:"EUR"`
	Name      string    `json:"name" example:"Euro"`
	Type      string    `json:"type" example:"currency"`
	CreatedAt time.Time `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt time.Time `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
}

type entityListResponse struct {
	Entities   []entityJSON `json:"entities"`
	NextCursor *string      `json:"nextCursor"`
}

type entityResponse struct {
	Entity entityJSON `json:"entity"`
}

type entityPairJSON struct {
	ID            uuid.UUID `json:"id" example:"c1e0f4d2-6a1b-4f3e-9d7c-2b8a5e6f4c10"`
	Symbol        string    `json:"symbol" example:"EURUSD"`
	BaseEntityID  uuid.UUID `json:"baseEntityId" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	QuoteEntityID uuid.UUID `json:"quoteEntityId" example:"3f2a9c81-5b7d-4e2a-8c6f-1d9e4b7a2f30"`
	CreatedAt     time.Time `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt     time.Time `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
}

type entityPairListResponse struct {
	EntityPairs []entityPairJSON `json:"entityPairs"`
	NextCursor  *string          `json:"nextCursor"`
}

type entityPairResponse struct {
	EntityPair entityPairJSON `json:"entityPair"`
}

type indicatorJSON struct {
	ID        uuid.UUID `json:"id" example:"6b5f8e2d-4c9a-4f1e-b8d7-3a2c6e9f1b40"`
	Name      string    `json:"name" example:"Nonfarm Payrolls"`
	Type      string    `json:"type" example:"labor"`
	EntityID  uuid.UUID `json:"entityId" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	CreatedAt time.Time `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt time.Time `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
}

type indicatorListResponse struct {
	Indicators []indicatorJSON `json:"indicators"`
	NextCursor *string         `json:"nextCursor"`
}

type indicatorResponse struct {
	Indicator indicatorJSON `json:"indicator"`
}

type knowledgeTermJSON struct {
	ID          uuid.UUID  `json:"id" example:"8c4a2f6e-1d3b-4e7a-9c5f-6b8d0e2a4f71"`
	Name        string     `json:"name" example:"nonfarm payrolls"`
	Type        string     `json:"type" example:"indicator_alias"`
	EntityID    *uuid.UUID `json:"entityId" example:"9d1e4a63-8f6d-4b9c-1f4e-7a553f8b2a60"`
	IndicatorID *uuid.UUID `json:"indicatorId" example:"6b5f8e2d-4c9a-4f1e-b8d7-3a2c6e9f1b40"`
	CreatedAt   time.Time  `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt   time.Time  `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
}

type knowledgeTermListResponse struct {
	KnowledgeTerms []knowledgeTermJSON `json:"knowledgeTerms"`
	NextCursor     *string             `json:"nextCursor"`
}

type knowledgeTermResponse struct {
	KnowledgeTerm knowledgeTermJSON `json:"knowledgeTerm"`
}

type createEntityRequest struct {
	Code string `json:"code" example:"EUR"`
	Name string `json:"name" example:"Euro"`
	Type string `json:"type" example:"currency"`
}

type createEntityPairRequest struct {
	BaseCode  string `json:"baseCode" example:"EUR"`
	QuoteCode string `json:"quoteCode" example:"USD"`
	Symbol    string `json:"symbol" example:"EURUSD"`
}

type createIndicatorRequest struct {
	Name       string `json:"name" example:"Nonfarm Payrolls"`
	Type       string `json:"type" example:"labor"`
	EntityCode string `json:"entityCode" example:"US"`
}

type createKnowledgeTermRequest struct {
	Name        string     `json:"name" example:"nonfarm payrolls"`
	Type        string     `json:"type" example:"indicator_alias"`
	EntityCode  string     `json:"entityCode" example:"US"`
	IndicatorID *uuid.UUID `json:"indicatorId" example:"6b5f8e2d-4c9a-4f1e-b8d7-3a2c6e9f1b40"`
}

func newEntityJSON(entity entitymodels.Entity) entityJSON {
	return entityJSON{
		ID:        entity.ID,
		Code:      entity.Code,
		Name:      entity.Name,
		Type:      entity.Type,
		CreatedAt: entity.CreatedAt,
		UpdatedAt: entity.UpdatedAt,
	}
}

// @Summary List entities.
// @Description Returns reference entities newest first. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} entityListResponse "The entities and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/entities [get]
func listEntities(entities Entities) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, cursor, ok := parseListQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := entities.ListEntitiesPage(ctx.Request.Context(), cursor, limit)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		response := entityListResponse{Entities: make([]entityJSON, 0, len(rows))}
		for _, row := range rows {
			response.Entities = append(response.Entities, newEntityJSON(row))
		}
		response.NextCursor = encodeNextCursor(next)
		ctx.JSON(http.StatusOK, response)
	}
}

// @Summary Create an entity.
// @Description Stores a reference entity. The code must not already exist.
// @Tags admin
// @Param body body createEntityRequest true "The entity to store."
// @Success 200 {object} entityResponse "The stored entity."
// @Failure 400 {object} openapi.Error "A field is missing or the body is not valid JSON."
// @Failure 409 {object} openapi.Error "An entity with that code already exists."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/entities [post]
func createEntity(entities Entities) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request createEntityRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		entity, err := entities.CreateEntity(ctx.Request.Context(), request.Code, request.Name, request.Type)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, entityResponse{Entity: newEntityJSON(entity)})
	}
}

func newEntityPairJSON(pair entitymodels.EntityPair) entityPairJSON {
	return entityPairJSON{
		ID:            pair.ID,
		Symbol:        pair.Symbol,
		BaseEntityID:  pair.BaseEntityID,
		QuoteEntityID: pair.QuoteEntityID,
		CreatedAt:     pair.CreatedAt,
		UpdatedAt:     pair.UpdatedAt,
	}
}

// @Summary List entity pairs.
// @Description Returns tradable pairs newest first. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} entityPairListResponse "The pairs and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/entity-pairs [get]
func listEntityPairs(entities Entities) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, cursor, ok := parseListQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := entities.ListEntityPairsPage(ctx.Request.Context(), cursor, limit)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		response := entityPairListResponse{EntityPairs: make([]entityPairJSON, 0, len(rows))}
		for _, row := range rows {
			response.EntityPairs = append(response.EntityPairs, newEntityPairJSON(row))
		}
		response.NextCursor = encodeNextCursor(next)
		ctx.JSON(http.StatusOK, response)
	}
}

// @Summary Create an entity pair.
// @Description Stores a pair of existing entities under an explicit product symbol. Both codes must exist and differ.
// @Tags admin
// @Param body body createEntityPairRequest true "The pair to store."
// @Success 200 {object} entityPairResponse "The stored pair."
// @Failure 400 {object} openapi.Error "A field is missing or a code is unknown."
// @Failure 409 {object} openapi.Error "An entity pair with that symbol already exists."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/entity-pairs [post]
func createEntityPair(entities Entities) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request createEntityPairRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		pair, err := entities.CreateEntityPair(ctx.Request.Context(), request.BaseCode, request.QuoteCode, request.Symbol)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, entityPairResponse{EntityPair: newEntityPairJSON(pair)})
	}
}

func newIndicatorJSON(indicator entitymodels.Indicator) indicatorJSON {
	return indicatorJSON{
		ID:        indicator.ID,
		Name:      indicator.Name,
		Type:      indicator.Type,
		EntityID:  indicator.EntityID,
		CreatedAt: indicator.CreatedAt,
		UpdatedAt: indicator.UpdatedAt,
	}
}

// @Summary List economic indicators.
// @Description Returns indicators newest first. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} indicatorListResponse "The indicators and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/indicators [get]
func listIndicators(entities Entities) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, cursor, ok := parseListQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := entities.ListIndicatorsPage(ctx.Request.Context(), cursor, limit)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		response := indicatorListResponse{Indicators: make([]indicatorJSON, 0, len(rows))}
		for _, row := range rows {
			response.Indicators = append(response.Indicators, newIndicatorJSON(row))
		}
		response.NextCursor = encodeNextCursor(next)
		ctx.JSON(http.StatusOK, response)
	}
}

// @Summary Create an economic indicator.
// @Description Stores an indicator under an existing entity. The name must be unique within that entity.
// @Tags admin
// @Param body body createIndicatorRequest true "The indicator to store."
// @Success 200 {object} indicatorResponse "The stored indicator."
// @Failure 400 {object} openapi.Error "A field is missing or the entity code is unknown."
// @Failure 409 {object} openapi.Error "That entity already has an indicator with this name."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/indicators [post]
func createIndicator(entities Entities) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request createIndicatorRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		indicator, err := entities.CreateIndicator(ctx.Request.Context(), request.Name, request.Type, request.EntityCode)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, indicatorResponse{Indicator: newIndicatorJSON(indicator)})
	}
}

func newKnowledgeTermJSON(term entitymodels.KnowledgeTerm) knowledgeTermJSON {
	return knowledgeTermJSON{
		ID:          term.ID,
		Name:        term.Name,
		Type:        term.Type,
		EntityID:    optionalUUID(term.EntityID),
		IndicatorID: optionalUUID(term.IndicatorID),
		CreatedAt:   term.CreatedAt,
		UpdatedAt:   term.UpdatedAt,
	}
}

func optionalUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// @Summary List knowledge terms.
// @Description Returns classification phrases newest first. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} knowledgeTermListResponse "The terms and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/knowledge-terms [get]
func listKnowledgeTerms(entities Entities) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, cursor, ok := parseListQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := entities.ListKnowledgeTermsPage(ctx.Request.Context(), cursor, limit)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		response := knowledgeTermListResponse{KnowledgeTerms: make([]knowledgeTermJSON, 0, len(rows))}
		for _, row := range rows {
			response.KnowledgeTerms = append(response.KnowledgeTerms, newKnowledgeTermJSON(row))
		}
		response.NextCursor = encodeNextCursor(next)
		ctx.JSON(http.StatusOK, response)
	}
}

// @Summary Create a knowledge term.
// @Description Stores a classification phrase linked to an entity, an indicator, or both. An indicator link carries the indicator's entity automatically.
// @Tags admin
// @Param body body createKnowledgeTermRequest true "The term to store."
// @Success 200 {object} knowledgeTermResponse "The stored term."
// @Failure 400 {object} openapi.Error "A field is missing, no link was given, or a reference is unknown."
// @Failure 409 {object} openapi.Error "A term with that name and type already exists."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/knowledge-terms [post]
func createKnowledgeTerm(entities Entities) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request createKnowledgeTermRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		term, err := entities.CreateKnowledgeTerm(
			ctx.Request.Context(),
			request.Name,
			request.Type,
			request.EntityCode,
			request.IndicatorID,
		)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, knowledgeTermResponse{KnowledgeTerm: newKnowledgeTermJSON(term)})
	}
}

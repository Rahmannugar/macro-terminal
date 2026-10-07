package adminconfig

import (
	"encoding/json"
	"net/http"
	"time"

	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type sourceJSON struct {
	ID        uuid.UUID `json:"id" example:"5a1c3e7b-9d2f-4c8a-b6e1-7f4d2a9c3e50"`
	Name      string    `json:"name" example:"BLS"`
	Type      string    `json:"type" example:"api"`
	CreatedAt time.Time `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt time.Time `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
}

type sourceListResponse struct {
	Sources    []sourceJSON `json:"sources"`
	NextCursor *string      `json:"nextCursor"`
}

type sourceResponse struct {
	Source sourceJSON `json:"source"`
}

type sourceConfigurationJSON struct {
	ID        uuid.UUID       `json:"id" example:"2e8f4a6c-7b3d-4e9a-8c1f-5d0b6a4e9f22"`
	SourceID  uuid.UUID       `json:"sourceId" example:"5a1c3e7b-9d2f-4c8a-b6e1-7f4d2a9c3e50"`
	Type      string          `json:"type" example:"api"`
	Config    json.RawMessage `json:"config" swaggertype:"object"`
	CreatedAt time.Time       `json:"createdAt" example:"2026-10-06T09:00:00Z"`
	UpdatedAt time.Time       `json:"updatedAt" example:"2026-10-06T09:00:00Z"`
	LastRunAt *time.Time      `json:"lastRunAt" example:"2026-10-06T09:00:00Z"`
}

type sourceConfigurationListResponse struct {
	SourceConfigurations []sourceConfigurationJSON `json:"sourceConfigurations"`
	NextCursor           *string                   `json:"nextCursor"`
}

type sourceConfigurationResponse struct {
	SourceConfiguration sourceConfigurationJSON `json:"sourceConfiguration"`
}

type createSourceRequest struct {
	Name string `json:"name" example:"BLS"`
	Type string `json:"type" example:"api"`
}

type createSourceConfigurationRequest struct {
	SourceID uuid.UUID       `json:"sourceId" example:"5a1c3e7b-9d2f-4c8a-b6e1-7f4d2a9c3e50"`
	Type     string          `json:"type" example:"api"`
	Config   json.RawMessage `json:"config" swaggertype:"object"`
}

type updateSourceConfigurationRequest struct {
	Config json.RawMessage `json:"config" swaggertype:"object"`
}

func newSourceJSON(source sourcemodels.Source) sourceJSON {
	return sourceJSON{
		ID:        source.ID,
		Name:      source.Name,
		Type:      source.Type,
		CreatedAt: source.CreatedAt,
		UpdatedAt: source.UpdatedAt,
	}
}

func newSourceConfigurationJSON(configuration sourcemodels.SourceConfiguration) sourceConfigurationJSON {
	return sourceConfigurationJSON{
		ID:        configuration.ID,
		SourceID:  configuration.SourceID,
		Type:      configuration.Type,
		Config:    configuration.Config,
		CreatedAt: configuration.CreatedAt,
		UpdatedAt: configuration.UpdatedAt,
		LastRunAt: configuration.LastRunAt,
	}
}

// @Summary List sources.
// @Description Returns logical sources newest first. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} sourceListResponse "The sources and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/sources [get]
func listSources(sources Sources) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, cursor, ok := parseListQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := sources.ListSourcesPage(ctx.Request.Context(), cursor, limit)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		response := sourceListResponse{Sources: make([]sourceJSON, 0, len(rows))}
		for _, row := range rows {
			response.Sources = append(response.Sources, newSourceJSON(row))
		}
		response.NextCursor = encodeNextCursor(next)
		ctx.JSON(http.StatusOK, response)
	}
}

// @Summary Create a source.
// @Description Stores a logical source. The name must not already exist.
// @Tags admin
// @Param body body createSourceRequest true "The source to store."
// @Success 200 {object} sourceResponse "The stored source."
// @Failure 400 {object} openapi.Error "A field is missing or the body is not valid JSON."
// @Failure 409 {object} openapi.Error "A source with that name already exists."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/sources [post]
func createSource(sources Sources) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request createSourceRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		source, err := sources.CreateSource(ctx.Request.Context(), request.Name, request.Type)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, sourceResponse{Source: newSourceJSON(source)})
	}
}

// @Summary List source configurations.
// @Description Returns configuration payloads newest first. Pass the returned nextCursor to fetch the next page.
// @Tags admin
// @Param limit query int false "Page size between 1 and 100. Defaults to 25."
// @Param cursor query string false "Opaque cursor returned as nextCursor by the previous page."
// @Success 200 {object} sourceConfigurationListResponse "The configurations and the cursor for the next page, if any."
// @Failure 400 {object} openapi.Error "The limit or cursor is invalid."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/source-configurations [get]
func listSourceConfigurations(sources Sources) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		limit, cursor, ok := parseListQuery(ctx)
		if !ok {
			return
		}
		rows, next, err := sources.ListSourceConfigurationsPage(ctx.Request.Context(), cursor, limit)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		response := sourceConfigurationListResponse{
			SourceConfigurations: make([]sourceConfigurationJSON, 0, len(rows)),
		}
		for _, row := range rows {
			response.SourceConfigurations = append(response.SourceConfigurations, newSourceConfigurationJSON(row))
		}
		response.NextCursor = encodeNextCursor(next)
		ctx.JSON(http.StatusOK, response)
	}
}

// @Summary Create a source configuration.
// @Description Stores a configuration payload for an existing source. The payload must be a JSON object with a url the server is allowed to fetch, may not carry secret values, and the url may not repeat within one source and type.
// @Tags admin
// @Param body body createSourceConfigurationRequest true "The configuration to store."
// @Success 200 {object} sourceConfigurationResponse "The stored configuration."
// @Failure 400 {object} openapi.Error "The payload is invalid or its url points at a refused address."
// @Failure 404 {object} openapi.Error "No source has that id."
// @Failure 409 {object} openapi.Error "That source already has a configuration for this url."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/source-configurations [post]
func createSourceConfiguration(sources Sources) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request createSourceConfigurationRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		configuration, err := sources.CreateSourceConfiguration(
			ctx.Request.Context(),
			request.SourceID,
			request.Type,
			request.Config,
		)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, sourceConfigurationResponse{
			SourceConfiguration: newSourceConfigurationJSON(configuration),
		})
	}
}

// @Summary Replace a source configuration payload.
// @Description Replaces the stored payload of an existing configuration. Its source and type stay fixed, the new payload must pass the same validation as a create, and its url must be one the server may fetch.
// @Tags admin
// @Param id path string true "Source configuration id (UUID)."
// @Param body body updateSourceConfigurationRequest true "The payload to store."
// @Success 200 {object} sourceConfigurationResponse "The updated configuration."
// @Failure 400 {object} openapi.Error "The id is not a UUID, or the payload is invalid or refused."
// @Failure 404 {object} openapi.Error "No configuration has that id."
// @Failure 500 {object} openapi.Error "The request could not be completed."
// @Router /api/v1/admin/source-configurations/{id} [patch]
func updateSourceConfiguration(sources Sources) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id, err := uuid.Parse(ctx.Param("id"))
		if err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_id", "The id must be a UUID.")
			return
		}
		var request updateSourceConfigurationRequest
		if err := ctx.ShouldBindJSON(&request); err != nil {
			writeFailure(ctx, http.StatusBadRequest, "invalid_request", "The request body must be valid JSON.")
			return
		}
		configuration, err := sources.UpdateSourceConfiguration(ctx.Request.Context(), id, request.Config)
		if err != nil {
			writeConfigFailure(ctx, err)
			return
		}
		ctx.JSON(http.StatusOK, sourceConfigurationResponse{
			SourceConfiguration: newSourceConfigurationJSON(configuration),
		})
	}
}

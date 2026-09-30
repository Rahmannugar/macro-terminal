package openapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type operation struct {
	Method, Path, Summary, Tag, Request, SuccessCode, Success, SuccessDescription string
	Errors                                                                        map[string]string
}

// Document builds the deterministic OpenAPI contract served by the API and
// written to openapi.json for review and stale-output checks.
func Document() ([]byte, error) {
	operations := allOperations()
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Path == operations[j].Path {
			return operations[i].Method < operations[j].Method
		}
		return operations[i].Path < operations[j].Path
	})
	paths := map[string]any{}
	for _, endpoint := range operations {
		path, _ := paths[endpoint.Path].(map[string]any)
		if path == nil {
			path = map[string]any{}
			paths[endpoint.Path] = path
		}
		description := endpoint.SuccessDescription
		if description == "" {
			description = successDescription(endpoint.SuccessCode)
		}
		success := map[string]any{"description": description}
		if endpoint.Success != "" {
			success["content"] = jsonContent(schemaReference(endpoint.Success), exampleFor(endpoint.Success))
		}
		responses := operationResponses(endpoint, success)
		for code, alternateDescription := range endpoint.Errors {
			responses[code] = map[string]any{"description": alternateDescription}
		}
		tag := endpoint.Tag
		if tag == "" {
			tag = tagFor(endpoint.Path)
		}
		operationDocument := map[string]any{"summary": endpoint.Summary, "tags": []string{tag}, "responses": responses}
		if endpoint.Request != "" {
			operationDocument["requestBody"] = map[string]any{
				"required": true,
				"content":  jsonContent(schemaReference(endpoint.Request), exampleFor(endpoint.Request)),
			}
		}
		path[endpoint.Method] = operationDocument
	}
	document := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Macro Terminal API",
			"version":     "0.1.0",
			"description": "Macro Terminal macro intelligence and fundamental research terminal API.",
		},
		"servers": []map[string]string{
			{"url": "http://localhost:8081", "description": "Local development"},
		},
		"paths": paths,
		"components": map[string]any{
			"schemas":   schemas(),
			"responses": errorResponses(),
		},
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode OpenAPI document: %w", err)
	}
	return append(encoded, '\n'), nil
}

func allOperations() []operation {
	groups := [][]operation{healthOperations()}
	var result []operation
	for _, group := range groups {
		result = append(result, group...)
	}
	return result
}

func schemas() map[string]any {
	result := map[string]any{
		"Error": object([]string{"error"}, map[string]any{
			"error": object([]string{"code"}, map[string]any{
				"code":    map[string]any{"type": "string"},
				"message": map[string]any{"type": "string"},
			}),
		}),
	}
	for _, additions := range []map[string]any{healthSchemas()} {
		for name, schema := range additions {
			result[name] = schema
		}
	}
	return result
}

func errorResponses() map[string]any {
	return map[string]any{
		"BadRequest": response("The request was malformed.", schemaReference("Error")),
		"RateLimited": response(
			"Too many requests. Retry after the indicated delay.",
			schemaReference("Error"),
		),
		"InternalServer": response("The request could not be completed.", schemaReference("Error")),
	}
}

func operationResponses(endpoint operation, success map[string]any) map[string]any {
	responses := map[string]any{endpoint.SuccessCode: success}
	if _, declared := endpoint.Errors["400"]; !declared {
		responses["400"] = map[string]any{"$ref": "#/components/responses/BadRequest"}
	}
	if _, declared := endpoint.Errors["429"]; !declared {
		responses["429"] = map[string]any{"$ref": "#/components/responses/RateLimited"}
	}
	if _, declared := endpoint.Errors["500"]; !declared {
		responses["500"] = map[string]any{"$ref": "#/components/responses/InternalServer"}
	}
	return responses
}

func response(description, reference string) map[string]any {
	return map[string]any{
		"description": description,
		"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": reference}}},
	}
}

func jsonContent(reference string, example any) map[string]any {
	schema := map[string]any{"$ref": reference}
	if example != nil {
		schema = map[string]any{"allOf": []any{map[string]any{"$ref": reference}}, "example": example}
	}
	return map[string]any{"application/json": map[string]any{"schema": schema}}
}

func schemaReference(name string) string {
	return "#/components/schemas/" + name
}

func exampleFor(name string) any {
	if example := healthExampleFor(name); example != nil {
		return example
	}
	return nil
}

func object(required []string, properties map[string]any) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func successDescription(code string) string {
	switch code {
	case "200":
		return "The request completed successfully."
	case "201":
		return "The resource was created."
	case "204":
		return "The request completed successfully."
	default:
		return "The request completed successfully."
	}
}

func tagFor(path string) string {
	segment, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if segment == "" {
		return "default"
	}
	return segment
}

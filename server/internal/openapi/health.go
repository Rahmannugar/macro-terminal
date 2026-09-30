package openapi

func healthOperations() []operation {
	return []operation{
		{Method: "get", Path: "/health/live", Summary: "Report whether the API process is alive.", SuccessCode: "200", Success: "Health"},
		{Method: "get", Path: "/health/ready", Summary: "Report whether PostgreSQL is reachable.", SuccessCode: "200", Success: "Health"},
	}
}

func healthSchemas() map[string]any {
	return map[string]any{
		"Health": object([]string{"status"}, map[string]any{"status": map[string]any{"type": "string", "example": "ok"}}),
	}
}

func healthExampleFor(name string) map[string]any {
	if name == "Health" {
		return map[string]any{"status": "ok"}
	}
	return nil
}

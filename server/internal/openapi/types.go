package openapi

// Error is the envelope every failed request returns: a machine-readable
// code under a human-readable message.
type Error struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code" example:"invalid_request"`
	Message string `json:"message" example:"The request was malformed."`
}

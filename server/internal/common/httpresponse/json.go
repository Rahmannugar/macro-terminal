package httpresponse

import (
	"encoding/json"
	"net/http"
)

type Error struct {
	Error ErrorDetails `json:"error"`
}

type ErrorDetails struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(response http.ResponseWriter, status int, body any) error {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	return json.NewEncoder(response).Encode(body)
}

func WriteError(response http.ResponseWriter, status int, code, message string) error {
	return WriteJSON(response, status, Error{
		Error: ErrorDetails{Code: code, Message: message},
	})
}

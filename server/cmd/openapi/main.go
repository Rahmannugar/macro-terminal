package main

import (
	"fmt"
	"os"

	"github.com/Rahmannugar/macro-terminal/server/internal/openapi"
)

const documentPath = "server/internal/openapi/openapi.json"

func main() {
	document, err := openapi.Document()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build OpenAPI document: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(documentPath, document, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write OpenAPI document: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("wrote", documentPath)
}

package search

import (
	"encoding/base64"
	"errors"
	"strconv"
)

var ErrCursorInvalid = errors.New("search cursor is invalid")

// Cursor carries a position in the similarity ranking, which lives only in the vector index.
type Cursor struct {
	Offset int
}

func EncodeCursor(cursor Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(cursor.Offset)))
}

func DecodeCursor(value string) (*Cursor, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrCursorInvalid
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 {
		return nil, ErrCursorInvalid
	}
	return &Cursor{Offset: offset}, nil
}

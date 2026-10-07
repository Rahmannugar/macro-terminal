// Package paging is the shared keyset-pagination contract: an opaque
// cursor over a (sort timestamp, id) pair, used by every list endpoint.
package paging

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const cursorSeparator = "."

var ErrCursorInvalid = errors.New("cursor is invalid")

// Cursor is the sort timestamp of the previous page's last row plus that
// row's id, which breaks timestamp ties.
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

func EncodeCursor(cursor Cursor) string {
	payload := strconv.FormatInt(cursor.At.UnixNano(), 10) + cursorSeparator + cursor.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func DecodeCursor(value string) (*Cursor, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrCursorInvalid
	}
	parts := strings.Split(string(decoded), cursorSeparator)
	if len(parts) != 2 {
		return nil, ErrCursorInvalid
	}
	nanoseconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, ErrCursorInvalid
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, ErrCursorInvalid
	}
	return &Cursor{At: time.Unix(0, nanoseconds).UTC(), ID: id}, nil
}

package models

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

type ListCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func EncodeCursor(cursor ListCursor) string {
	payload := strconv.FormatInt(cursor.CreatedAt.UnixNano(), 10) + cursorSeparator + cursor.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func DecodeCursor(value string) (*ListCursor, error) {
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
	return &ListCursor{CreatedAt: time.Unix(0, nanoseconds).UTC(), ID: id}, nil
}

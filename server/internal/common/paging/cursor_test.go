package paging

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	cursor := Cursor{
		At: time.Unix(0, 1759750000123456789).UTC(),
		ID: uuid.New(),
	}

	decoded, err := DecodeCursor(EncodeCursor(cursor))
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if decoded == nil {
		t.Fatal("DecodeCursor() returned nil cursor")
	}
	if !decoded.At.Equal(cursor.At) {
		t.Errorf("At = %v, want %v", decoded.At, cursor.At)
	}
	if decoded.ID != cursor.ID {
		t.Errorf("ID = %v, want %v", decoded.ID, cursor.ID)
	}
}

func TestDecodeCursorEmptyIsFirstPage(t *testing.T) {
	decoded, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if decoded != nil {
		t.Errorf("DecodeCursor() = %+v, want nil", decoded)
	}
}

func TestDecodeCursorRejectsGarbage(t *testing.T) {
	cases := []string{
		"not-base64!!",
		"bm90LWEtY3Vyc29y",               // decodes to "not-a-cursor" without separator
		"MTIzLm5vdC1hLXV1aWQ",            // "123.not-a-uuid"
		"bm90LW51bWJlci57MTIzLWNscw",     // "not-number.{123-cls"
		"YWJjZGVmZ2hpamtsbW5vcA.bm90LWE", // "abcdefghijklmnop" with junk id
	}
	for _, value := range cases {
		if _, err := DecodeCursor(value); !errors.Is(err, ErrCursorInvalid) {
			t.Errorf("DecodeCursor(%q) error = %v, want ErrCursorInvalid", value, err)
		}
	}
}

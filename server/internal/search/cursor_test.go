package search

import (
	"errors"
	"testing"
)

func TestCursorDecodes(t *testing.T) {
	encoded := EncodeCursor(Cursor{Offset: 75})

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded == nil || decoded.Offset != 75 {
		t.Fatalf("decoded cursor = %+v, want offset 75", decoded)
	}

	if decoded, err = DecodeCursor(""); err != nil || decoded != nil {
		t.Fatalf("decode empty cursor = (%+v, %v), want (nil, nil)", decoded, err)
	}
}

func TestCursorRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"not-base64!!", EncodeCursor(Cursor{Offset: -1}), EncodeCursor(Cursor{Offset: 1}) + "garbage"} {
		if _, err := DecodeCursor(value); !errors.Is(err, ErrCursorInvalid) {
			t.Errorf("decode %q error = %v, want ErrCursorInvalid", value, err)
		}
	}
}

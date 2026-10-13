package webhook

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateAcceptsSlackAndDiscordEndpoints(t *testing.T) {
	cases := []string{
		"https://hooks.slack.com/services/T00/B00/xxxx",
		"https://discord.com/api/webhooks/1234567890/abcdefghijklmnopqrstuvwx-yz0123456789",
		"https://discordapp.com/api/webhooks/1234567890/abcdefghijklmnopqrstuvwx-yz0123456789",
	}
	for _, raw := range cases {
		clean, hint, err := Validate(raw)
		if err != nil {
			t.Errorf("Validate(%q) error = %v, want accepted", raw, err)
			continue
		}
		if clean == "" {
			t.Errorf("Validate(%q) clean = empty, want the canonical URL", raw)
		}
		if strings.Contains(hint, "abcdefghijklmnop") {
			t.Errorf("hint = %q, must never echo the secret path", hint)
		}
		if !strings.HasSuffix(hint, raw[len(raw)-4:]) {
			t.Errorf("hint = %q, want the last path characters for recognition", hint)
		}
	}
}

func TestValidateRejectsUnsafeEndpoints(t *testing.T) {
	cases := []string{
		"",
		"http://hooks.slack.com/services/T000/B000/XXXX",
		"https://evil.test/api/webhooks/1234567890/abcdefghijklmnopqrstuvwx-yz0123456789",
		"https://hooks.slack.com/reactions/add",
		"https://discord.com/api/webhooks/1",
		"https://hooks.slack.com/services/T000/B000/XXXX?token=abc",
		"https://user@hooks.slack.com/services/T000/B000/XXXX",
	}
	for _, raw := range cases {
		if _, _, err := Validate(raw); err == nil {
			t.Errorf("Validate(%q) accepted, want rejection", raw)
		}
	}
}

func TestCipherRoundTripsAndDetectsTampering(t *testing.T) {
	cipher, err := NewCipher(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	endpoint := "https://hooks.slack.com/services/T000/B000/XXXX"

	nonce, ciphertext, err := cipher.Encrypt(endpoint)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	plain, err := cipher.Decrypt(nonce, ciphertext)
	if err != nil || plain != endpoint {
		t.Fatalf("decrypt = %q, %v; want the endpoint", plain, err)
	}

	ciphertext[len(ciphertext)-1] ^= 0xFF
	if _, err := cipher.Decrypt(nonce, ciphertext); err == nil {
		t.Error("tampered ciphertext decrypted, want failure")
	}
}

func TestNewCipherRequiresKeyMaterial(t *testing.T) {
	if _, err := NewCipher([]byte("short")); err == nil {
		t.Error("short key accepted, want failure")
	}
}

func TestMessageRendersPerChannel(t *testing.T) {
	slack, err := Message(ChannelSlack, "new_article", "Fed holds rates steady", "BTC/USD")
	if err != nil {
		t.Fatalf("slack message: %v", err)
	}
	if !strings.Contains(string(slack), `"text"`) || !strings.Contains(string(slack), "BTC/USD") {
		t.Errorf("slack body = %s, want text payload with the pair", slack)
	}

	discord, err := Message(ChannelDiscord, "new_calendar_event", "CPI", "EUR/USD")
	if err != nil {
		t.Fatalf("discord message: %v", err)
	}
	if !strings.Contains(string(discord), `"content"`) || !strings.Contains(string(discord), "Calendar event") {
		t.Errorf("discord body = %s, want content payload with the label", discord)
	}

	if _, err := Message("email", "new_article", "t", "BTC/USD"); err == nil {
		t.Error("unknown channel accepted, want failure")
	}
}

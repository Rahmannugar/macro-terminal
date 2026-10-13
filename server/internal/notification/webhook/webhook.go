// Package webhook validates, encrypts, and formats Slack/Discord webhook
// endpoints. The endpoint URL is a secret: callers only ever store the
// ciphertext plus a masked hint for the UI.
package webhook

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	ChannelSlack   = "slack"
	ChannelDiscord = "discord"
)

// ErrInvalidWebhookURL rejects anything that is not an https Slack or
// Discord webhook endpoint; the URL is later fetched server-side, so hosts
// outside this allowlist would be a server-side request forgery.
var ErrInvalidWebhookURL = errors.New("webhook URL must be an https Slack or Discord webhook endpoint")

var allowedEndpoints = map[string]string{
	"hooks.slack.com":    "/services/",
	"discord.com":        "/api/webhooks/",
	"discordapp.com":     "/api/webhooks/",
	"canary.discord.com": "/api/webhooks/",
	"ptb.discord.com":    "/api/webhooks/",
}

// Validate checks the raw URL and returns its canonical form plus a masked
// hint safe to show in the UI (host and the last path characters only).
func Validate(raw string) (clean string, hint string, err error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", ErrInvalidWebhookURL
	}
	host := strings.ToLower(parsed.Hostname())
	pathPrefix, allowed := allowedEndpoints[host]
	if !allowed || !strings.HasPrefix(parsed.Path, pathPrefix) {
		return "", "", ErrInvalidWebhookURL
	}
	tail := parsed.Path
	if separator := strings.LastIndex(tail, "/"); separator >= 0 && separator+1 < len(tail) {
		tail = tail[separator+1:]
	}
	if len(tail) < 4 {
		return "", "", ErrInvalidWebhookURL
	}
	hint = parsed.Hostname() + "/…" + tail[len(tail)-4:]
	return parsed.String(), hint, nil
}

// Cipher encrypts webhook URLs with a purpose-derived key so the channel
// secrets never use raw authentication key material.
type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(masterSecret []byte) (*Cipher, error) {
	if len(masterSecret) < 32 {
		return nil, fmt.Errorf("webhook encryption requires at least 32 bytes of key material")
	}
	mac := hmac.New(sha256.New, masterSecret)
	_, _ = mac.Write([]byte("macro-terminal/notification-webhooks/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("create webhook cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create webhook AEAD: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(plaintext string) (nonce []byte, ciphertext []byte, err error) {
	nonce = make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("seal webhook URL: %w", err)
	}
	return nonce, c.aead.Seal(nil, nonce, []byte(plaintext), nil), nil
}

func (c *Cipher) Decrypt(nonce []byte, ciphertext []byte) (string, error) {
	plain, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("open webhook URL: %w", err)
	}
	return string(plain), nil
}

// Message renders the JSON body posted to the channel endpoint. The title
// links back to the Macro Terminal page for the subject, and articles carry
// their lead image when one is known.
func Message(channel string, alertType string, title string, pairSymbol string, pageURL string, imageURL string) ([]byte, error) {
	label := "Notification"
	switch alertType {
	case "new_article":
		label = "New article"
	case "new_calendar_event":
		label = "Calendar event"
	}
	context := fmt.Sprintf("%s · %s", label, pairSymbol)
	const brandColor = "#087CEC"
	switch channel {
	case ChannelSlack:
		attachment := map[string]any{
			"color":      brandColor,
			"title":      title,
			"title_link": pageURL,
			"text":       context,
			"footer":     "Macro Terminal",
		}
		if imageURL != "" {
			attachment["image_url"] = imageURL
		}
		return json.Marshal(map[string]any{
			"text":        fmt.Sprintf("Macro Terminal · %s", context),
			"attachments": []map[string]any{attachment},
		})
	case ChannelDiscord:
		embed := map[string]any{
			"title":       title,
			"url":         pageURL,
			"description": context,
			"color":       0x087CEC,
			"footer":      map[string]string{"text": "Macro Terminal"},
		}
		if imageURL != "" {
			embed["image"] = map[string]string{"url": imageURL}
		}
		return json.Marshal(map[string]any{"embeds": []map[string]any{embed}})
	default:
		return nil, fmt.Errorf("unknown webhook channel %q", channel)
	}
}

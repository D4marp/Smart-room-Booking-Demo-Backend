// Package integration delivers integration events to an external webhook.
package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

const maxAttempts = 50

// Sign returns the value of the X-Signature header:
// "sha256=" + hex(HMAC-SHA256(secret, timestamp + "." + body)).
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func backoff(attempts int) time.Duration {
	d := 5 * time.Second << min(attempts, 7) // 5s, 10s, ... capped ~10m
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

type pending struct {
	id        int64
	eventType string
	bookingID string
	roomCode  string
	payload   string
	createdAt int64
	attempts  int
}

// StartWebhookDispatcher pushes events to url until ctx is cancelled.
// Delivery is at-least-once and ordered per booking: if one event of a
// booking fails, later events of the same booking wait for its retry.
func StartWebhookDispatcher(ctx context.Context, db *sql.DB, url, secret string) {
	client := &http.Client{Timeout: 10 * time.Second}
	log.Printf("Integration webhook dispatcher started -> %s", url)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dispatchBatch(ctx, db, client, url, secret)
		}
	}
}

func dispatchBatch(ctx context.Context, db *sql.DB, client *http.Client, url, secret string) {
	now := time.Now().UnixMilli()
	rows, err := db.QueryContext(ctx,
		`SELECT id, event_type, COALESCE(booking_id,''), COALESCE(room_code,''), payload, created_at, attempts
		 FROM integration_events
		 WHERE delivered_at IS NULL AND attempts < ? AND next_attempt_at <= ?
		 ORDER BY id ASC LIMIT 50`, maxAttempts, now)
	if err != nil {
		log.Printf("webhook: query failed: %v", err)
		return
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.eventType, &p.bookingID, &p.roomCode, &p.payload, &p.createdAt, &p.attempts); err == nil {
			batch = append(batch, p)
		}
	}
	rows.Close()

	blocked := map[string]bool{}
	for _, p := range batch {
		if p.bookingID != "" && blocked[p.bookingID] {
			continue
		}
		if err := deliver(ctx, client, url, secret, p); err != nil {
			log.Printf("webhook: event %d (%s) failed: %v", p.id, p.eventType, err)
			db.ExecContext(ctx,
				"UPDATE integration_events SET attempts = attempts + 1, next_attempt_at = ? WHERE id = ?",
				time.Now().Add(backoff(p.attempts)).UnixMilli(), p.id)
			if p.bookingID != "" {
				blocked[p.bookingID] = true
			}
			continue
		}
		db.ExecContext(ctx, "UPDATE integration_events SET delivered_at = ? WHERE id = ?",
			time.Now().UnixMilli(), p.id)
	}
}

func deliver(ctx context.Context, client *http.Client, url, secret string, p pending) error {
	body, err := json.Marshal(map[string]any{
		"id":         p.id,
		"type":       p.eventType,
		"occurredAt": time.UnixMilli(p.createdAt).Format(time.RFC3339),
		"roomCode":   p.roomCode,
		"bookingId":  p.bookingID,
		"data":       json.RawMessage(p.payload),
	})
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Id", strconv.FormatInt(p.id, 10))
	req.Header.Set("X-Event-Type", p.eventType)
	req.Header.Set("X-Timestamp", ts)
	if secret != "" {
		req.Header.Set("X-Signature", Sign(secret, ts, body))
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

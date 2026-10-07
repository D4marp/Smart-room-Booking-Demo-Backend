package integration

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Runs only when TEST_MYSQL_DSN points at a disposable database that already
// has the integration_events table (run the handlers integration test first).
func TestDispatchBatch(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec("DELETE FROM integration_events")
	now := time.Now().UnixMilli()
	db.Exec(`INSERT INTO integration_events (event_type, booking_id, room_code, payload, created_at, next_attempt_at)
	         VALUES ('booking.checked_out','b1','Q1.01.03','{"x":1}',?,?)`, now, now)

	fail := true
	var gotSig, gotTS string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig, gotTS = r.Header.Get("X-Signature"), r.Header.Get("X-Timestamp")
		gotBody, _ = io.ReadAll(r.Body)
		if fail {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	client := srv.Client()
	dispatchBatch(context.Background(), db, client, srv.URL, "secret")
	var delivered sql.NullInt64
	var attempts int
	db.QueryRow("SELECT delivered_at, attempts FROM integration_events").Scan(&delivered, &attempts)
	if delivered.Valid || attempts != 1 {
		t.Fatalf("after 500: delivered=%v attempts=%d, want undelivered/1", delivered, attempts)
	}

	// make it due again and succeed
	fail = false
	db.Exec("UPDATE integration_events SET next_attempt_at = 0")
	dispatchBatch(context.Background(), db, client, srv.URL, "secret")
	db.QueryRow("SELECT delivered_at, attempts FROM integration_events").Scan(&delivered, &attempts)
	if !delivered.Valid {
		t.Fatal("event should be delivered after 2xx")
	}
	if gotSig != Sign("secret", gotTS, gotBody) {
		t.Fatalf("signature mismatch: %s", gotSig)
	}
}

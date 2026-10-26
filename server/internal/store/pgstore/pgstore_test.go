package pgstore

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/server/internal/store"
	"github.com/tolgayayci/muretto/server/internal/store/storetest"
)

// Postgres tests are opt-in: set MURETTO_PG_URL to a disposable database.
// The suite creates rows with unique identities and never drops anything.
func pgURL(t *testing.T) string {
	url := os.Getenv("MURETTO_PG_URL")
	if url == "" {
		t.Skip("MURETTO_PG_URL not set; skipping Postgres tests")
	}
	return url
}

func open(t *testing.T) *Store {
	t.Helper()
	s, _, err := Open(context.Background(), pgURL(t), Options{MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestMigrationsLoad(t *testing.T) {
	ms, err := loadMigrations()
	if err != nil || len(ms) == 0 || ms[0].version != 1 {
		t.Fatalf("migrations: %v %v", ms, err)
	}
}

func TestPoolConfigTransactionPooler(t *testing.T) {
	cfg, err := PoolConfig("postgres://u:p@localhost:6543/db", Options{MaxConns: 3})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 3 || cfg.ConnConfig.DefaultQueryExecMode.String() != "exec" {
		t.Fatalf("pool config: %d %s", cfg.MaxConns, cfg.ConnConfig.DefaultQueryExecMode)
	}
	if _, err := PoolConfig("::not a url::", Options{}); err == nil || err.Error() != "invalid database URL" {
		t.Fatalf("bad URL error: %v", err)
	}
}

func TestConformance(t *testing.T) {
	s := open(t)
	if _, err := Migrate(context.Background(), s.Pool()); err != nil { // idempotent
		t.Fatal(err)
	}
	storetest.Run(t, func(*testing.T) store.Store { return s })
}

func TestListen(t *testing.T) {
	s := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var got []store.Event
	go s.Listen(ctx, pgURL(t), func(evs []store.Event) {
		mu.Lock()
		got = append(got, evs...)
		mu.Unlock()
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	time.Sleep(500 * time.Millisecond)
	u, err := s.UpsertUser(ctx, "test:listen-"+time.Now().Format(time.RFC3339Nano), "lena", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ingest(ctx, store.IngestInput{UserID: u.ID, Developer: "lena", BatchID: "b", Now: time.Now(),
		Records: []store.Record{{Kind: store.KindResource, Key: "r:listen", Rev: "abcd", Data: []byte(`{}`)}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, e := range got {
			if e.UserID == u.ID && e.EntityID == "r:listen" {
				mu.Unlock()
				return
			}
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no notification received")
}

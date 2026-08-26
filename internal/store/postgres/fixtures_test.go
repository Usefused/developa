package postgres

import (
	"context"
	"testing"
	"time"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
)

func catalogFixture(t *testing.T) (*Store, *queryCounter) {
	t.Helper()
	store, counter := unmigratedFixture(t)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureRepository(context.Background(), domain.Repository{ID: "repo", Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	return store, counter
}

func unmigratedFixture(t *testing.T) (*Store, *queryCounter) {
	t.Helper()
	cfg := integrationConfig(t)
	admin, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	schema := "catalog_" + fingerprint(t.Name() + time.Now().String())[:20]
	if _, err := admin.pool.Exec(context.Background(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal("could not create isolated test schema")
	}
	t.Cleanup(func() {
		_, _ = admin.pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	return schemaStore(t, cfg, schema)
}

func schemaStore(t *testing.T, cfg Config, schema string) (*Store, *queryCounter) {
	t.Helper()
	parsed, err := poolConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	counter := &queryCounter{}
	parsed.ConnConfig.RuntimeParams["search_path"] = schema
	parsed.ConnConfig.Tracer = counter
	store, err := openPool(context.Background(), parsed)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store, counter
}

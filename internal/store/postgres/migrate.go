package postgres

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/001_catalog.sql
var initialCatalog string

//go:embed migrations/002_publication_identity.sql
var publicationIdentity string

//go:embed migrations/003_intelligence.sql
var intelligenceCatalog string

//go:embed migrations/004_analysis_jobs.sql
var analysisJobs string

//go:embed migrations/005_commit_analysis_cache.sql
var commitAnalysisCache string

//go:embed migrations/006_function_reviews.sql
var functionReviews string

//go:embed migrations/007_saved_feature_snapshot.sql
var savedFeatureSnapshot string

//go:embed migrations/008_saved_answers.sql
var savedAnswers string

//go:embed migrations/009_workspaces.sql
var workspaces string

//go:embed migrations/010_file_source.sql
var fileSource string

//go:embed migrations/011_implementation_candidates.sql
var implementationCandidates string

type migration struct {
	version int
	sql     string
}

var catalogMigrations = []migration{{1, initialCatalog}, {2, publicationIdentity}, {3, intelligenceCatalog}, {4, analysisJobs}, {5, commitAnalysisCache}, {6, functionReviews}, {7, savedFeatureSnapshot}, {8, savedAnswers}, {9, workspaces}, {10, fileSource}, {11, implementationCandidates}}

func (s *Store) Migrate(ctx context.Context) (err error) {
	ctx, done := operation(ctx, "postgres.migrate")
	defer func() { done(err) }()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError(err)
	}
	defer rollback(tx)
	if err := migrateCatalog(ctx, tx); err != nil {
		return databaseError(err)
	}
	return databaseError(tx.Commit(ctx))
}

func migrateCatalog(ctx context.Context, tx pgx.Tx) error {
	// A transaction-level lock serializes first boot across multiple API instances.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(84679927940321)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS developa_schema_migrations
		(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, migration := range catalogMigrations {
		if err := applyMigration(ctx, tx, migration); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, tx pgx.Tx, migration migration) error {
	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM developa_schema_migrations WHERE version=$1)`, migration.version).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	if _, err := tx.Exec(ctx, migration.sql); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO developa_schema_migrations (version) VALUES ($1)`, migration.version)
	return err
}

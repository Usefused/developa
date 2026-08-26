package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/trace"
)

var hexID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type intelligenceMutation func(context.Context, pgx.Tx, int) error

func (s *Store) intelligenceWrite(ctx context.Context, repositoryID, snapshotID, action string, execution domain.Execution, counts map[string]int, mutate intelligenceMutation) (err error) {
	ctx, done := operation(ctx, action)
	defer func() { done(err) }()
	if !validExecution(execution, "completed") {
		return domain.ErrInvalidInput
	}
	traceExecution(ctx, repositoryID, execution)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError(err)
	}
	defer rollback(tx)
	var total int
	err = tx.QueryRow(ctx, `SELECT (metadata->>'symbol_count')::integer FROM developa_snapshots
		WHERE repository_id=$1 AND id=$2 FOR NO KEY UPDATE`, repositoryID, snapshotID).Scan(&total)
	if err != nil {
		return databaseError(err)
	}
	if err := validateAnalysisLease(ctx, tx, repositoryID, snapshotID, execution); err != nil {
		return err
	}
	if err := mutate(ctx, tx, total); err != nil {
		return intelligenceError(err)
	}
	if err := validateAnalysisLease(ctx, tx, repositoryID, snapshotID, execution); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, repositoryID, &snapshotID, execution, "completed", counts); err != nil {
		return databaseError(err)
	}
	err = databaseError(tx.Commit(ctx))
	if err == nil {
		trace.SpanFromContext(ctx).AddEvent("intelligence.published")
	}
	return err
}

func intelligenceError(err error) error {
	if errors.Is(err, domain.ErrInvalidInput) || errors.Is(err, domain.ErrInvalidModelOutput) || errors.Is(err, domain.ErrBusy) {
		return err
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23503" {
		return domain.ErrInvalidModelOutput
	}
	return databaseError(err)
}

func (s *Store) SaveFeatures(ctx context.Context, repositoryID string, run domain.FeatureRun, features []domain.Feature, execution domain.Execution) error {
	if err := validateFeatures(run, features); err != nil {
		return err
	}
	return s.intelligenceWrite(ctx, repositoryID, run.SnapshotID, "postgres.save_features", execution,
		map[string]int{"features": len(features)}, func(ctx context.Context, tx pgx.Tx, total int) error {
			return insertFeatureRun(ctx, tx, repositoryID, run, features, total)
		})
}

func validateFeatures(run domain.FeatureRun, features []domain.Feature) error {
	if !validFeatureRunIdentity(run) {
		return domain.ErrInvalidInput
	}
	if run.Status != "completed" && run.Status != "partial" {
		return domain.ErrInvalidInput
	}
	if len(features) > 512 {
		return domain.ErrInvalidInput
	}
	seen := make(map[string]bool)
	for _, feature := range features {
		if seen[feature.ID] || !validFeature(feature) {
			return domain.ErrInvalidModelOutput
		}
		seen[feature.ID] = true
	}
	return nil
}

func validFeature(feature domain.Feature) bool {
	if !hexID.MatchString(feature.ID) || feature.Status != "inferred" {
		return false
	}
	if !boundedText(feature.Title, 160) || !boundedText(feature.Summary, 2000) {
		return false
	}
	return len(feature.Evidence) > 0 && len(feature.Evidence) <= 16
}

func insertFeatureRun(ctx context.Context, tx pgx.Tx, repositoryID string, run domain.FeatureRun, features []domain.Feature, total int) error {
	run, err := prepareFeatureRun(ctx, tx, repositoryID, run, len(features), total)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(run)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO developa_feature_runs (repository_id,snapshot_id,id,metadata) VALUES ($1,$2,$3,$4)`, repositoryID, run.SnapshotID, run.ID, payload)
	if err != nil {
		return err
	}
	if err := copyInheritedFeatures(ctx, tx, repositoryID, run); err != nil {
		return err
	}
	if err := copyFeatures(ctx, tx, repositoryID, run, features); err != nil {
		return err
	}
	return publishFeatureRun(ctx, tx, repositoryID, run)
}

func validFeatureRunIdentity(run domain.FeatureRun) bool {
	if !auditToken.MatchString(run.ID) || !hexID.MatchString(run.SnapshotID) || !validModel(run.Model) {
		return false
	}
	return run.ParentRunID == "" || auditToken.MatchString(run.ParentRunID)
}

func prepareFeatureRun(ctx context.Context, tx pgx.Tx, repositoryID string, run domain.FeatureRun, added, total int) (domain.FeatureRun, error) {
	if err := validateFeatureProgress(run, total); err != nil {
		return run, err
	}
	inherited, err := inheritedFeatureCount(ctx, tx, repositoryID, run)
	if err != nil {
		return run, err
	}
	run.FeatureCount, run.Limitations = inherited+added, nonNil(run.Limitations)
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	return run, nil
}

func validateFeatureProgress(run domain.FeatureRun, total int) error {
	if run.CachedBatches < 0 || run.ModelCalls < 0 {
		return domain.ErrInvalidInput
	}
	if run.TotalSymbols != total || run.AnalyzedSymbols < 0 || run.AnalyzedSymbols > total {
		return domain.ErrInvalidInput
	}
	if run.Status == "completed" && run.AnalyzedSymbols < total {
		return domain.ErrInvalidInput
	}
	return nil
}

func inheritedFeatureCount(ctx context.Context, tx pgx.Tx, repositoryID string, run domain.FeatureRun) (int, error) {
	if run.ParentRunID == "" {
		return 0, nil
	}
	var payload []byte
	err := tx.QueryRow(ctx, `SELECT r.metadata FROM developa_feature_runs r JOIN developa_snapshots s
		ON s.repository_id=r.repository_id AND s.id=r.snapshot_id AND s.latest_feature_run_id=r.id
		WHERE s.repository_id=$1 AND s.id=$2 AND r.id=$3`, repositoryID, run.SnapshotID, run.ParentRunID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrBusy
	}
	if err != nil {
		return 0, err
	}
	var parent domain.FeatureRun
	if err := decodeJSON(payload, &parent); err != nil {
		return 0, err
	}
	if err := validateFeatureParent(parent, run); err != nil {
		return 0, err
	}
	return parent.FeatureCount, nil
}

func validateFeatureParent(parent, run domain.FeatureRun) error {
	if run.CachedBatches < parent.CachedBatches || run.ModelCalls < parent.ModelCalls {
		return domain.ErrInvalidInput
	}
	if parent.Model != run.Model || parent.TotalSymbols != run.TotalSymbols {
		return domain.ErrInvalidInput
	}
	if parent.AnalyzedSymbols >= parent.TotalSymbols || run.AnalyzedSymbols < parent.AnalyzedSymbols {
		return domain.ErrInvalidInput
	}
	return nil
}

func copyInheritedFeatures(ctx context.Context, tx pgx.Tx, repositoryID string, run domain.FeatureRun) error {
	if run.ParentRunID == "" {
		return nil
	}
	// The snapshot lock makes the parent check and accumulated publication atomic.
	// Copying inside SQL retains arbitrarily many prior rows without loading them in Go.
	_, err := tx.Exec(ctx, `INSERT INTO developa_features
		(repository_id,snapshot_id,run_id,id,title,summary,status,evidence_ids)
		SELECT repository_id,snapshot_id,$4,id,title,summary,status,evidence_ids FROM developa_features
		WHERE repository_id=$1 AND snapshot_id=$2 AND run_id=$3`, repositoryID, run.SnapshotID, run.ParentRunID, run.ID)
	return err
}

func copyFeatures(ctx context.Context, tx pgx.Tx, repositoryID string, run domain.FeatureRun, features []domain.Feature) error {
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"developa_features"},
		[]string{"repository_id", "snapshot_id", "run_id", "id", "title", "summary", "status", "evidence_ids"},
		pgx.CopyFromSlice(len(features), func(i int) ([]any, error) {
			feature := features[i]
			return []any{repositoryID, run.SnapshotID, run.ID, feature.ID, feature.Title, feature.Summary, feature.Status, citationIDs(feature.Evidence)}, nil
		}))
	return err
}

func publishFeatureRun(ctx context.Context, tx pgx.Tx, repositoryID string, run domain.FeatureRun) error {
	// Evidence FKs validate every proposed citation in one statement. Reader joins
	// derive names/paths/spans from indexed symbols, never from model-supplied labels.
	_, err := tx.Exec(ctx, `INSERT INTO developa_feature_evidence (repository_id,snapshot_id,run_id,feature_id,symbol_id)
		SELECT DISTINCT f.repository_id,f.snapshot_id,f.run_id,f.id,e.id
		FROM developa_features f CROSS JOIN LATERAL unnest(f.evidence_ids) AS e(id)
		WHERE f.repository_id=$1 AND f.snapshot_id=$2 AND f.run_id=$3`, repositoryID, run.SnapshotID, run.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE developa_snapshots SET latest_feature_run_id=$3 WHERE repository_id=$1 AND id=$2`, repositoryID, run.SnapshotID, run.ID)
	return err
}

func (s *Store) SaveAnswer(ctx context.Context, repositoryID string, answer domain.Answer, execution domain.Execution) error {
	if !validAnswer(answer) {
		return domain.ErrInvalidModelOutput
	}
	return s.intelligenceWrite(ctx, repositoryID, answer.SnapshotID, "postgres.save_answer", execution,
		map[string]int{"answers": 1, "citations": len(answer.Evidence)}, func(ctx context.Context, tx pgx.Tx, _ int) error {
			return insertAnswer(ctx, tx, repositoryID, answer)
		})
}

func validAnswer(answer domain.Answer) bool {
	if answer.ContextKey != "" && !hexID.MatchString(answer.ContextKey) {
		return false
	}
	if !auditToken.MatchString(answer.ID) || !hexID.MatchString(answer.SnapshotID) || !validModel(answer.Model) {
		return false
	}
	if !boundedText(answer.Text, 16000) || len(answer.Evidence) > 16 {
		return false
	}
	return answer.InsufficientEvidence || len(answer.Evidence) > 0
}

func insertAnswer(ctx context.Context, tx pgx.Tx, repositoryID string, answer domain.Answer) error {
	ids := citationIDs(answer.Evidence)
	answer.Evidence = nil
	answer.Limitations = nonNil(answer.Limitations)
	if answer.CreatedAt.IsZero() {
		answer.CreatedAt = time.Now().UTC()
	}
	payload, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO developa_answers (repository_id,snapshot_id,id,metadata,context_key) VALUES ($1,$2,$3,$4::jsonb-'evidence',NULLIF($5,''))`, repositoryID, answer.SnapshotID, answer.ID, payload, answer.ContextKey)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO developa_answer_evidence (repository_id,snapshot_id,answer_id,symbol_id)
		SELECT DISTINCT $1::text,$2::text,$3::text,id FROM unnest($4::text[]) AS e(id)`, repositoryID, answer.SnapshotID, answer.ID, ids)
	return err
}

func citationIDs(citations []domain.Citation) []string {
	ids := make([]string, len(citations))
	for i, citation := range citations {
		ids[i] = citation.SymbolID
	}
	return ids
}

func validModel(model string) bool { return boundedText(model, 200) }

func boundedText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit
}

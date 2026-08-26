package postgres

import (
	"context"

	"developa/internal/domain"
)

var _ domain.RepositoryReader = (*Store)(nil)

func (s *Store) Repositories(ctx context.Context, configuredIDs []string, filter domain.Filter) (page domain.RepositoryPage, err error) {
	ctx, done := operation(ctx, "postgres.repositories")
	defer func() { done(err) }()
	if !validRepositoryFilter(filter) {
		return page, domain.ErrInvalidInput
	}
	filter = boundedFilter(filter)
	page.Limit, page.Offset = filter.Limit, filter.Offset
	var payload []byte
	err = s.pool.QueryRow(ctx, repositoriesSQL, configuredIDs, filter.Query, filter.Limit, filter.Offset).Scan(&page.Total, &payload)
	if err != nil {
		return page, databaseError(err)
	}
	err = decodeJSON(payload, &page.Items)
	return page, err
}

func validRepositoryFilter(filter domain.Filter) bool {
	return filter.Kind == "" && filter.File == "" && validSearchQuery(filter.Query, 200) &&
		filter.Limit >= 0 && filter.Limit <= 100 && filter.Offset >= 0 && filter.Offset <= 100000
}

const repositoriesSQL = `WITH filtered AS MATERIALIZED (
	SELECT r.id,r.name,s.metadata AS snapshot
	FROM developa_repositories r LEFT JOIN developa_snapshots s
	ON s.repository_id=r.id AND s.id=r.latest_snapshot_id
	WHERE r.id=ANY($1::text[]) AND ($2='' OR strpos(lower(r.name),lower($2))>0)
), page AS (
	SELECT id,name,jsonb_build_object('id',id,'name',name,'snapshot',snapshot) AS item
	FROM filtered ORDER BY lower(name),name,id LIMIT $3 OFFSET $4
)
SELECT (SELECT count(*) FROM filtered),
	COALESCE(jsonb_agg(item ORDER BY lower(name),name,id),'[]'::jsonb) FROM page`

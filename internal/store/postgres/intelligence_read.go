package postgres

import (
	"context"
	"strings"
	"unicode/utf8"

	"developa/internal/domain"
)

var _ domain.IntelligenceStore = (*Store)(nil)

func (s *Store) Calls(ctx context.Context, repositoryID, snapshotID string, filter domain.CallFilter) (page domain.CallPage, err error) {
	ctx, done := operation(ctx, "postgres.calls")
	defer func() { done(err) }()
	filter, err = normalizeCalls(filter)
	if err != nil {
		return page, err
	}
	page.Limit, page.Offset = filter.Limit, filter.Offset
	var exists bool
	var payload []byte
	err = s.pool.QueryRow(ctx, callsSQL, repositoryID, snapshotID, filter.SymbolID, filter.Direction, filter.Resolution, filter.Limit, filter.Offset).Scan(&exists, &page.Total, &payload)
	if err := pageError(err, exists); err != nil {
		return page, err
	}
	err = decodeJSON(payload, &page.Items)
	return page, err
}

func (s *Store) Chain(ctx context.Context, repositoryID, snapshotID, symbolID string, options domain.ChainOptions) (chain domain.CallChain, err error) {
	ctx, done := operation(ctx, "postgres.chain")
	defer func() { done(err) }()
	options, err = normalizeChain(options)
	if err != nil {
		return chain, err
	}
	chain = domain.CallChain{SnapshotID: snapshotID, RootID: symbolID, Direction: options.Direction, Depth: options.Depth}
	var exists bool
	var nodes, edges []byte
	err = s.pool.QueryRow(ctx, chainSQL, repositoryID, snapshotID, symbolID, options.Direction, options.Depth, options.Limit).Scan(&exists, &nodes, &edges, &chain.Truncated)
	if err := pageError(err, exists); err != nil {
		return chain, err
	}
	if err := decodeSymbols(nodes, &chain.Nodes); err != nil {
		return chain, err
	}
	err = decodeJSON(edges, &chain.Edges)
	return chain, err
}

func (s *Store) Context(ctx context.Context, repositoryID, snapshotID, query string, limit int) (pack domain.ContextPack, err error) {
	ctx, done := operation(ctx, "postgres.context")
	defer func() { done(err) }()
	if limit < 1 || limit > 20 || !validSearchQuery(query, 2000) {
		return pack, domain.ErrInvalidInput
	}
	pack = domain.ContextPack{RepositoryID: repositoryID, SnapshotID: snapshotID, Query: query}
	var exists bool
	var payload []byte
	err = s.pool.QueryRow(ctx, contextSQL, repositoryID, snapshotID, strings.TrimSpace(query), limit).Scan(&exists, &pack.Total, &payload)
	if err := pageError(err, exists); err != nil {
		return pack, err
	}
	pack.Truncated = pack.Total > limit
	err = decodeSymbols(payload, &pack.Items)
	return pack, err
}

func normalizeCalls(filter domain.CallFilter) (domain.CallFilter, error) {
	if filter.Direction == "" {
		filter.Direction = "out"
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if !validDirection(filter.Direction) || !validResolution(filter.Resolution) {
		return filter, domain.ErrInvalidInput
	}
	if filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 || filter.Offset > 100000 {
		return filter, domain.ErrInvalidInput
	}
	return filter, nil
}

func normalizeChain(options domain.ChainOptions) (domain.ChainOptions, error) {
	if options.Direction == "" {
		options.Direction = "out"
	}
	if options.Depth == 0 {
		options.Depth = 3
	}
	if options.Limit == 0 {
		options.Limit = 50
	}
	if !validDirection(options.Direction) || options.Depth < 1 || options.Depth > 5 {
		return options, domain.ErrInvalidInput
	}
	if options.Limit < 1 || options.Limit > 100 {
		return options, domain.ErrInvalidInput
	}
	return options, nil
}

func validDirection(direction string) bool { return direction == "out" || direction == "in" }

func validSearchQuery(query string, limit int) bool {
	return len(query) <= limit && utf8.ValidString(query) && !strings.ContainsRune(query, 0)
}

func validResolution(resolution string) bool {
	switch resolution {
	case "", "resolved", "unresolved", "external", "builtin":
		return true
	default:
		return false
	}
}

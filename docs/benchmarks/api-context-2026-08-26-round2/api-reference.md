# Preloaded API reference

All operations below are GETs with the supplied bearer credential. Source prefix S=/api/repositories/{repository}/snapshots/{snapshot}; substitute the supplied IDs. URL-encode query values. IDs are 64 lowercase hex characters.

S-relative routes:
- /file?path=<canonical repository-relative path> (required): file metadata, package, imports with spans, overview and completeness. Not a raw-file download.
- /files: q,file,kind,limit(default24,1..100),offset(default0,0..100000). Returns metadata page {items,total,limit,offset}.
- /symbols: q,file,kind,limit(default50,1..100),offset(default0,0..100000). Returns {items,total,limit,offset}; each item has path,symbol and optional saved review.
- /symbols/{symbol}: one declaration detail with path,symbol and optional saved review; no query parameters.
- /calls: symbol_id optional,direction=in|out(defaultout),resolution=resolved|unresolved|external|builtin or omitted,limit(default50,1..100),offset(default0,0..100000). Returns callsite page {items,total,limit,offset}; inspect caller/target IDs, spans, resolution and reason.
- /symbols/{symbol}/chain: direction=in|out(defaultout),depth(default2,1..5),limit(default40,1..100). Bounded nodes/edges,root ID,direction,depth,truncation.
- /flow: symbol_id or feature_id mutually exclusive;depth(default6,1..12),limit(default80,1..100). Bounded nodes/edges, roots, dependencies, cycles, limitations. No selector discovers application roots.
- /context: q(up to2000 UTF-8 bytes),limit(default12,1..20). Bounded lexical source context pack; no inference.
- /details: no query parameters. Snapshot,call_analysis,limitations,diagnostics,exclusions,changes,skipped. Repository-wide response; no file filter or pagination.
- /features: q,limit(default24,1..100),offset(default0,0..100000). Cached inferred feature page and generation metadata; no inference.
- /features/{feature}: saved inferred feature and evidence; no query parameters.
- /function-reviews: symbol_id or callee_of mutually exclusive;limit(default4,1..8),offset(default0,0..100000). Saved review page; no inference.
- /analysis-job: saved background status; no query parameters; does not admit work.

Catalog q limit is 200 UTF-8 bytes; file/path limits4096 bytes. These are search filters; verify returned names/paths instead of assuming exact name matches. Kind values: function,method,struct,interface,interface_method,alias,named_type,field,constant,variable,closure. Omit unused filters and page using totals/offsets.

symbol includes id,source_id,content_hash,kind,name,signature,visibility,span,source,source_truncated. Depending on kind it may include parent_id,receiver,receiver_name,type,parameters,results,type_parameters,fields,values,doc,comment,documentation. documentation has summary,comments,origin,truncated. source is the captured declaration implementation; direct filesystem source reads remain forbidden.

Positions use one-based physical lines and UTF-8 byte columns, zero-based byte offsets, exclusive span ends. Respect null/omitted optional fields, source_truncated, graph truncation, completeness and limitations. Source completeness and call-analysis completeness are separate. Resolved calls are static local bindings, not runtime execution order or build proof. Unresolved calls can lack target IDs; reasons distinguish unsupported bindings. Empty incoming lists alone do not prove code is unused. Features are inferred and require cited evidence.

Keep parsed responses in memory; print useful projections rather than entire large bodies, but count all bytes received. Errors use HTTP status and safe status string; X-Trace-ID is available. Do not change snapshots or use default-workspace aliases. This supplied API briefing replaces all OpenAPI discovery for the experiment.

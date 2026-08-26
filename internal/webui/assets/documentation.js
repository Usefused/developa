export function sourceSummary(symbol) {
  return symbol.documentation?.summary ?? [symbol.doc,symbol.comment].filter(Boolean).join('\n\n');
}

export function documentationWarning(symbol) {
  const doc = symbol.documentation;
  if (!doc?.truncated) return '';
  return 'Incomplete: some comments or source were not captured.';
}

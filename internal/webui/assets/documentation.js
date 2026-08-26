export function sourceSummary(symbol) {
  return symbol.documentation?.summary ?? [symbol.doc,symbol.comment].filter(Boolean).join('\n\n');
}

export function documentationNote(symbol) {
  const doc = symbol.documentation;
  if (!doc) return 'Declaration comments; inline comments are unavailable in this record.';
  const origin = doc.origin === 'captured_excerpt' ? 'Compiled from saved comments and the captured excerpt.' : 'Compiled from declaration docs and inline comments, in source order.';
  return `${origin} No AI used.${doc.truncated ? ' Incomplete: some comments or source were not captured.' : ''}`;
}

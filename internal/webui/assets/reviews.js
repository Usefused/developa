export function reviewable(symbol) {
  return ['function','method','closure','interface_method'].includes(symbol.kind);
}

export function reviewRequest(id, callees = false, offset = 0) {
  return {...(callees ? {callee_of:id} : {symbol_id:id}),limit:4,offset};
}

export function reviewRange(page) {
  const offset = page.options.offset ?? 0;
  const start = page.items.length ? offset+1 : 0;
  return `${start}–${offset+page.items.length} of ${page.total}`;
}

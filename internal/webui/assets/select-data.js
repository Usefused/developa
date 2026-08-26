const sequence = Symbol.for('developa.search-select.sequence');

// All instances of the shared React control use this counter. IDs do
// not need randomness, and must work on self-hosted HTTP without secure-context APIs.
export function nextSelectID() {
  globalThis[sequence] = (globalThis[sequence] || 0)+1;
  return `search-select-${globalThis[sequence]}`;
}

export function localOptions(options, query, offset, limit = 24) {
  const needle = query.trim().toLocaleLowerCase();
  const matches = options.filter(item=>item.label.toLocaleLowerCase().includes(needle));
  return {items:matches.slice(offset,offset+limit),total:matches.length,offset,limit};
}

export function featureOptions(api, snapshot) {
  return async(query, offset, signal)=>{
    const page = await api.features(snapshot,{q:query,offset,limit:24},signal);
    return {...page,items:page.items.map(feature=>({value:feature.id,label:feature.title}))};
  };
}

export function optionStep(index, direction, length) {
  if (!length) return -1;
  if (index < 0) return direction > 0 ? 0 : length-1;
  return (index+direction+length)%length;
}

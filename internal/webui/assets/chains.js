export function chainLevels(chain) {
  const nodes = new Map(chain.nodes.map(item=>[item.symbol.id,item]));
  const seen = new Set([chain.root_id]);
  const levels = [[nodes.get(chain.root_id)].filter(Boolean)];
  let frontier = [chain.root_id];
  for (let depth = 0; depth < chain.depth; depth++) {
    frontier = nextLevel(chain,frontier,seen);
    if (!frontier.length) break;
    levels.push(frontier.map(id=>nodes.get(id)).filter(Boolean));
  }
  return levels;
}

function nextLevel(chain, frontier, seen) {
  const next = [];
  const incoming = chain.direction === 'in';
  for (const edge of chain.edges) {
    const from = incoming ? edge.target_id : edge.caller_id;
    const to = incoming ? edge.caller_id : edge.target_id;
    if (!frontier.includes(from) || seen.has(to)) continue;
    seen.add(to);
    next.push(to);
  }
  return next;
}

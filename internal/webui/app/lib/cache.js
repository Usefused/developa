// Tab-local, bounded and credential-scoped. Never persist source or auth tokens
// to browser storage. Explicit refresh bypasses the two-minute read cache.
export class ReadCache {
  constructor(now = Date.now, capacity = 64) { this.now = now; this.capacity = capacity; this.items = new Map(); }
  get(key) {
    const item = this.items.get(key);
    if (!item || this.now()-item.saved >= 120000) return undefined;
    return item.data;
  }
  set(key, data) {
    this.items.delete(key);
    this.items.set(key,{data,saved:this.now()});
    if (this.items.size > this.capacity) this.items.delete(this.items.keys().next().value);
  }
  clear() { this.items.clear(); }
}

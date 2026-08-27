// Reads remain snapshot-specific. A navigation hint requires a second GET so
// old citations are never passed off as analysis of newly indexed source.
export async function loadFeaturePage(api, snapshot, offset, request, preferSaved = false, query = '') {
  let page = await api.features(snapshot.id,{q:query,limit:24,offset},request.signal);
  if (!request.current()) return null;
  if (preferSaved && !page.run && page.saved_snapshot) {
    snapshot = page.saved_snapshot;
    page = await api.features(snapshot.id,{q:query,limit:24,offset:0},request.signal);
  }
  return request.current() ? {snapshot,page} : null;
}

export function featurePageChanged(page, job) {
  if (!job?.base_run_id) return false;
  return job.base_run_id !== page.run?.id;
}

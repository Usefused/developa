export function jobActive(job) { return ['queued','running'].includes(job?.status); }

export function generationLabel(run, job) {
  if (jobActive(job)) return job.status === 'running' ? 'Indexing in background' : 'Analysis queued';
  if (job?.status === 'failed') return 'Retry analysis';
  if (!run) return 'Queue analysis';
  return run.analyzed_symbols < run.total_symbols ? 'Resume analysis' : 'Rebuild analysis';
}

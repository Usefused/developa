export function modelDisclosure(capabilities) {
  if (!capabilities.ollama_configured) return 'Set OLLAMA_ANALYSIS_MODEL or OLLAMA_MODEL to enable indexing. Local is the default; cloud requires OLLAMA_CLOUD=true and a server-side API key. Models are never downloaded automatically.';
  if (capabilities.ollama_cloud) return 'Ollama Cloud is enabled. Generating features sends selected source excerpts to ollama.com. Model output and citations are validated by this server.';
  return 'Inference uses the configured local Ollama server. Cloud fallback and automatic model downloads are disabled.';
}

export function jobActive(job) { return ['queued','running'].includes(job?.status); }

export function generationLabel(run, job) {
  if (jobActive(job)) return job.status === 'running' ? 'Indexing in background' : 'Analysis queued';
  if (job?.status === 'failed') return 'Retry analysis';
  if (!run) return 'Queue analysis';
  return run.analyzed_symbols < run.total_symbols ? 'Resume analysis' : 'Rebuild analysis';
}

export function analysisPolicy(capabilities) {
  if (capabilities.automatic_features) return 'Automatic discovery is enabled once per clean commit. Unchanged pages reuse cached results. Manual queueing can analyze this snapshot without committing.';
  return 'Feature discovery runs when you request it. Normal indexing and page visits do not invoke AI. Unchanged pages reuse cached results; automatic commit analysis is an optional server setting.';
}

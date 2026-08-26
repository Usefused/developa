// Only this read-only subscription reconnects. A dropped POST explanation is
// never replayed automatically because it could spend another inference call.
export async function watchAnalysis(api, snapshot, receive, onError, signal, wait = retryDelay) {
  let backoff = 1000;
  while (!signal.aborted) {
    const result = await readAnalysis(api,snapshot,receive,signal);
    if (signal.aborted) return;
    if (result.error) onError(connectionError(result.error));
    if (!result.retry) return;
    const delay = result.received ? 1000 : backoff;
    await wait(delay,signal);
    backoff = result.received ? 1000 : Math.min(backoff*2,30000);
  }
}

async function readAnalysis(api, snapshot, receive, signal) {
  let received = false;
  try {
    await api.analysisStream(snapshot,job=>{ received = true; receive(job); },signal);
    return {received,retry:true};
  } catch (error) { return {received,error,retry:retryable(error)}; }
}

function retryable(error) {
  if (error.name === 'StreamError' || error.name === 'AbortError') return false;
  if (error.status === 408 || error.status === 429) return true;
  return !error.status || error.status >= 500;
}

function connectionError(error) {
  if (error.status || error.name === 'StreamError') return error;
  return new Error('Live updates were interrupted. Reconnecting…');
}

function retryDelay(delay, signal) {
  return new Promise(resolve=>{
    if (signal.aborted) return resolve();
    const finish = ()=>{ clearTimeout(timer); signal.removeEventListener('abort',finish); resolve(); };
    const timer = setTimeout(finish,delay);
    signal.addEventListener('abort',finish,{once:true});
  });
}

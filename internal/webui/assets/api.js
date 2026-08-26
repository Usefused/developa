import {query} from './model.js';
import {consumeEvents,parseEventData,StreamError} from './sse.js';

export class APIError extends Error {
  constructor(status, traceID, code) {
    const messages = {400:'Check your request or filter and try again.',401:'Enter a valid server access token to open this workspace.',403:'This request was blocked by the server’s origin policy.',404:'This record is not available in the selected snapshot.',409:'An execution is already running. Try again after it finishes.',502:'The model response failed evidence validation. No new results were published.',503:'The server, repository, or configured Ollama model is unavailable. Check the server configuration.',504:'The execution reached its time limit. Previously published results remain available.'};
    super(messages[status] || 'The server could not complete this request.');
    this.status = status;
    this.traceID = traceID;
    this.code = code;
    const workspaceMessages = {not_git_repository:'Git is not enabled in this folder, or this is not the repository root. Select the folder containing .git, or initialize Git locally and try again.',folder_forbidden:'This folder is unavailable or outside the server’s allowed workspace folders.',workspace_limit:'This server already manages the maximum of 32 workspaces.'};
    if (Object.hasOwn(workspaceMessages,code)) this.message = workspaceMessages[code];
  }
}

export class API {
  #credentials;
  #ownsCredentials;

  constructor(fetcher = (path, options) => fetch(path, options), repositoryID = '', credentials = null) {
    this.fetcher = fetcher;
    this.#credentials = credentials || {token:''};
    this.#ownsCredentials = credentials === null;
    Object.defineProperty(this,'repositoryID',{value:repositoryID,enumerable:true});
  }

  get token() { return this.#credentials.token; }
  set token(value) {
    if (!this.#ownsCredentials) throw new TypeError('Set credentials on the root API client.');
    this.#credentials.token = value;
  }

  forRepository(repositoryID = '') {
    if (typeof repositoryID !== 'string') throw new TypeError('Repository ID must be a string.');
    // A scope fixes routing, not a copy of credentials that could survive logout.
    return new API(this.fetcher,repositoryID,this.#credentials);
  }

  async get(path, signal) {
    return this.request(path,{signal});
  }

  async request(path, options = {}) {
    const headers = this.headers('application/json',options.method);
    const response = await this.fetcher(path,{...options,headers,cache:'no-store',credentials:'omit'});
    if (!response.ok) throw new APIError(response.status,response.headers.get('X-Trace-ID'),await errorCode(response));
    return response.json();
  }

  headers(accept, method) {
    const headers = {Accept:accept};
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    if (method === 'POST') headers['Content-Type'] = 'application/json';
    return headers;
  }

  async stream(path, options, receive) {
    const headers = this.headers('text/event-stream',options.method);
    const response = await this.fetcher(path,{...options,headers,cache:'no-store',credentials:'omit'});
    if (!response.ok) {
      await discardStream(response);
      throw new APIError(response.status,response.headers.get('X-Trace-ID'));
    }
    if (response.headers.get('Content-Type')?.split(';')[0].trim().toLowerCase() !== 'text/event-stream') {
      await discardStream(response);
      throw new StreamError();
    }
    await consumeEvents(response,receive,options.signal);
  }

  analysisStream(snapshot, receive, signal) {
    return this.stream(this.path(snapshot,'events'),{signal},event=>{
      if (event.event === 'error') throw streamAPIError(event);
      if (event.event !== 'analysis') return;
      const job = parseEventData(event);
      if (!job || job.snapshot_id !== snapshot || typeof job.status !== 'string') throw new StreamError();
      receive(job);
    });
  }

  async answerStream(snapshot, request, signal, started = ()=>{}) {
    return this.resultStream(snapshot,'answers/stream',request,signal,{eventName:'answer',validate:validAnswerEvent,started});
  }

  savedAnswer(snapshot, request, signal) {
    return this.request(this.path(snapshot,'answers/lookup'),{method:'POST',body:JSON.stringify(request),signal});
  }

  async resultStream(snapshot, suffix, request, signal, {eventName,validate,started = ()=>{}}) {
    let result;
    await this.stream(this.path(snapshot,suffix),{method:'POST',body:JSON.stringify(request),signal},event=>{
      if (event.event === 'error') throw streamAPIError(event);
      if (event.event === 'started') { started(); return; }
      if (event.event !== eventName) return;
      result = parseEventData(event);
      if (!result || result.snapshot_id !== snapshot || !validate(result)) throw new StreamError();
      return false;
    });
    if (!result) throw new StreamError();
    return result;
  }

  async reviewStream(snapshot, request, signal) {
    return this.resultStream(snapshot,'function-reviews/stream',request,signal,{eventName:'reviews',validate:validReviewEvent});
  }

  reviews(snapshot, options, signal) { return this.get(this.path(snapshot,`function-reviews?${query(options)}`),signal); }

  repositories(filters = {}, signal) {
    const search = query(filters);
    return this.get(`/api/repositories${search ? `?${search}` : ''}`,signal);
  }
  project(signal) { return this.get(this.repositoryPath('project'),signal); }
  workspaceRoots(signal) { return this.get('/api/workspace-roots',signal); }
  workspaceFolders(filters,signal) { return this.get(`/api/workspace-folders?${query(filters)}`,signal); }
  addWorkspace(request,signal) { return this.request('/api/repositories',{method:'POST',body:JSON.stringify(request),signal}); }
  scan(signal) { return this.request(this.repositoryPath('scan'),{method:'POST',signal}); }
  files(snapshot, filters, signal) { return this.get(this.path(snapshot,`files?${query(filters)}`),signal); }
  file(snapshot, path, signal) { return this.get(this.path(snapshot,`file?${query({path})}`),signal); }
  symbols(snapshot, filters, signal) { return this.get(this.path(snapshot,`symbols?${query(filters)}`),signal); }
  symbol(snapshot, id, signal) { return this.get(this.path(snapshot,`symbols/${encodeURIComponent(id)}`),signal); }
  details(snapshot, signal) { return this.get(this.path(snapshot,'details'),signal); }
  capabilities(signal) { return this.get(this.repositoryPath('capabilities'),signal); }
  calls(snapshot, filters, signal) { return this.get(this.path(snapshot,`calls?${query(filters)}`),signal); }
  flow(snapshot, filters, signal) { return this.get(this.path(snapshot,`flow?${query(filters)}`),signal); }
  chain(snapshot, id, filters, signal) { return this.get(this.path(snapshot,`symbols/${encodeURIComponent(id)}/chain?${query(filters)}`),signal); }
  features(snapshot, filters, signal) { return this.get(this.path(snapshot,`features?${query(filters)}`),signal); }
  feature(snapshot, id, signal) { return this.get(this.path(snapshot,`features/${encodeURIComponent(id)}`),signal); }
  analysisJob(snapshot, signal) { return this.get(this.path(snapshot,'analysis-job'),signal); }
  discover(snapshot, signal) { return this.request(this.path(snapshot,'features/generate'),{method:'POST',signal}); }
  answer(snapshot, request, signal) { return this.request(this.path(snapshot,'answers'),{method:'POST',body:JSON.stringify(request),signal}); }
  repositoryPath(suffix) { return this.repositoryID ? `/api/repositories/${encodeURIComponent(this.repositoryID)}/${suffix}` : `/api/${suffix}`; }
  path(snapshot, suffix) { return this.repositoryPath(`snapshots/${encodeURIComponent(snapshot)}/${suffix}`); }
}

async function errorCode(response) {
  try { return (await response.json()).status; } catch { return undefined; }
}

function validAnswerEvent(answer) { return typeof answer.text === 'string' && Array.isArray(answer.evidence); }

function validReviewEvent(page) {
  return Array.isArray(page.items) && page.items.length <= 8 && page.options && Number.isInteger(page.total) &&
    (page.next_offset === null || Number.isInteger(page.next_offset));
}

function streamAPIError(event) {
  const payload = parseEventData(event);
  if (!payload || !Number.isInteger(payload.status) || payload.status < 400 || payload.status > 599) throw new StreamError();
  return new APIError(payload.status,typeof payload.trace_id === 'string' ? payload.trace_id : '');
}

async function discardStream(response) {
  if (response.body?.cancel) await response.body.cancel().catch(()=>{});
}

// Each navigation cancels earlier reads. The generation check also protects
// against a response already decoded when its request was canceled.
export class RequestGate {
  constructor() { this.generation = 0; this.controller = null; }
  begin() {
    this.controller?.abort();
    this.controller = new AbortController();
    const generation = ++this.generation;
    return {signal:this.controller.signal,current:()=>generation === this.generation};
  }
  cancel() { this.generation++; this.controller?.abort(); }
}

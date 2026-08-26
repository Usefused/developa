import test from 'node:test';
import assert from 'node:assert/strict';
import {API,APIError} from './assets/api.js';
import {EventParser,StreamError} from './assets/sse.js';
import {watchAnalysis} from './assets/job-stream.js';

import {featurePageChanged} from './assets/feature-state.js';

const encoder = new globalThis.TextEncoder();
const savedAnswer = {id:'answer-A',snapshot_id:'snapshot-A',text:'Saved explanation',evidence:[]};
const job = {id:'job-A',snapshot_id:'snapshot-A',status:'running',updated_at:'2026-08-26T10:00:00Z'};

test('only a new persisted checkpoint marks saved feature cards as outdated',()=>{
  const page = {run:{id:'saved-run'}};
  assert.equal(featurePageChanged(page,{...job,base_run_id:'saved-run'}),false);
  assert.equal(featurePageChanged(page,{...job,base_run_id:'next-run'}),true);
  assert.equal(featurePageChanged({run:null},job),false);
  assert.equal(featurePageChanged({run:null},{...job,base_run_id:'first-run'}),true);
});

test('review SSE returns a saved batch and rejects wrong snapshots or incomplete streams',async()=>{
  const page = {snapshot_id:'snapshot-A',items:[],options:{limit:4,offset:0},total:0,next_offset:null};
  let sent;
  const api = new API(async(path,options)=>{
    sent = {path,options};
    return streamResponse([event('started',{}),event('reviews',page)]);
  });
  assert.deepEqual(await api.reviewStream('snapshot-A',{callee_of:'root',limit:4,offset:8}),page);
  assert.equal(sent.path,'/api/snapshots/snapshot-A/function-reviews/stream');
  assert.deepEqual(JSON.parse(sent.options.body),{callee_of:'root',limit:4,offset:8});
  const wrong = new API(async()=>streamResponse([event('reviews',{...page,snapshot_id:'other'})]));
  await assert.rejects(()=>wrong.reviewStream('snapshot-A',{}),StreamError);
  const partial = new API(async()=>streamResponse([event('started',{})]));
  await assert.rejects(()=>partial.reviewStream('snapshot-A',{}),StreamError);
});

function event(name, payload) { return `event: ${name}\ndata: ${JSON.stringify(payload)}\n\n`; }

function streamResponse(chunks, options = {}) {
  const body = new globalThis.ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(typeof chunk === 'string' ? encoder.encode(chunk) : chunk);
      if (!options.open) controller.close();
    },
    cancel() { options.cancel?.(); },
  });
  return {ok:true,body,headers:{get:name=>name === 'Content-Type' ? 'text/event-stream; charset=utf-8' : 'trace-123'}};
}

test('SSE answer requests use bearer headers and render only the saved terminal event',async()=>{
  let request;
  let canceled = false;
  let started = 0;
  const api = new API(async(path,options)=>{
    request = {path,options};
    return streamResponse([event('started',{status:'started',trace_id:'trace'}),event('answer',savedAnswer)],{open:true,cancel:()=>{ canceled = true; }});
  });
  api.token = 'private-token';
  const controller = new AbortController();
  const answer = await api.answerStream('snapshot-A',{question:'private prompt',feature_id:'feature-A'},controller.signal,()=>started++);
  assert.deepEqual(answer,savedAnswer);
  assert.equal(started,1);
  assert.equal(canceled,true);
  assert.equal(request.path,'/api/snapshots/snapshot-A/answers/stream');
  assert.equal(request.options.headers.Authorization,'Bearer private-token');
  assert.equal(request.options.headers.Accept,'text/event-stream');
  assert.equal(request.options.headers['Content-Type'],'application/json');
  assert.equal(request.options.credentials,'omit');
  assert.equal(request.options.signal,controller.signal);
  assert.deepEqual(JSON.parse(request.options.body),{question:'private prompt',feature_id:'feature-A'});
});

test('SSE decoding handles split UTF-8, CRLF, comments, and multiline JSON fields',async()=>{
  const source = '\ufeff: keepalive\r\nid: ignored\r\nretry: 1\r\nevent: answer\r\ndata: {"snapshot_id":"snapshot-A",\r\ndata: "text":"Résumé 🏠",\r\ndata: "evidence":[]}\r\n\r\n';
  const chunks = [...encoder.encode(source)].map(byte=>new Uint8Array([byte]));
  const api = new API(async()=>streamResponse(chunks));
  const answer = await api.answerStream('snapshot-A',{question:'explain'});
  assert.equal(answer.text,'Résumé 🏠');
});

test('SSE analysis frames stay snapshot scoped and ignore heartbeat comments',async()=>{
  const requests = [];
  const api = new API(async(path,options)=>{
    requests.push({path,options});
    return streamResponse([': keepalive\n\n',event('analysis',job),event('analysis',{...job,status:'completed'})]);
  });
  api.token = 'header-only-token';
  const jobs = [];
  await api.analysisStream('snapshot-A',value=>jobs.push(value));
  assert.deepEqual(jobs.map(value=>value.status),['running','completed']);
  assert.equal(requests[0].path,'/api/snapshots/snapshot-A/events');
  assert.equal(requests[0].options.method,undefined);
  assert.equal(requests[0].options.headers.Authorization,'Bearer header-only-token');
  const wrong = new API(async()=>streamResponse([event('analysis',{...job,snapshot_id:'snapshot-B'})]));
  await assert.rejects(()=>wrong.analysisStream('snapshot-A',()=>assert.fail('cross-snapshot event delivered')),StreamError);
});

test('SSE parser bounds both an unfinished line and accumulated multiline event data',()=>{
  const line = new EventParser(()=>assert.fail('oversized line delivered'),32);
  assert.throws(()=>line.feed(`data: ${'x'.repeat(33)}`),StreamError);
  const event = new EventParser(()=>assert.fail('oversized event delivered'),32);
  assert.throws(()=>event.feed('data: 123456789\ndata: 123456789\ndata: 123456789\n\n'),StreamError);
});

test('SSE parser supports lone CR delimiters and discards unfinished events',()=>{
  const events = [];
  const parser = new EventParser(value=>events.push(value));
  parser.feed(': comment\revent: analysis\rdata: first\rdata: second\r\r');
  parser.feed('event: analysis\ndata: unfinished');
  assert.deepEqual(events,[{event:'analysis',data:'first\nsecond'}]);
});

test('malformed SSE JSON and UTF-8 fail without exposing response contents',async()=>{
  for (const payload of ['event: answer\ndata: private-secret-invalid-json\n\n',new Uint8Array([0xff])]) {
    const api = new API(async()=>streamResponse([payload]));
    await assert.rejects(()=>api.answerStream('snapshot-A',{}),error=>{
      assert.ok(error instanceof StreamError);
      assert.ok(!error.message.includes('private-secret'));
      return true;
    });
  }
});

test('POST streams never replay on disconnect and reject partial terminal events',async()=>{
  let calls = 0;
  const api = new API(async()=>{
    calls++;
    return streamResponse([event('started',{status:'started'}),'event: answer\ndata: {"text":"partial"}']);
  });
  await assert.rejects(()=>api.answerStream('snapshot-A',{}),StreamError);
  assert.equal(calls,1);
});

test('stream error events preserve safe HTTP status and trace correlation only',async()=>{
  const api = new API(async()=>streamResponse([event('started',{status:'started'}),event('error',{status:502,trace_id:'safe-trace',message:'private-upstream-secret'})]));
  await assert.rejects(()=>api.answerStream('snapshot-A',{}),error=>{
    assert.ok(error instanceof APIError);
    assert.equal(error.status,502);
    assert.equal(error.traceID,'safe-trace');
    assert.ok(!error.message.includes('private-upstream-secret'));
    return true;
  });
});

test('stream preflight HTTP errors reject without consuming arbitrary bodies',async()=>{
  const api = new API(async()=>({ok:false,status:401,headers:{get:()=> 'auth-trace'},json:()=>assert.fail('untrusted body parsed')}));
  await assert.rejects(()=>api.answerStream('snapshot-A',{}),error=>error.status === 401 && error.traceID === 'auth-trace');
});

test('unexpected stream content types close the body without displaying its contents',async()=>{
  let canceled = false;
  const response = streamResponse(['private unexpected HTML'],{open:true,cancel:()=>{ canceled = true; }});
  response.headers.get = ()=> 'text/html';
  const api = new API(async()=>response);
  await assert.rejects(()=>api.answerStream('snapshot-A',{}),StreamError);
  assert.equal(canceled,true);
});

test('canceling a POST stream closes its reader and ignores later answer data',async()=>{
  const controller = new AbortController();
  let canceled = false;
  const api = new API(async()=>streamResponse([event('started',{status:'started'}),event('answer',savedAnswer)],{open:true,cancel:()=>{ canceled = true; }}));
  await assert.rejects(()=>api.answerStream('snapshot-A',{},controller.signal,()=>controller.abort()),error=>error.name === 'AbortError');
  assert.equal(canceled,true);
});

test('analysis GET reconnect uses bounded backoff and cancellation ends retrying',async()=>{
  const controller = new AbortController();
  const delays = [];
  let calls = 0;
  const api = {analysisStream:async()=>{ calls++; throw new TypeError('private network diagnostic'); }};
  const errors = [];
  await watchAnalysis(api,'snapshot-A',()=>{},error=>errors.push(error),controller.signal,async delay=>{
    delays.push(delay);
    if (delays.length === 8) controller.abort();
  });
  assert.deepEqual(delays,[1000,2000,4000,8000,16000,30000,30000,30000]);
  assert.equal(calls,8);
  assert.ok(errors.every(error=>!error.message.includes('private network diagnostic')));
});

test('analysis reconnect resets delay after valid events and stops on permanent errors',async()=>{
  const delays = [];
  let calls = 0;
  const api = {analysisStream:async(_snapshot,receive)=>{
    calls++;
    if (calls === 3) receive(job);
    if (calls === 5) throw new APIError(401,'trace');
    throw new TypeError('disconnected');
  }};
  const received = [];
  await watchAnalysis(api,'snapshot-A',value=>received.push(value),()=>{},new AbortController().signal,async delay=>delays.push(delay));
  assert.deepEqual(delays,[1000,2000,1000,1000]);
  assert.deepEqual(received,[job]);
  assert.equal(calls,5);
});

test('repository analysis streams pin their route and abort without retaining copied credentials',async()=>{
  let request,canceled = false;
  const root = new API(async(path,options)=>{
    request = {path,options};
    return streamResponse([event('analysis',job)],{open:true,cancel:()=>{canceled = true;}});
  });
  const api = root.forRepository('repo-A');
  root.token = 'session-secret';
  const controller = new AbortController();
  await assert.rejects(()=>api.analysisStream('snapshot-A',()=>controller.abort(),controller.signal),error=>error.name === 'AbortError');
  assert.equal(request.path,'/api/repositories/repo-A/snapshots/snapshot-A/events');
  assert.equal(request.options.signal,controller.signal);
  assert.equal(request.options.headers.Authorization,'Bearer session-secret');
  assert.equal(canceled,true);
  root.token = '';
  assert.equal(api.headers('text/event-stream').Authorization,undefined);
});

test('repository answer and review streams retain scoped paths and independent cancellation',async()=>{
  const calls = [];
  const page = {snapshot_id:'snapshot-A',items:[],options:{limit:4,offset:0},total:0,next_offset:null};
  const root = new API(async(path,options)=>{
    calls.push({path,options});
    return streamResponse([path.includes('function-reviews') ? event('reviews',page) : event('answer',savedAnswer)]);
  });
  const first = new AbortController(),second = new AbortController();
  await root.forRepository('repo-A').answerStream('snapshot-A',{question:'Explain'},first.signal);
  await root.forRepository('repo-B').reviewStream('snapshot-A',{symbol_id:'symbol'},second.signal);
  assert.deepEqual(calls.map(call=>call.path),['/api/repositories/repo-A/snapshots/snapshot-A/answers/stream','/api/repositories/repo-B/snapshots/snapshot-A/function-reviews/stream']);
  assert.equal(calls[0].options.signal,first.signal);
  assert.equal(calls[1].options.signal,second.signal);
  first.abort();
  assert.equal(second.signal.aborted,false);
});

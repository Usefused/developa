import test from 'node:test';
import assert from 'node:assert/strict';
import {API,APIError,RequestGate} from './assets/api.js';
import {loadFeaturePage} from './assets/feature-state.js';

test('workspace management uses authenticated root API routes even from a repository scope',async()=>{
  const calls = [];
  const root = new API(async(path,options)=>{calls.push({path,options});return {ok:true,json:async()=>({id:'added'})};});
  root.token = 'secret';
  const api = root.forRepository('current');
  await api.workspaceRoots();
  await api.workspaceFolders({root_id:'allowed',path:'repo folder',offset:0});
  await api.addWorkspace({root_id:'allowed',path:'repo folder',name:'New repo'});
  assert.equal(calls[0].path,'/api/workspace-roots');
  assert.equal(calls[1].path,'/api/workspace-folders?root_id=allowed&path=repo+folder&offset=0');
  assert.equal(calls[2].path,'/api/repositories');
  assert.equal(calls[2].options.method,'POST');
  assert.equal(calls[2].options.headers.Authorization,'Bearer secret');
  assert.deepEqual(JSON.parse(calls[2].options.body),{root_id:'allowed',path:'repo folder',name:'New repo'});
});

test('workspace validation displays known safe messages without exposing server error text',async()=>{
  const api = new API(async()=>({ok:false,status:422,headers:{get:()=>null},json:async()=>({status:'not_git_repository',message:'private filesystem details'})}));
  await assert.rejects(()=>api.addWorkspace({}),error=>error.code === 'not_git_repository' && /Git is not enabled/.test(error.message) && !error.message.includes('private'));
});

test('opening Features finds saved analysis with separate snapshot-pinned GETs only',async()=>{
  const calls = [];
  const saved = {id:'saved-source'};
  const page = {run:{id:'run'},items:[{id:'feature'}],offset:0};
  const api = new API(async(path,options)=>{
    calls.push({path,options});
    return {ok:true,json:async()=>calls.length === 1 ? {run:null,items:[],saved_snapshot:saved} : page};
  });
  const result = await loadFeaturePage(api,{id:'latest-source'},24,new RequestGate().begin(),true);
  assert.deepEqual(result,{snapshot:saved,page});
  assert.deepEqual(calls.map(call=>call.path),['/api/snapshots/latest-source/features?limit=24&offset=24','/api/snapshots/saved-source/features?limit=24&offset=0']);
  assert.ok(calls.every(call=>call.options.method === undefined));
});

test('explicit snapshot reads and existing generations never fall back to another source',async()=>{
  for (const [preferSaved,run] of [[false,null],[true,{id:'existing-run'}]]) {
    let calls = 0;
    const api = {features:async()=>{ calls++; return {run,items:[],saved_snapshot:{id:'other'}}; }};
    const result = await loadFeaturePage(api,{id:'selected'},0,new RequestGate().begin(),preferSaved);
    assert.equal(result.snapshot.id,'selected');
    assert.equal(calls,1);
  }
});

test('feature navigation cancellation rejects late saved-snapshot reads',async()=>{
  const gate = new RequestGate();
  const request = gate.begin();
  let finish;
  const api = {features:async(_id)=>{
    if (_id === 'latest') return {run:null,items:[],saved_snapshot:{id:'saved'}};
    return new Promise(resolve=>{ finish = resolve; });
  }};
  const pending = loadFeaturePage(api,{id:'latest'},0,request,true);
  await new Promise(resolve=>setTimeout(resolve,0));
  gate.cancel();
  finish({run:{id:'run'},items:[]});
  assert.equal(await pending,null);
  assert.equal(request.signal.aborted,true);
});

test('reading saved callee reviews is a scoped GET without model execution',async()=>{
  let call;
  const api = new API(async(path,options)=>{
    call = {path,options};
    return {ok:true,json:async()=>({items:[]})};
  });
  await api.reviews('snapshot-A',{callee_of:'function-A',limit:4,offset:8});
  assert.equal(call.path,'/api/snapshots/snapshot-A/function-reviews?callee_of=function-A&limit=4&offset=8');
  assert.equal(call.options.method,undefined);
  assert.equal(call.options.body,undefined);
});

test('saved explanation lookup keeps its question in the body and uses the selected repository',async()=>{
  let call;
  const api = new API(async(path,options)=>{call={path,options};return {ok:true,json:async()=>({answer:null})};});
  const request = {symbol_id:'function',question:'Explain private code'};
  const controller = new AbortController();
  const result = await api.forRepository('repository').savedAnswer('snapshot',request,controller.signal);
  assert.equal(result.answer,null);
  assert.equal(call.path,'/api/repositories/repository/snapshots/snapshot/answers/lookup');
  assert.equal(call.options.method,'POST');
  assert.deepEqual(JSON.parse(call.options.body),request);
  assert.equal(call.options.signal,controller.signal);
});

test('default fetch adapter does not bind the browser fetch method to the API instance',async()=>{
  const previous = globalThis.fetch;
  globalThis.fetch = function() {
    assert.ok(this === undefined || this === globalThis);
    return Promise.resolve({ok:true,json:async()=>({configured:true})});
  };
  try { assert.deepEqual(await new API().get('/api/info'),{configured:true}); }
  finally { globalThis.fetch = previous; }
});

test('catalog calls pin snapshot and scope filters without placing tokens in URLs',async()=>{
  const calls = [];
  const api = new API(async(path,options)=>{
    calls.push({path,options});
    return {ok:true,json:async()=>({items:[]})};
  });
  api.token = 'private-token';
  await api.symbols('snapshot-A',{file:'a b.go',offset:0,limit:50});
  assert.equal(calls[0].path,'/api/snapshots/snapshot-A/symbols?file=a+b.go&offset=0&limit=50');
  assert.equal(calls[0].options.headers.Authorization,'Bearer private-token');
  assert.equal(calls[0].options.credentials,'omit');
});

test('API errors retain trace correlation but never render arbitrary server bodies',async()=>{
  const api = new API(async()=>({ok:false,status:401,headers:{get:()=> 'trace-123'},json:async()=>({message:'secret source payload'})}));
  await assert.rejects(()=>api.project(),error=>{
    assert.ok(error instanceof APIError);
    assert.equal(error.traceID,'trace-123');
    assert.equal(error.status,401);
    assert.ok(!error.message.includes('secret source payload'));
    return true;
  });
});

test('new navigation invalidates and aborts stale reads, including after logout',()=>{
  const gate = new RequestGate();
  const first = gate.begin();
  const second = gate.begin();
  assert.equal(first.signal.aborted,true);
  assert.equal(first.current(),false);
  assert.equal(second.current(),true);
  gate.cancel();
  assert.equal(second.current(),false);
  assert.equal(second.signal.aborted,true);
});

test('scan requests do not transmit a repository path or caller-selected actor',async()=>{
  let request;
  const api = new API(async(path,options)=>{
    request = {path,options};
    return {ok:true,json:async()=>({id:'execution'})};
  });
  await api.scan();
  assert.equal(request.path,'/api/scan');
  assert.equal(request.options.method,'POST');
  assert.equal(request.options.body,undefined);
});

test('intelligence requests pin snapshots and send questions only in the request body',async()=>{
  const calls = [];
  const api = new API(async(path,options)=>{
    calls.push({path,options});
    return {ok:true,json:async()=>({})};
  });
  const controller = new AbortController();
  await api.chain('source-A','symbol-A',{direction:'in',depth:2},controller.signal);
  await api.features('source-A',{limit:24,offset:0},controller.signal);
  await api.discover('source-A',controller.signal);
  await api.answer('source-A',{question:'private code question',symbol_id:'symbol-A'},controller.signal);
  await api.analysisJob('source-A',controller.signal);
  await api.answer('source-A',{question:'Explain supported behavior',feature_id:'feature-A'},controller.signal);
  await api.flow('source-A',{feature_id:'feature-A',depth:6,limit:20},controller.signal);
  assert.equal(calls[0].path,'/api/snapshots/source-A/symbols/symbol-A/chain?direction=in&depth=2');
  assert.equal(calls[1].path,'/api/snapshots/source-A/features?limit=24&offset=0');
  assert.equal(calls[2].options.method,'POST');
  assert.equal(calls[2].options.body,undefined);
  assert.equal(calls[3].path,'/api/snapshots/source-A/answers');
  assert.deepEqual(JSON.parse(calls[3].options.body),{question:'private code question',symbol_id:'symbol-A'});
  assert.equal(calls[4].path,'/api/snapshots/source-A/analysis-job');
  assert.equal(calls[4].options.method,undefined);
  assert.deepEqual(JSON.parse(calls[5].options.body),{question:'Explain supported behavior',feature_id:'feature-A'});
  assert.equal(calls[6].path,'/api/snapshots/source-A/flow?feature_id=feature-A&depth=6&limit=20');
  assert.equal(calls[6].options.method,undefined);
  assert.ok(calls.every(call=>call.options.signal === controller.signal));
});

test('repository scopes are immutable and share only live root credentials',async()=>{
  const calls = [];
  const root = new API(async(path,options)=>{calls.push({path,options});return {ok:true,json:async()=>({})};});
  root.token = 'first-secret';
  const first = root.forRepository('repo/A');
  const second = first.forRepository('repo-B');
  await Promise.all([first.project(),second.project(),root.project()]);
  assert.deepEqual(calls.map(call=>call.path),['/api/repositories/repo%2FA/project','/api/repositories/repo-B/project','/api/project']);
  assert.equal(first.repositoryID,'repo/A');
  assert.equal(second.repositoryID,'repo-B');
  assert.equal(root.repositoryID,'');
  assert.throws(()=>{first.repositoryID = 'changed';},TypeError);
  assert.throws(()=>{first.token = 'stale-secret';},TypeError);
  root.token = 'replacement-secret';
  await first.project();
  assert.equal(calls.at(-1).options.headers.Authorization,'Bearer replacement-secret');
  root.token = '';
  await second.project();
  assert.equal(calls.at(-1).options.headers.Authorization,undefined);
  assert.ok(!JSON.stringify(first).includes('secret'));
});

test('repository clients scope every catalog and mutation while directory reads stay global',async()=>{
  const calls = [];
  const api = new API(async(path,options)=>{calls.push({path,options});return {ok:true,json:async()=>({})};}).forRepository('repo-A');
  const controller = new AbortController();
  const snapshot = 'same-snapshot';
  const prefix = '/api/repositories/repo-A';
  const options = {limit:1};
  const cases = [
    [()=>api.project(controller.signal),`${prefix}/project`],
    [()=>api.capabilities(controller.signal),`${prefix}/capabilities`],
    [()=>api.scan(controller.signal),`${prefix}/scan`],
    [()=>api.files(snapshot,options,controller.signal),`${prefix}/snapshots/${snapshot}/files?limit=1`],
    [()=>api.file(snapshot,'a b.go',controller.signal),`${prefix}/snapshots/${snapshot}/file?path=a+b.go`],
    [()=>api.symbols(snapshot,options,controller.signal),`${prefix}/snapshots/${snapshot}/symbols?limit=1`],
    [()=>api.symbol(snapshot,'symbol',controller.signal),`${prefix}/snapshots/${snapshot}/symbols/symbol`],
    [()=>api.details(snapshot,controller.signal),`${prefix}/snapshots/${snapshot}/details`],
    [()=>api.calls(snapshot,options,controller.signal),`${prefix}/snapshots/${snapshot}/calls?limit=1`],
    [()=>api.flow(snapshot,options,controller.signal),`${prefix}/snapshots/${snapshot}/flow?limit=1`],
    [()=>api.chain(snapshot,'symbol',options,controller.signal),`${prefix}/snapshots/${snapshot}/symbols/symbol/chain?limit=1`],
    [()=>api.features(snapshot,options,controller.signal),`${prefix}/snapshots/${snapshot}/features?limit=1`],
    [()=>api.feature(snapshot,'feature',controller.signal),`${prefix}/snapshots/${snapshot}/features/feature`],
    [()=>api.analysisJob(snapshot,controller.signal),`${prefix}/snapshots/${snapshot}/analysis-job`],
    [()=>api.discover(snapshot,controller.signal),`${prefix}/snapshots/${snapshot}/features/generate`],
    [()=>api.answer(snapshot,{question:'Explain'},controller.signal),`${prefix}/snapshots/${snapshot}/answers`],
    [()=>api.reviews(snapshot,options,controller.signal),`${prefix}/snapshots/${snapshot}/function-reviews?limit=1`],
    [()=>api.repositories({q:'team A',limit:24,offset:0},controller.signal),'/api/repositories?q=team+A&limit=24&offset=0'],
  ];
  for (const [run,path] of cases) {
    await run();
    assert.equal(calls.at(-1).path,path);
    assert.equal(calls.at(-1).options.signal,controller.signal);
  }
  assert.equal(calls[2].options.method,'POST');
  assert.equal(calls[2].options.body,undefined);
  await api.repositories();
  assert.equal(calls.at(-1).path,'/api/repositories');
});

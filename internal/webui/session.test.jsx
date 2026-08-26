import test from 'node:test';
import assert from 'node:assert/strict';
import {JSDOM} from 'jsdom';
import {act} from 'react';
import {createRoot} from 'react-dom/client';
import {API} from './assets/api.js';
import {ReadCache} from './app/lib/cache.js';
import {preferencesKey} from './app/lib/preferences.js';
import {apiTokenKey} from './app/lib/auth-storage.js';
import {SessionContext,SessionProvider,ScopedSessionProvider,useSession} from './app/hooks/use-session.jsx';
import {useResource} from './app/hooks/use-resource.js';
import {usePreferences} from './app/hooks/use-preferences.js';

function sessionEnvironment(t) {
  const dom = new JSDOM('<!doctype html><div id="root"></div>',{url:'http://localhost/'});
  const names = {window:dom.window,document:dom.window.document,localStorage:dom.window.localStorage,fetch:globalThis.fetch,IS_REACT_ACT_ENVIRONMENT:true};
  const previous = Object.fromEntries(Object.keys(names).map(key=>[key,globalThis[key]]));
  Object.assign(globalThis,names);
  const root = createRoot(document.getElementById('root'));
  t.after(async()=>{await act(async()=>root.unmount());dom.window.close();Object.assign(globalThis,previous);});
  return {root,dom};
}

function jsonResponse(value) { return {ok:true,json:async()=>value}; }

function ScopedProbe({receive}) {
  const session = useSession();
  receive(session);
  const resource = useResource('same-snapshot:same-symbol',signal=>session.api.symbol('same-snapshot','same-symbol',signal));
  return <p>{resource.data?.name || 'loading'}</p>;
}

test('repository provider replaces equal-snapshot hook state and aborts previous reads',async t=>{
  const {root} = sessionEnvironment(t);
  const pending = [],scopes = new Map();
  const api = new API((path,options)=>new Promise(resolve=>pending.push({path,options,resolve})));
  api.token = 'root-token';
  const session = {api,cache:new ReadCache(),epoch:1,status:'ready',defaultRepositoryID:'repo-A'};
  const view = id=><SessionContext.Provider value={session}><ScopedSessionProvider repositoryID={id}><ScopedProbe receive={value=>scopes.set(id,value)}/></ScopedSessionProvider></SessionContext.Provider>;
  await act(async()=>root.render(view('repo-A')));
  await act(async()=>root.render(view('repo-B')));
  assert.equal(pending[0].options.signal.aborted,true);
  assert.equal(pending[1].path,'/api/repositories/repo-B/snapshots/same-snapshot/symbols/same-symbol');
  await act(async()=>pending[1].resolve(jsonResponse({name:'Repository B'})));
  await act(async()=>pending[0].resolve(jsonResponse({name:'Late repository A'})));
  assert.equal(document.body.textContent,'Repository B');
  assert.notEqual(scopes.get('repo-A').cache,scopes.get('repo-B').cache);
  assert.equal(scopes.get('repo-A').cache.get('same-snapshot:same-symbol'),undefined);
  assert.equal(scopes.get('repo-B').cache.get('same-snapshot:same-symbol').name,'Repository B');
  assert.equal(scopes.get('repo-B').rootApi,api);
  assert.equal(api.repositoryID,'');
});

function SessionProbe({receive}) {
  const session = useSession();
  receive(session);
  return <p>{session.status}:{session.defaultRepositoryID}</p>;
}

async function connectionFixture(t) {
  const {root} = sessionEnvironment(t);
  const pending = [];
  let current;
  globalThis.fetch = (path,options)=>{
    if (path === '/api/info') return Promise.resolve(jsonResponse({configured:true,authentication_required:true}));
    assert.equal(path,'/api/project');
    return new Promise(resolve=>pending.push({options,resolve}));
  };
  await act(async()=>root.render(<SessionProvider><SessionProbe receive={value=>{current=value;}}/></SessionProvider>));
  return {pending,current:()=>current};
}

test('authentication captures only the current default repository and replaces credential-scoped caches',async t=>{
  const fixture = await connectionFixture(t);
  assert.equal(fixture.current().status,'locked');
  await act(async()=>fixture.current().connect('first-token'));
  await act(async()=>fixture.current().connect('second-token'));
  assert.equal(localStorage.getItem(apiTokenKey),null);
  assert.equal(fixture.pending[0].options.signal.aborted,true);
  await act(async()=>fixture.pending[0].resolve(jsonResponse({repository:{id:'stale-default'}})));
  assert.equal(fixture.current().status,'connecting');
  assert.equal(fixture.current().defaultRepositoryID,'');
  await act(async()=>fixture.pending[1].resolve(jsonResponse({repository:{id:'repo-B'}})));
  assert.equal(fixture.current().status,'ready');
  assert.equal(fixture.current().defaultRepositoryID,'repo-B');
  assert.equal(localStorage.getItem(apiTokenKey),'second-token');
  const previousCache = fixture.current().cache;
  previousCache.set('private','old credential source');
  await act(async()=>fixture.current().lock());
  previousCache.set('private','late response');
  assert.equal(fixture.current().cache.get('private'),undefined);
  assert.equal(fixture.current().api.token,'');
  assert.equal(localStorage.getItem(apiTokenKey),null);
  assert.equal(fixture.current().defaultRepositoryID,'');
});

test('page refresh restores a saved token and authenticates before reopening the workspace',async t=>{
  const {root} = sessionEnvironment(t);
  localStorage.setItem(apiTokenKey,'persisted-token');
  const credentials = [];
  globalThis.fetch = async(path,options)=>{
    if (path === '/api/info') return jsonResponse({configured:true,authentication_required:true});
    credentials.push(options.headers.Authorization);
    return jsonResponse({repository:{id:'saved-repo'}});
  };
  let current;
  const view = key=><SessionProvider key={key}><SessionProbe receive={value=>{current=value;}}/></SessionProvider>;
  await act(async()=>root.render(view('before-refresh')));
  await act(async()=>root.render(view('after-refresh')));
  assert.deepEqual(credentials,['Bearer persisted-token','Bearer persisted-token']);
  assert.equal(current.status,'ready');
  assert.equal(current.defaultRepositoryID,'saved-repo');
});

test('rejected saved credentials are forgotten and an empty managed engine can be unlocked',async t=>{
  const {root} = sessionEnvironment(t);
  localStorage.setItem(apiTokenKey,'expired-token');
  globalThis.fetch = async(path)=>path === '/api/info'
    ? jsonResponse({configured:false,workspace_management:true,authentication_required:true})
    : {ok:false,status:401,headers:{get:()=>null},json:async()=>({status:'unauthorized'})};
  let current;
  await act(async()=>root.render(<SessionProvider><SessionProbe receive={value=>{current=value;}}/></SessionProvider>));
  assert.equal(current.status,'locked');
  assert.equal(current.api.token,'');
  assert.equal(localStorage.getItem(apiTokenKey),null);
  globalThis.fetch = async path=>jsonResponse(path === '/api/info'
    ? {configured:false,workspace_management:true,authentication_required:true} : {configured:false,repository:{id:''}});
  await act(async()=>current.connect('valid-token'));
  assert.equal(current.status,'ready');
  assert.equal(localStorage.getItem(apiTokenKey),'valid-token');
});

test('logout aborts connection preflight and a late success cannot reopen the session',async t=>{
  const fixture = await connectionFixture(t);
  await act(async()=>fixture.current().connect('temporary-token'));
  const scoped = fixture.current().api.forRepository('repo-A');
  await act(async()=>fixture.current().lock());
  assert.equal(fixture.pending[0].options.signal.aborted,true);
  await act(async()=>fixture.pending[0].resolve(jsonResponse({repository:{id:'late-default'}})));
  assert.equal(fixture.current().status,'locked');
  assert.equal(fixture.current().defaultRepositoryID,'');
  assert.equal(scoped.headers('application/json').Authorization,undefined);
});

function PreferencesProbe({id,defaultID,receive}) {
  const [value,update] = usePreferences(id,defaultID);
  receive({value,update});
  return <p>{id}:{value.root}:{value.theme}</p>;
}

test('mounted preference scopes share theme while merging separate editor paths',async t=>{
  const {root,dom} = sessionEnvironment(t);
  dom.window.localStorage.setItem(preferencesKey,JSON.stringify({root:'/legacy/default',editor:'cursor'}));
  const scopes = new Map();
  await act(async()=>root.render(<>
    <PreferencesProbe id="default" defaultID="default" receive={value=>scopes.set('default',value)}/>
    <PreferencesProbe id="other" defaultID="default" receive={value=>scopes.set('other',value)}/>
  </>));
  assert.equal(scopes.get('default').value.root,'/legacy/default');
  assert.equal(scopes.get('other').value.root,'');
  await act(async()=>scopes.get('other').update({root:'/projects/other',editor:'vscode'}));
  await act(async()=>scopes.get('default').update(current=>({...current,theme:'dark'})));
  assert.equal(scopes.get('default').value.root,'/legacy/default');
  assert.equal(scopes.get('other').value.root,'/projects/other');
  assert.equal(scopes.get('other').value.theme,'dark');
  assert.equal(document.documentElement.dataset.theme,'dark');
  const stored = JSON.parse(dom.window.localStorage.getItem(preferencesKey));
  assert.equal(stored.legacyRepositoryID,'default');
  assert.deepEqual(stored.repositories,{other:{root:'/projects/other',editor:'vscode'}});
});

import test from 'node:test';
import assert from 'node:assert/strict';
import {JSDOM} from 'jsdom';
import {act} from 'react';
import {createRoot} from 'react-dom/client';
import {createMemoryRouter,RouterProvider,Outlet} from 'react-router';
import {SessionContext} from './app/hooks/use-session.jsx';
import {WorkspaceContext} from './app/hooks/use-workspace.jsx';
import {ReadCache} from './app/lib/cache.js';
import {useResource} from './app/hooks/use-resource.js';
import {useAction} from './app/hooks/use-action.js';
import {SearchSelect} from './app/components/search-select.jsx';
import FeaturesRoute from './app/routes/features.jsx';
import ChangesRoute from './app/routes/changes.jsx';
import BlocksRoute from './app/routes/blocks.jsx';
import FlowRoute from './app/routes/flow.jsx';
import HomeRoute from './app/routes/home.jsx';
import {SourceViewer} from './app/components/details/source-viewer.jsx';
import {Workspace} from './app/routes/workspace.jsx';
import './session.test.jsx';
import {AskAIButton} from './app/components/intelligence/ask-ai-button.jsx';
import {AskAIProvider,conversationQuestion} from './app/components/intelligence/ask-ai.jsx';
import {FunctionReviews} from './app/components/intelligence/function-reviews.jsx';
import {EditorLink} from './app/components/editor-link.jsx';
import {EditorActions} from './app/components/details/editor-actions.jsx';
import {AddWorkspaceDialog} from './app/components/workspaces/add-workspace-dialog.jsx';

const snapshot = {id:'a'.repeat(64),file_count:1,changes_known:true};
const saved = {run:{id:'run-one',status:'partial',analyzed_symbols:1,total_symbols:8,model:'fixture'},
  total:1,limit:24,offset:0,items:[{id:'feature',title:'Saved feature',summary:'Already persisted',evidence:[]}]};

function environment(t) {
  const dom = new JSDOM('<!doctype html><div id="root"></div>',{url:'http://localhost/'});
  const names = {window:dom.window,document:dom.window.document,IS_REACT_ACT_ENVIRONMENT:true};
  const previous = Object.fromEntries(Object.keys(names).map(key=>[key,globalThis[key]]));
  Object.assign(globalThis,names);
  const root = createRoot(document.getElementById('root'));
  t.after(async()=>{await act(async()=>root.unmount());dom.window.close();Object.assign(globalThis,previous);});
  return {root,dom};
}

function fixtureAPI() {
  const state = {reads:0,contextReads:0,flowReads:0,subscriptions:0,aborts:0,posts:0};
  const api = {
    features:async()=>{state.reads++;return saved;},
    savedAnswer:async()=>({answer:null}),
    capabilities:async()=>({analysis_jobs:true}),
    details:async()=>({snapshot,changes:[]}),
    files:async()=>({items:[],offset:0,limit:24,total:0}),
    flow:async(_id,options)=>{state.flowReads++;return {snapshot_id:snapshot.id,options,mode:'feature',nodes:[],edges:[],limitations:[]};},
    feature:async(_id,id)=>({id,title:`Feature ${id}`,summary:'Saved evidence',evidence:[]}),
    featureContext:async(_id,id)=>{state.contextReads++;const symbol={id:'symbol-one',name:'LoadWorkspace',kind:'function',signature:'func LoadWorkspace() error',documentation:{summary:'Loads a saved workspace and validates its repository.'},span:{start:{line:14,column:1}}};return {repository_id:'repo',snapshot_id:snapshot.id,options:{source_limit:20,depth:6,flow_limit:80},feature:{id,title:'Saved feature',summary:'Already persisted',evidence:[]},source:{total:1,truncated:false,items:[{path:'workspace.go',symbol}]},flow:{snapshot_id:snapshot.id,nodes:[{path:'workspace.go',symbol,seed:true,incoming_count:0,outgoing_count:1,unresolved_count:0,description:'Loads a saved workspace and validates its repository.'}],edges:[],limitations:[],truncated:false},limitations:[]};},
    discover:async()=>{state.posts++;throw new Error('fixture unavailable');},
    analysisStream:(_id,receive,signal)=>{
      state.subscriptions++;state.receive = receive;
      return new Promise(resolve=>signal.addEventListener('abort',()=>{state.aborts++;resolve();},{once:true}));
    },
  };
  return {api,state};
}

function testRouter(api, initial='/features') {
  const cache = new ReadCache();
  function Layout() {
    return <SessionContext.Provider value={{api,cache,status:'ready'}}><WorkspaceContext.Provider value={{snapshot,preferences:{root:'',editor:'vscode'}}}><Outlet/></WorkspaceContext.Provider></SessionContext.Provider>;
  }
  const children = [{path:'/features',element:<FeaturesRoute/>},{path:'/changes',element:<ChangesRoute/>},{path:'/blocks',element:<BlocksRoute/>},{path:'/flow',element:<FlowRoute/>}];
  return createMemoryRouter([{path:'/',element:<HomeRoute/>},{element:<Layout/>,children}],{initialEntries:[`${initial}?snapshot=${snapshot.id}`]});
}

function button(label) { return [...document.querySelectorAll('button')].find(item=>item.textContent === label); }
async function click(element, dom) { await act(async()=>element.dispatchEvent(new dom.window.MouseEvent('click',{bubbles:true}))); }

test('add workspace browses folders without mutation and keeps Git failures in the dialog',async t=>{
  const {root,dom} = environment(t);
  dom.window.HTMLDialogElement.prototype.showModal = function(){this.open=true;};
  const requests = [],added = [];
  const api = {
    workspaceRoots:async()=>[{id:'allowed',path:'/repos'}],
    workspaceFolders:async({path})=>({items:path === '.' ? [{name:'Backend',path:'backend'}] : [],next_offset:null}),
    addWorkspace:async request=>{requests.push(request);if (requests.length === 1) throw new Error('Git is not enabled in this folder.');return {id:'new-workspace'};},
  };
  await act(async()=>root.render(<SessionContext.Provider value={{api,cache:new ReadCache()}}><AddWorkspaceDialog close={()=>{}} added={(id,path)=>added.push([id,path])}/></SessionContext.Provider>));
  assert.equal(requests.length,0);
  await click([...document.querySelectorAll('.folder-option')][0],dom);
  assert.match(document.querySelector('.selected-folder').textContent,/backend/);
  await click(button('Add selected folder'),dom);
  assert.match(document.querySelector('[role=alert]').textContent,/Git is not enabled/);
  assert.equal(document.querySelector('dialog').open,true);
  assert.deepEqual(requests[0],{root_id:'allowed',path:'backend',name:''});
  await click(button('Add selected folder'),dom);
  assert.deepEqual(added,[['new-workspace','/repos/backend']]);
});

function editorView(root='/projects/My Repo', path='main.go', settings=()=>{}) {
  return <WorkspaceContext.Provider value={{preferences:{root,editor:'vscode'},settings}}>
    <EditorLink path={path} position={{line:17,column:4}} className="primary-button"/>
  </WorkspaceContext.Provider>;
}

test('editor launch preserves native navigation and offers honest local fallback feedback',async t=>{
  const {root,dom} = environment(t);
  let settings = 0;
  await act(async()=>root.render(editorView(undefined,undefined,()=>settings++)));
  const link = document.querySelector('a');
  assert.equal(link.getAttribute('href'),'vscode://file/projects/My%20Repo/main.go:17:4');
  assert.equal(document.querySelector('details').open,false);
  // Cancel navigation in jsdom only; the component must not launch processes or
  // replace the browser's user-initiated external protocol handling.
  const event = new dom.window.MouseEvent('click',{bubbles:true,cancelable:true});
  event.preventDefault();
  await act(async()=>link.dispatchEvent(event));
  assert.equal(document.querySelector('details').open,true);
  assert.match(document.body.textContent,/If VS Code did not open/);
  assert.equal(document.querySelector('input').value,'/projects/My Repo/main.go:17:4');
  await click(button('Editor settings'),dom);
  assert.equal(settings,1);
});

function clipboardFixture(t, writeText) {
  const previous = Object.getOwnPropertyDescriptor(globalThis.navigator,'clipboard');
  Object.defineProperty(globalThis.navigator,'clipboard',{configurable:true,value:{writeText}});
  t.after(()=>{
    if (previous) Object.defineProperty(globalThis.navigator,'clipboard',previous);
    else delete globalThis.navigator.clipboard;
  });
}

test('editor fallback copies the absolute location, handles denial, and resets across files',async t=>{
  const {root,dom} = environment(t);
  const copies = [];
  clipboardFixture(t,async value=>copies.push(value));
  await act(async()=>root.render(editorView()));
  await click(button('Copy local location'),dom);
  assert.deepEqual(copies,['/projects/My Repo/main.go:17:4']);
  assert.match(document.querySelector('[role=status]').textContent,/Local editor location copied/);
  await act(async()=>root.render(editorView('/projects/Other Repo','other.go')));
  assert.equal(document.querySelector('[role=status]').textContent,'');
  assert.equal(document.querySelector('input').value,'/projects/Other Repo/other.go:17:4');
  navigator.clipboard.writeText = async()=>{throw new Error('denied');};
  await click(button('Copy local location'),dom);
  assert.match(document.querySelector('[role=status]').textContent,/Select and copy the location above/);
});

test('invalid editor paths require setup and never expose an external launch',async t=>{
  const {root,dom} = environment(t);
  let settings = 0;
  const item = {path:'main.go',symbol:{span:{start:{line:1,column:1}}}};
  await act(async()=>root.render(<WorkspaceContext.Provider value={{preferences:{root:'',editor:'vscode'},settings:()=>settings++}}><EditorActions item={item}/></WorkspaceContext.Provider>));
  assert.equal(document.querySelector('a'),null);
  await click(button('Set up editor ↗'),dom);
  assert.equal(settings,1);
  await act(async()=>root.render(editorView('/repo','../outside.go')));
  assert.equal(document.body.textContent,'');
});

test('React feature SSE preserves cards and only explicit refresh reloads saved results',async t=>{
  const {root,dom} = environment(t);
  const {api,state} = fixtureAPI();
  const router = testRouter(api);
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  const card = document.querySelector('.feature-card');
  assert.ok(card);
  await act(async()=>state.receive({snapshot_id:snapshot.id,status:'running',base_run_id:'new'}));
  assert.equal(document.querySelector('.feature-card'),card);
  assert.equal(state.reads,1);
  assert.equal(state.subscriptions,1);
  await click(button('Refresh saved features'),dom);
  assert.equal(state.reads,2);
  assert.equal(document.querySelector('.feature-card'),card);
});

test('selecting a feature opens its agent-ready implementation context before the graph',async t=>{
  const {root,dom} = environment(t);
  const {api,state} = fixtureAPI();
  const router = testRouter(api);
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  await click(document.querySelector('.feature-card'),dom);
  assert.equal(state.contextReads,1);
  assert.equal(state.flowReads,0);
  assert.match(document.querySelector('.feature-workspace').textContent,/How the code supports this feature/);
  assert.match(document.querySelector('.feature-support-card').textContent,/Loads a saved workspace and validates its repository/);
  assert.match(router.state.location.search,/feature=feature/);
});

test('routed navigation aborts SSE and back navigation uses saved cache without inference',async t=>{
  const {root} = environment(t);
  const {api,state} = fixtureAPI();
  const router = testRouter(api);
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  await act(async()=>router.navigate(`/changes?snapshot=${snapshot.id}`));
  assert.equal(state.aborts,1);
  assert.match(document.body.textContent,/No file-content changes/);
  await act(async()=>router.navigate(-1));
  assert.ok(document.querySelector('.feature-card'));
  assert.equal(state.reads,1);
  assert.equal(state.posts,0);
});

test('explicit feature queue failure retains cards and does not retry the POST',async t=>{
  const {root,dom} = environment(t);
  const {api,state} = fixtureAPI();
  const router = testRouter(api);
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  const card = document.querySelector('.feature-card');
  await click(button('Resume analysis'),dom);
  assert.equal(state.posts,1);
  assert.equal(document.querySelector('.feature-card'),card);
  assert.match(document.body.textContent,/fixture unavailable/);
});

test('resource key changes abort reads and cannot display a late previous snapshot',async t=>{
  const {root} = environment(t);
  const cache = new ReadCache();
  const pending = {};
  const load = id=>signal=>new Promise(resolve=>{pending[id] = {signal,resolve};});
  function Probe({id}) { const result = useResource(id,load(id)); return <p>{result.data || 'loading'}</p>; }
  const view = id=><SessionContext.Provider value={{cache}}><Probe id={id}/></SessionContext.Provider>;
  await act(async()=>root.render(view('old')));
  await act(async()=>root.render(view('new')));
  assert.equal(pending.old.signal.aborted,true);
  await act(async()=>pending.new.resolve('new source'));
  await act(async()=>pending.old.resolve('stale source'));
  assert.equal(document.body.textContent,'new source');
  assert.equal(cache.get('old'),undefined);
});

test('action unmount aborts a stream and does not retry or publish a late answer',async t=>{
  const {root,dom} = environment(t);
  let signal,finish,calls = 0;
  function Probe() {
    const action = useAction();
    return <button onClick={()=>action.run(value=>{calls++;signal=value;return new Promise(resolve=>{finish=resolve;});})}>{action.data || 'Explain'}</button>;
  }
  await act(async()=>root.render(<Probe/>));
  await click(button('Explain'),dom);
  await act(async()=>root.render(<p>Other page</p>));
  assert.equal(signal.aborted,true);
  await act(async()=>finish('Late answer'));
  assert.equal(document.body.textContent,'Other page');
  assert.equal(calls,1);
});

test('shared React selector keeps selection separate from its full-width search query',async t=>{
  const {root,dom} = environment(t);
  const options = [{value:'method',label:'Methods'},{value:'function',label:'Functions'}];
  const changes = [];
  await act(async()=>root.render(<SearchSelect label="Kind" options={options} selected={options[0]} onChange={value=>changes.push(value)}/>));
  const chevron = document.querySelector('svg.search-select-chevron');
  assert.equal(chevron.namespaceURI,'http://www.w3.org/2000/svg');
  assert.equal(chevron.getAttribute('aria-hidden'),'true');
  assert.equal(chevron.getAttribute('focusable'),'false');
  await click(document.querySelector('[role=combobox]'),dom);
  const input = document.querySelector('[role=searchbox]');
  const searchIcon = input.parentElement.querySelector('svg.search-select-icon');
  assert.equal(searchIcon.getAttribute('width'),'18');
  assert.equal(searchIcon.getAttribute('height'),'18');
  assert.equal(searchIcon.getAttribute('aria-hidden'),'true');
  assert.equal(searchIcon.getAttribute('focusable'),'false');
  assert.equal(input.value,'');
  input.value = 'function';
  await act(async()=>input.dispatchEvent(new dom.window.Event('input',{bubbles:true})));
  assert.equal(document.querySelectorAll('[role=option]').length,1);
  await click(document.querySelector('[role=option]'),dom);
  assert.deepEqual(changes,['function']);
  assert.equal(document.querySelector('[role=combobox]').textContent,'Functions');
});

test('failed feature refresh keeps the existing card and enables manual retry',async t=>{
  const {root,dom} = environment(t);
  const {api} = fixtureAPI();
  const router = testRouter(api);
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  const card = document.querySelector('.feature-card');
  api.features = async()=>{throw new Error('refresh unavailable');};
  await click(button('Refresh saved features'),dom);
  assert.equal(document.querySelector('.feature-card'),card);
  assert.equal(button('Refresh saved features').disabled,false);
  assert.match(document.body.textContent,/refresh unavailable/);
});

test('feature switching preserves native depth and rejects a stale evidence response',async t=>{
  const {root,dom} = environment(t);
  const {api} = fixtureAPI();
  const pending = {};
  api.feature = (_snapshot,id,signal)=>new Promise(resolve=>{pending[id]={signal,resolve};});
  const router = testRouter(api,'/flow');
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  await act(async()=>router.navigate(`/flow?snapshot=${snapshot.id}&feature=old&depth=8`));
  await act(async()=>router.navigate(`/flow?snapshot=${snapshot.id}&feature=new&depth=8`));
  assert.equal(pending.old.signal.aborted,true);
  await act(async()=>pending.new.resolve({id:'new',title:'New feature',summary:'Current evidence',evidence:[]}));
  await act(async()=>pending.old.resolve({id:'old',title:'Stale feature',summary:'Old evidence',evidence:[]}));
  assert.match(document.body.textContent,/New feature/);
  assert.doesNotMatch(document.body.textContent,/Stale feature/);
  const depth = document.querySelector('select[aria-label="Traversal depth"]');
  assert.equal(depth.value,'8');
  depth.value = '12';
  await act(async()=>depth.dispatchEvent(new dom.window.Event('change',{bubbles:true})));
  assert.match(router.state.location.search,/feature=new.*depth=12/);
});

test('home redirect precedes workspace snapshot selection and retains legacy pins',async t=>{
  const {root} = environment(t);
  const {api} = fixtureAPI();
  const router = testRouter(api,'/');
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  assert.equal(router.state.location.pathname,'/features');
  await act(async()=>router.navigate('/'));
  assert.equal(router.state.location.pathname,'/blocks');
});

test('React source viewer preserves Go text and never interprets source as markup',async t=>{
  const {root} = environment(t);
  const source = 'func demo() {\n  // <img src=x onerror=alert(1)>\n}\n';
  const symbol = {name:'demo',source,source_truncated:true,span:{start:{line:12,column:1}}};
  await act(async()=>root.render(<SourceViewer symbol={symbol} path="demo.go"/>));
  assert.equal(document.querySelector('code').textContent,source);
  assert.equal(document.querySelector('code img'),null);
  assert.equal(document.querySelector('.source-line-number').dataset.line,'12');
  assert.match(document.body.textContent,/Partial excerpt/);
});

function repositoryFixture(id, name) {
  const {api,state} = fixtureAPI();
  const repository = {id,name};
  api.repositoryID = id;
  api.project = async()=>({configured:true,repository,snapshot,status:'ready',watching:true});
  api.features = async()=>{state.reads++;return {...saved,items:[{...saved.items[0],title:`${name} saved feature`}]};};
  api.scan = async()=>{state.scans = (state.scans || 0)+1;return {id:'execution',status:'queued'};};
  return {api,state,repository};
}

function workspaceRouter(initial) {
  const alpha = repositoryFixture('1'.repeat(64),'Alpha');
  const beta = repositoryFixture('2'.repeat(64),'Beta');
  const records = {[alpha.repository.id]:alpha,[beta.repository.id]:beta};
  const queries = [];
  const api = {
    forRepository:id=>records[id].api,
    repositories:async options=>{queries.push(options);return {items:[alpha.repository,beta.repository],offset:0,limit:24,total:2};},
  };
  alpha.api.repositories = api.repositories;
  beta.api.repositories = api.repositories;
  const value = {api,rootApi:api,cache:new ReadCache(),status:'ready',epoch:1,defaultRepositoryID:alpha.repository.id};
  const layout = <SessionContext.Provider value={value}><Workspace/></SessionContext.Provider>;
  const children = [{path:'/features',element:<FeaturesRoute/>},{path:'/blocks',element:<BlocksRoute/>},{path:'/changes',element:<ChangesRoute/>}];
  const router = createMemoryRouter([{element:layout,children}],{initialEntries:[initial || `/features?repo=${alpha.repository.id}&snapshot=${snapshot.id}`]});
  return {router,alpha,beta,queries};
}

test('workspace picker isolates equal snapshot IDs, cancels old SSE and scopes navigation',async t=>{
  const {root,dom} = environment(t);
  const {router,alpha,beta,queries} = workspaceRouter();
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  assert.match(document.querySelector('.feature-card').textContent,/Alpha saved feature/);
  await click(document.querySelector('[aria-label=Workspace][role=combobox]'),dom);
  assert.deepEqual(queries,[{q:'',offset:0,limit:24}]);
  await click([...document.querySelectorAll('[role=option]')].find(item=>item.textContent.startsWith('Beta')),dom);
  assert.match(document.querySelector('.feature-card').textContent,/Beta saved feature/);
  assert.equal(alpha.state.aborts,1);
  assert.equal(beta.state.reads,1);
  const links = [...document.querySelectorAll('.nav-item')];
  assert.ok(links.every(link=>new dom.window.URL(link.href).searchParams.get('repo') === beta.repository.id));
  assert.equal(alpha.state.posts+beta.state.posts,0);
  await act(async()=>router.navigate(-1));
  assert.match(document.querySelector('.feature-card').textContent,/Alpha saved feature/);
  assert.equal(beta.state.aborts,1);
});

test('legacy links pin the default repository and manual scans use the selected workspace',async t=>{
  const {root,dom} = environment(t);
  const {router,alpha,beta} = workspaceRouter('/blocks');
  t.after(()=>router.dispose());
  await act(async()=>root.render(<RouterProvider router={router}/>));
  assert.equal(new URLSearchParams(router.state.location.search).get('repo'),alpha.repository.id);
  await act(async()=>router.navigate(`/blocks?repo=${beta.repository.id}`));
  await click(button('↻ Reindex'),dom);
  assert.equal(beta.state.scans,1);
  assert.equal(alpha.state.scans,undefined);
});

function explanationView(api, cache, id='function-one') {
  return <SessionContext.Provider value={{api,cache}}><WorkspaceContext.Provider value={{snapshot,preferences:{root:'',editor:'vscode'}}}>
    <AskAIProvider><AskAIButton target={{type:'symbol',id,title:id}}/></AskAIProvider>
  </WorkspaceContext.Provider></SessionContext.Provider>;
}

test('Ask AI opens contextual chat and invokes the model only after a question',async t=>{
  const {root,dom} = environment(t);
  let generations = 0, request;
  const api = {
    answerStream:async(_snapshot,value)=>{generations++;request=value;return {id:'answer',snapshot_id:snapshot.id,text:'Explains this function.',model:'fixture',evidence:[],limitations:[]};},
  };
  await act(async()=>root.render(explanationView(api,new ReadCache())));
  assert.equal(generations,0);
  await click(button('Ask AI about this function'),dom);
  assert.ok(document.querySelector('.ask-ai-window'));
  assert.equal(generations,0);
  await click(button('Explain this function'),dom);
  assert.match(document.querySelector('.ask-ai-message.assistant').textContent,/Explains this function/);
  assert.equal(request.symbol_id,'function-one');
  assert.match(request.question,/parameters/);
  assert.equal(generations,1);
});

test('Ask AI follow-up context stays within the server UTF-8 byte bound',()=>{
  const messages = [{role:'assistant',text:'😀'.repeat(1000)},{role:'user',text:'latest question'}];
  const question = conversationQuestion(messages);
  assert.ok(new globalThis.TextEncoder().encode(question).length <= 2000);
  assert.match(question,/latest question$/);
});

test('Ask AI restores a matching saved contextual answer without new inference',async t=>{
  const {root,dom} = environment(t);
  let generations = 0;
  const answer = {snapshot_id:snapshot.id,text:'Previously saved explanation.',evidence:[],limitations:[],cached:true};
  const api = {savedAnswer:async()=>({answer}),answerStream:async()=>{generations++;return answer;}};
  await act(async()=>root.render(explanationView(api,new ReadCache())));
  await click(button('Ask AI about this function'),dom);
  assert.match(document.querySelector('.ask-ai-message.assistant').textContent,/Previously saved explanation/);
  assert.equal(generations,0);
});

test('Ask AI keeps several contextual popup chats open at once',async t=>{
  const {root,dom} = environment(t);
  const api = {answerStream:async()=>({snapshot_id:snapshot.id,text:'answer',evidence:[],limitations:[]})};
  const view = <SessionContext.Provider value={{api,cache:new ReadCache()}}><WorkspaceContext.Provider value={{snapshot,preferences:{root:'',editor:'vscode'}}}><AskAIProvider>
    <AskAIButton target={{type:'symbol',id:'function-one',title:'LoadWorkspace'}}/><AskAIButton target={{type:'feature',id:'feature-one',title:'Workspace indexing'}}/>
  </AskAIProvider></WorkspaceContext.Provider></SessionContext.Provider>;
  await act(async()=>root.render(view));
  await click(button('Ask AI about this function'),dom);
  await click(button('Ask AI about this feature'),dom);
  assert.equal(document.querySelectorAll('.ask-ai-window').length,2);
  assert.match(document.body.textContent,/LoadWorkspace/);
  assert.match(document.body.textContent,/Workspace indexing/);
});

test('closing an Ask AI popup cancels its in-flight contextual request',async t=>{
  const {root,dom} = environment(t);
  let signal;
  const api = {
    answerStream:(_snapshot,_request,value)=>new Promise(resolve=>{signal=value;value.addEventListener('abort',resolve,{once:true});}),
  };
  await act(async()=>root.render(explanationView(api,new ReadCache())));
  await click(button('Ask AI about this function'),dom);
  await click(button('Explain this function'),dom);
  await click(document.querySelector('[aria-label="Close chat"]'),dom);
  assert.equal(signal.aborted,true);
  assert.equal(document.querySelector('.ask-ai-window'),null);
});

test('function review offers one clearly scoped action without preflight policy copy or empty callee details',async t=>{
  const {root} = environment(t);
  const api = {
    capabilities:async()=>({function_review_generation:true}),
    reviews:async()=>({snapshot_id:snapshot.id,total:0,items:[],unresolved_count:1,model_calls:0,options:{offset:0,limit:4},next_offset:null}),
  };
  const item = {symbol:{id:'function-one',kind:'function'},review:null};
  await act(async()=>root.render(<SessionContext.Provider value={{api,cache:new ReadCache()}}><WorkspaceContext.Provider value={{snapshot}}><FunctionReviews item={item} updated={()=>{}}/></WorkspaceContext.Provider></SessionContext.Provider>));
  assert.equal(document.querySelector('.ai-review-section h3').textContent,'AI card metadata');
  assert.ok(button('Generate card description + parameter notes'));
  assert.equal(document.querySelector('.callee-reviews'),null);
  assert.doesNotMatch(document.body.textContent,/AI runs only|Each batch sends|model calls this batch|Calls 0 local functions/);
});

test('function review hides background review lookup failures before an AI action',async t=>{
  const {root} = environment(t);
  const api = {
    capabilities:async()=>({function_review_generation:true}),
    reviews:async()=>{ throw new Error('review lookup failed'); },
  };
  const item = {symbol:{id:'function-one',kind:'function'},review:null};
  await act(async()=>root.render(<SessionContext.Provider value={{api,cache:new ReadCache()}}><WorkspaceContext.Provider value={{snapshot}}><FunctionReviews item={item} updated={()=>{}}/></WorkspaceContext.Provider></SessionContext.Provider>));
  assert.ok(button('Generate card description + parameter notes'));
  assert.equal(document.querySelector('.error-banner'),null);
  assert.doesNotMatch(document.body.textContent,/review lookup failed/);
});

import test from 'node:test';
import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
import {flowDiagram,flowRelations,flowTitle,rootLabel,initialFocus,connectionFocus} from './flow-source/model.js';
import {ConnectionProof,FunctionDescription} from './flow-source/cards.js';
import {callPath} from './flow-source/edges.js';

function declaration(id, incoming = [], outgoing = [], extra = {}) {
  return {path:`${id}.go`,seed:false,root_kind:'',incoming_ids:incoming,outgoing_ids:outgoing,
    incoming_count:incoming.length,outgoing_count:outgoing.length,unresolved_count:0,
    symbol:{id,name:id,kind:'function',signature:`func ${id}()`,span:{start:{line:1,column:1}}},...extra};
}

function call(id, caller, target, extra = {}) {
  return {id,caller_id:caller,target_id:target,caller_name:caller,target_name:target,
    resolution:'resolved',path:`${caller}.go`,span:{start:{line:3,column:2}},...extra};
}

function flow(nodes, edges = [], extra = {}) {
  return {nodes,edges,mode:'application',options:{depth:6,limit:80},seed_ids:[],cycle_groups:[],...extra};
}

function diamond() {
  return flow([
    declaration('main',[],['left','right'],{root_kind:'main',seed:true}),
    declaration('left',['main'],['shared']),declaration('right',['main'],['shared']),
    declaration('shared',['left','right']),
  ],[call('a','main','left'),call('b','main','right'),call('c','left','shared'),call('d','right','shared')]);
}

test('diamond shared dependencies remain one canonical declaration with both callers',()=>{
  const input = diamond();
  const diagram = flowDiagram(input);
  assert.deepEqual(diagram.nodes.map(item=>item.id),['main','left','right','shared']);
  assert.equal(diagram.edges.length,4);
  assert.deepEqual([...diagram.relations.incoming.get('shared')],['left','right']);
  assert.equal(diagram.nodes.filter(item=>item.id === 'shared').length,1);
  assert.strictEqual(diagram.relations.records.get('shared'),input.nodes[3]);
});

test('acyclic layout places roots above their callees without mutating API facts',()=>{
  const input = diamond();
  const before = JSON.stringify(input);
  const diagram = flowDiagram(input);
  const y = new Map(diagram.nodes.map(item=>[item.id,item.position.y]));
  for (const edge of diagram.edges) assert.ok(y.get(edge.source) < y.get(edge.target));
  for (const item of diagram.nodes) assert.ok(Number.isFinite(item.position.x) && Number.isFinite(item.position.y));
  assert.equal(JSON.stringify(input),before);
});

test('initial viewport selects the top of the flow instead of shrinking the whole graph',()=>{
  const diagram = flowDiagram(diamond());
  const focus = initialFocus(diagram);
  assert.equal(focus.length,2);
  assert.equal(focus[0].id,'main');
  assert.ok(focus[0].data.outgoing.includes(focus[1].id));
  assert.deepEqual(initialFocus({nodes:[]}),[]);
});

test('connection focus fades unrelated branches without changing layout or source evidence',()=>{
  const {nodes,edges} = flowDiagram(diamond());
  const before = JSON.stringify({nodes,edges});
  const focused = connectionFocus(nodes,edges,'main:left');
  assert.deepEqual(focused.nodes.filter(node=>node.className === 'flow-node-connected').map(node=>node.id),['main','left']);
  assert.deepEqual(focused.nodes.filter(node=>node.className === 'flow-node-muted').map(node=>node.id),['right','shared']);
  assert.deepEqual(focused.edges.filter(edge=>edge.selected).map(edge=>edge.id),['main:left']);
  assert.equal(focused.edges.filter(edge=>edge.className === 'flow-edge-muted').length,3);
  for (const [index,node] of focused.nodes.entries()) assert.strictEqual(node.position,nodes[index].position);
  for (const [index,edge] of focused.edges.entries()) assert.strictEqual(edge.data,edges[index].data);
  assert.equal(JSON.stringify({nodes,edges}),before);
});

test('switching connections replaces focus and clearing restores every node and edge',()=>{
  const {nodes,edges} = flowDiagram({...diamond(),mode:'feature'});
  connectionFocus(nodes,edges,'main:left');
  const next = connectionFocus(nodes,edges,'right:shared');
  assert.deepEqual(next.nodes.filter(node=>node.className === 'flow-node-connected').map(node=>node.id),['right','shared']);
  assert.deepEqual(next.edges.filter(edge=>edge.selected).map(edge=>edge.id),['right:shared']);
  for (const id of [null,undefined,'missing']) {
    const cleared = connectionFocus(nodes,edges,id);
    assert.strictEqual(cleared.nodes,nodes);
    assert.strictEqual(cleared.edges,edges);
  }
});

test('self-loop focus retains recursion styling and highlights only its own declaration',()=>{
  const {nodes,edges} = flowDiagram(flow([declaration('loop'),declaration('other')],
    [call('self','loop','loop')],{cycle_groups:[['loop']]}));
  nodes[0].className = 'existing-node-style';
  const focused = connectionFocus(nodes,edges,'loop:loop');
  assert.equal(focused.nodes[0].className,'existing-node-style flow-node-connected');
  assert.equal(focused.nodes[1].className,'flow-node-muted');
  assert.equal(focused.edges[0].className,'recursive-edge flow-edge-focused');
  assert.equal(focused.edges[0].data.recursive,true);
});

test('routed connections reserve label positions outside the function cards',()=>{
  const diagram = flowDiagram(diamond());
  for (const edge of diagram.edges) {
    assert.ok(Number.isFinite(edge.data.labelPosition.x) && Number.isFinite(edge.data.labelPosition.y));
    assert.ok(edge.data.route.length >= 2);
    for (const item of diagram.nodes) assert.ok(labelOutsideCard(edge.data.labelPosition,item));
  }
});

function labelOutsideCard(label, item) {
  return label.x < item.position.x || label.x > item.position.x+288 || label.y < item.position.y || label.y > item.position.y+266;
}

test('call routes preserve port endpoints without sharing a default center segment',()=>{
  const path = callPath([{x:4,y:10},{x:80,y:90},{x:120,y:150}],{x:10,y:20},{x:130,y:160});
  assert.ok(path.startsWith('M 10 20'));
  assert.ok(path.endsWith('L 130 160'));
  assert.ok(path.includes('Q 80 90'));
  assert.equal(path.includes('NaN'),false);
});

test('card descriptions render the API preview as text with its evidence source',()=>{
  const comment = renderToStaticMarkup(h(FunctionDescription,{description:'Reads <input> & returns bytes.',description_source:'source_comment'}));
  assert.ok(comment.includes('Reads &lt;input&gt; &amp; returns bytes.'));
  assert.ok(comment.includes('FROM SOURCE COMMENT'));
  const summary = renderToStaticMarkup(h(FunctionDescription,{description:'Returns error.',description_source:'signature'}));
  assert.ok(summary.includes('Returns error.'));
  assert.ok(summary.includes('SIGNATURE SUMMARY'));
  const review = renderToStaticMarkup(h(FunctionDescription,{description:'<script>inferred</script>',description_source:'llm_review'}));
  assert.ok(review.includes('SAVED AI REVIEW · INFERRED'));
  assert.ok(review.includes('&lt;script&gt;inferred&lt;/script&gt;'));
});

test('compiled source summary is labeled as requiring no AI',()=>{
  const card = renderToStaticMarkup(h(FunctionDescription,{description:'Header. Body reason.',description_source:'source_comments',review:{summary:'Different AI prose'}}));
  assert.ok(card.includes('SOURCE SUMMARY · NO AI'));
  assert.ok(card.includes('Header. Body reason.'));
  assert.equal(card.includes('Different AI prose'),false);
});

test('API recursive groups mark mutual recursion and self-loops only',()=>{
  const input = flow([
    declaration('main',[],['a']),declaration('a',['main','b'],['b']),
    declaration('b',['a'],['a','leaf']),declaration('leaf',['b','leaf'],['leaf']),
  ],[call('root','main','a'),call('ab','a','b'),call('ba','b','a'),call('end','b','leaf'),call('self','leaf','leaf')],
  {cycle_groups:[['a','b'],['leaf']]});
  const diagram = flowDiagram(input);
  assert.deepEqual(diagram.edges.filter(edge=>edge.data.recursive).map(edge=>edge.id),['a:b','b:a','leaf:leaf']);
  assert.deepEqual(diagram.edges.filter(edge=>!edge.data.recursive).map(edge=>edge.id),['main:a','b:leaf']);
  for (const edge of diagram.edges.filter(edge=>edge.data.recursive)) assert.equal(edge.label,'recursive');
  assert.equal(new Set(diagram.nodes.map(item=>item.id)).size,4);
});

test('call sites aggregate by canonical endpoints without unresolved or invented edges',()=>{
  const input = flow([declaration('a',[],['b']),declaration('b',['a'])],[
    call('first','a','b'),call('second','a','b'),
    call('candidate','a','b',{resolution:'candidate'}),call('dynamic','a','',{resolution:'unresolved'}),
    call('outside','a','absent'),call('missing-caller','absent','b'),
  ]);
  const diagram = flowDiagram(input);
  assert.equal(diagram.edges.length,1);
  assert.equal(diagram.edges[0].label,'2 call sites');
  assert.deepEqual(diagram.edges[0].data.calls.map(item=>item.id),['first','second']);
  assert.deepEqual([...diagram.relations.incoming.get('b')],['a']);
  assert.deepEqual([...diagram.relations.outgoing.get('a')],['b']);
  assert.equal(diagram.relations.records.has('absent'),false);
});

test('disconnected noncallable evidence retains its node without fabricated connections',()=>{
  const record = declaration('Config',[],[],{seed:true});
  record.symbol.kind = 'struct';
  record.symbol.signature = 'type Config struct {}';
  const diagram = flowDiagram(flow([record,declaration('Worker')],[],{mode:'feature',seed_ids:['Config']}));
  assert.deepEqual(diagram.nodes.map(item=>item.id),['Config','Worker']);
  assert.deepEqual(diagram.edges,[]);
  assert.equal(diagram.nodes[0].data.item.seed,true);
  assert.deepEqual([...diagram.relations.outgoing.get('Config')],[]);
});

test('an empty flow yields an empty diagram without invented roots or coordinates',()=>{
  const diagram = flowDiagram(flow([]));
  assert.deepEqual(diagram.nodes,[]);
  assert.deepEqual(diagram.edges,[]);
  assert.equal(diagram.relations.records.size,0);
  assert.equal(flowTitle(flow([])),'Application flow');
});

test('titles distinguish explicit source selection, inferred feature, and candidate roots',()=>{
  const input = diamond();
  assert.equal(flowTitle({...input,mode:'symbol',options:{symbol_id:'shared'}}),'Through shared');
  assert.equal(flowTitle({...input,mode:'feature'},'Inferred capability'),'Inferred capability');
  assert.equal(flowTitle({...input,mode:'feature'}),'Feature flow');
  assert.equal(rootLabel('main'),'APPLICATION ENTRY');
  assert.equal(rootLabel('candidate'),'CANDIDATE ROOT');
  assert.equal(rootLabel('boundary'),'GRAPH BOUNDARY');
  assert.equal(rootLabel('unknown'),'');
});

test('hostile source labels stay text and call proof does not fabricate source links',()=>{
  const hostile = '<img src=x onerror="alert(1)">';
  const source = declaration('source');
  source.symbol.name = hostile;
  source.path = 'javascript:alert(1)';
  const proof = call('evidence','source','target',{caller_name:hostile,path:source.path});
  const diagram = flowDiagram(flow([source,declaration('target')],[proof]));
  assert.ok(diagram.nodes[0].ariaLabel.includes(hostile));
  const markup = renderToStaticMarkup(h(ConnectionProof,{edge:diagram.edges[0],open:()=>{},close:()=>{}}));
  assert.ok(markup.includes('&lt;img src=x onerror=&quot;alert(1)&quot;&gt;'));
  assert.equal(markup.includes('<img'),false);
  assert.equal(markup.includes('href='),false);
  assert.ok(markup.includes('javascript:alert(1):3:2'));
});

test('relationship lookup contains only canonical API records',()=>{
  const input = diamond();
  const relations = flowRelations(input);
  assert.equal(relations.records.size,input.nodes.length);
  for (const item of input.nodes) assert.strictEqual(relations.records.get(item.symbol.id),item);
  assert.equal(relations.groups[0].calls[0].id,'a');
});

test('full-snapshot counts never fabricate callers or dependencies outside the API slice',()=>{
  const item = declaration('boundary',[],[],{root_kind:'boundary',incoming_count:50,outgoing_count:40,unresolved_count:12});
  const diagram = flowDiagram(flow([item]));
  assert.equal(diagram.nodes.length,1);
  assert.deepEqual(diagram.edges,[]);
  assert.deepEqual(diagram.nodes[0].data.incoming,[]);
  assert.deepEqual(diagram.nodes[0].data.outgoing,[]);
  assert.equal(diagram.nodes[0].data.item.incoming_count,50);
});

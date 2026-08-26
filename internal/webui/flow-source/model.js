import dagre from '@dagrejs/dagre';

export const cardSize = {width:288,height:266};

export function rootLabel(kind) {
  return {main:'APPLICATION ENTRY',init:'PACKAGE INITIALIZER',candidate:'CANDIDATE ROOT',boundary:'GRAPH BOUNDARY'}[kind] || '';
}

export function flowRelations(flow) {
  const records = new Map(flow.nodes.map(item=>[item.symbol.id,item]));
  const incoming = new Map(flow.nodes.map(item=>[item.symbol.id,new Set(item.incoming_ids)]));
  const outgoing = new Map(flow.nodes.map(item=>[item.symbol.id,new Set(item.outgoing_ids)]));
  const groups = new Map();
  for (const call of flow.edges) {
    if (call.resolution !== 'resolved' || !records.has(call.caller_id) || !records.has(call.target_id)) continue;
    const id = `${call.caller_id}:${call.target_id}`;
    if (!groups.has(id)) groups.set(id,{id,source:call.caller_id,target:call.target_id,calls:[]});
    groups.get(id).calls.push(call);
  }
  return {records,incoming,outgoing,groups:[...groups.values()]};
}

export function flowDiagram(flow) {
  const relations = flowRelations(flow);
  const graph = new dagre.graphlib.Graph().setGraph({rankdir:'TB',nodesep:56,ranksep:95,marginx:30,marginy:30});
  graph.setDefaultEdgeLabel(()=>({}));
  for (const item of flow.nodes) graph.setNode(item.symbol.id,{...cardSize});
  for (const edge of relations.groups) graph.setEdge(edge.source,edge.target,{width:116,height:26,labelpos:'c'});
  // Recursion is an API fact. Dagre only chooses positions for the renderer.
  const components = cycleComponents(flow.cycle_groups);
  dagre.layout(graph);
  const nodes = flow.nodes.map(item=>diagramNode(item,graph,relations));
  const edges = relations.groups.map(edge=>diagramEdge(edge,components,graph));
  return {nodes,edges,relations};
}

function cycleComponents(groups = []) {
  const components = new Map();
  for (const [index,ids] of groups.entries()) {
    for (const id of ids) components.set(id,index);
  }
  return components;
}

function diagramNode(item, graph, relations) {
  const {x,y} = graph.node(item.symbol.id);
  return {
    id:item.symbol.id,type:'function',position:{x:x-cardSize.width/2,y:y-cardSize.height/2},
    data:{item,incoming:[...relations.incoming.get(item.symbol.id)],outgoing:[...relations.outgoing.get(item.symbol.id)]},
    ariaLabel:`${item.symbol.name}, ${item.path}, line ${item.symbol.span.start.line}`,
  };
}

function diagramEdge(edge, components, graph) {
  const recursive = components.has(edge.source) && components.get(edge.source) === components.get(edge.target);
  const label = recursive ? 'recursive' : `${edge.calls.length} call site${edge.calls.length === 1 ? '' : 's'}`;
  const layout = graph.edge(edge.source,edge.target);
  return {...edge,type:'call',label,data:{calls:edge.calls,recursive,route:layout.points,labelPosition:{x:layout.x,y:layout.y}},className:recursive ? 'recursive-edge' : '',
    markerEnd:{type:'arrowclosed'},ariaLabel:`${label}: ${edge.calls[0].caller_name} calls ${edge.calls[0].target_name}`};
}

export function flowTitle(flow, featureTitle = '') {
  if (flow.mode === 'feature') return featureTitle || 'Feature flow';
  const seed = flow.nodes.find(item=>item.symbol.id === flow.options.symbol_id);
  return seed ? `Through ${seed.symbol.name}` : 'Application flow';
}

export function initialFocus(diagram) {
  const ordered = [...diagram.nodes].sort((a,b)=>a.position.y-b.position.y || a.position.x-b.position.x);
  const first = ordered[0];
  if (!first) return [];
  const child = ordered.find(item=>first.data.outgoing.includes(item.id) && item.position.y > first.position.y);
  return child ? [first,child] : [first];
}

export function connectionFocus(nodes, edges, id) {
  const connection = edges.find(edge=>edge.id === id);
  if (!connection) return {nodes,edges};
  // Keep the whole layout mounted so focusing a call never moves its neighbors.
  const endpoints = new Set([connection.source,connection.target]);
  return {
    nodes:nodes.map(node=>({...node,className:`${node.className || ''} flow-node-${endpoints.has(node.id) ? 'connected' : 'muted'}`.trim()})),
    edges:edges.map(edge=>({...edge,selected:edge.id === id,className:`${edge.className || ''} flow-edge-${edge.id === id ? 'focused' : 'muted'}`.trim()})),
  };
}

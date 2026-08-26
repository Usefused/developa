import {createElement as h,useMemo,useState,useCallback} from 'react';
import {ReactFlow,ReactFlowProvider,Background,Controls,MiniMap,useReactFlow,useNodesState} from '@xyflow/react';
import {flowDiagram,initialFocus,connectionFocus} from './model.js';
import {FunctionCard,DependencyPanel,ConnectionProof} from './cards.js';
import {SourceCallEdge} from './edges.js';
import {FlowSelect} from './search-select.js';

export {flowTitle} from './model.js';

const nodeTypes = {function:FunctionCard};
const edgeTypes = {call:SourceCallEdge};

function FlowCanvas({flow,actions}) {
  const diagram = useMemo(()=>flowDiagram(flow),[flow]);
  const [dependency,setDependency] = useState('');
  const [proof,setProof] = useState(null);
  const {fitView} = useReactFlow();
  const initialNodes = useMemo(()=>diagram.nodes.map(item=>({...item,
    data:{...item.data,onInspect:actions.symbol,onDependencies:setDependency}})),[diagram,actions]);
  const [nodes,setNodes,onNodesChange] = useNodesState(initialNodes);
  const focused = useMemo(()=>connectionFocus(nodes,diagram.edges,proof?.id),[nodes,diagram,proof]);
  const initialFit = useMemo(()=>({nodes:initialFocus(diagram),padding:0.15,maxZoom:0.9}),[diagram]);
  const jump = useCallback(id=>{
    setNodes(items=>items.map(item=>({...item,selected:item.id === id})));
    setDependency('');
    setProof(null);
    fitView({nodes:[{id}],padding:0.7,maxZoom:1,duration:250});
  },[fitView,setNodes]);
  const overview = ()=>{setProof(null);fitView({padding:0.15,duration:250});};
  const onEdgesChange = changes=>{
    const selected = changes.find(change=>change.type === 'select' && change.selected);
    if (selected) setProof(diagram.edges.find(edge=>edge.id === selected.id));
  };
  const onKeyDown = event=>{
    if (event.key !== 'Escape' || !proof) return;
    event.stopPropagation();
    setProof(null);
  };
  return h('div',{className:'flow-diagram',onKeyDown},
    h(FlowNavigator,{diagram,jump,inspect:setProof,overview}),
    h('div',{className:'flow-canvas'},h(ReactFlow,{
      nodes:focused.nodes,edges:focused.edges,nodeTypes,edgeTypes,onNodesChange,onEdgesChange,fitView:true,fitViewOptions:initialFit,
      minZoom:0.08,maxZoom:1.8,nodesDraggable:false,nodesConnectable:false,edgesReconnectable:false,
      deleteKeyCode:null,panOnScroll:true,zoomOnScroll:false,zoomOnDoubleClick:false,
      onEdgeClick:(_,edge)=>setProof(edge),
      onPaneClick:()=>{setDependency('');setProof(null);},'aria-label':'Resolved source call flow',
    },h(Background,{gap:22,size:1}),h(Controls,{showInteractive:false}),h(MiniMap,{pannable:true,zoomable:true,className:'flow-minimap'}),
    dependency && h(DependencyPanel,{id:dependency,relations:diagram.relations,jump,close:()=>setDependency(''),trace:actions.trace}))),
    h(ConnectionProof,{edge:proof,open:actions.symbol,close:()=>setProof(null)}),
    h(FlowOutline,{diagram,jump,open:actions.symbol}));
}

function FlowNavigator({diagram,jump,inspect,overview}) {
  const functions = useMemo(()=>diagram.nodes.map(item=>({value:item.id,label:`${item.data.item.symbol.name} · ${item.data.item.path}`})),[diagram]);
  const connections = useMemo(()=>diagram.edges.map(edge=>({value:edge.id,label:`${edge.calls[0].caller_name} → ${edge.calls[0].target_name}`})),[diagram]);
  return h('div',{className:'flow-navigator'},
    h('div',{className:'flow-picker'},h('span',null,'Jump to function'),h(FlowSelect,{label:'Jump to function',options:functions,onChange:jump,placeholder:'Find a function in this flow…'})),
    h('div',{className:'flow-picker'},h('span',null,'Inspect a connection'),h(FlowSelect,{label:'Inspect a connection',options:connections,onChange:id=>inspect(diagram.edges.find(edge=>edge.id === id)),placeholder:'Choose a call for source proof…'})),
    h('button',{className:'secondary-button',onClick:overview},'Show whole flow'));
}

function FlowOutline({diagram,jump,open}) {
  const ordered = [...diagram.nodes].sort((a,b)=>a.position.y-b.position.y || a.position.x-b.position.x);
  return h('details',{className:'flow-outline'},h('summary',null,`Accessible outline · ${ordered.length} declarations`),
    h('ol',null,ordered.map(item=>h('li',{key:item.id},
      h('button',{className:'text-button',onClick:()=>open(item.id)},item.data.item.symbol.name),
      h('span',{className:'mono'},`${item.data.item.path}:${item.data.item.symbol.span.start.line}`),
      h('button',{className:'quiet-button',onClick:()=>jump(item.id)},'Locate in flow ↗')))));
}

export function FlowDiagram({flow,actions}) {
  return h(ReactFlowProvider,null,h(FlowCanvas,{flow,actions}));
}

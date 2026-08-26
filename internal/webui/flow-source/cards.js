import {createElement as h} from 'react';
import {Handle,Position,Panel} from '@xyflow/react';
import {rootLabel} from './model.js';

export function FunctionCard({data,selected}) {
  const {item,onInspect,onDependencies} = data;
  const symbol = item.symbol;
  const label = rootLabel(item.root_kind) || (item.seed ? 'SELECTED EVIDENCE' : symbol.kind.toUpperCase());
  return h('article',{className:`flow-card ${item.seed ? 'flow-seed' : ''} ${selected ? 'flow-focused' : ''}`},
    h(Handle,{type:'target',position:Position.Top,isConnectable:false}),
    h('div',{className:'flow-card-kicker'},h('span',null,label),item.incoming_count > 1 && h('span',{className:'flow-shared'},'SHARED')),
    h('button',{className:'flow-name nodrag nopan',onClick:()=>onInspect(symbol.id),title:symbol.name},symbol.name),
    h('p',{className:'flow-card-path',title:`${item.path}:${symbol.span.start.line}`},`${item.path}:${symbol.span.start.line}`),
    h('p',{className:'flow-signature',title:symbol.signature},symbol.signature),
    h(FunctionDescription,item),
    h('button',{className:'flow-dependencies nodrag nopan',onClick:()=>onDependencies(symbol.id)},
      `${item.incoming_count} callers · ${item.outgoing_count} calls`,h('span',{'aria-hidden':true},' ↗')),
    item.unresolved_count > 0 && h('span',{className:'flow-unresolved'},`${item.unresolved_count} unresolved / external call sites`),
    h(Handle,{type:'source',position:Position.Bottom,isConnectable:false}));
}

export function FunctionDescription({description,description_source,review}) {
  const labels = {source_comments:'SOURCE SUMMARY · NO AI',source_comment:'FROM SOURCE COMMENT',signature:'SIGNATURE SUMMARY',llm_review:'SAVED AI REVIEW · INFERRED'};
  const extra = description_source?.startsWith('source_comment') && review ? ' · AI REVIEW SAVED' : '';
  return h('div',{className:'flow-description-block'},
    h('p',{className:'flow-card-description',title:description},description),
    h('span',{className:'flow-description-source'},`${labels[description_source] || labels.signature}${extra}`));
}

function jumpList(title, ids, records, jump) {
  return h('section',{className:'flow-link-group'},h('h4',null,title),
    ids.length === 0 ? h('p',null,'None in this view.') : h('ul',null,ids.map(id=>h('li',{key:id},
      h('button',{className:'text-button',onClick:()=>jump(id)},`${records.get(id).symbol.name} ↗`),
      h('span',{className:'flow-link-path'},records.get(id).path)))));
}

export function DependencyPanel({id,relations,jump,close,trace}) {
  const item = relations.records.get(id);
  if (!item) return null;
  const incoming = [...relations.incoming.get(id)];
  const outgoing = [...relations.outgoing.get(id)];
  const omitted = item.incoming_count > incoming.length || item.outgoing_count > outgoing.length;
  return h(Panel,{position:'top-right',className:'flow-links nodrag nopan nowheel'},
    h('div',{className:'flow-links-heading'},h('h3',null,item.symbol.name),h('button',{className:'quiet-button',onClick:close,'aria-label':'Close dependencies'},'×')),
    h('p',null,'Jump to a connected function in this flow.'),
    jumpList('Called by',incoming,relations.records,jump),jumpList('Calls',outgoing,relations.records,jump),
    omitted && h('p',{className:'flow-warning'},'More connections exist outside this bounded view.'),
    h('button',{className:'secondary-button',onClick:()=>trace(id)},'Trace from this function'));
}

export function ConnectionProof({edge,open,close}) {
  if (!edge) return null;
  return h('section',{className:'flow-proof','aria-label':'Call site evidence'},
    h('div',{className:'flow-links-heading'},h('h3',null,'Resolved call sites'),h('button',{className:'quiet-button',onClick:close,'aria-label':'Close call site evidence'},'×')),
    h('p',null,'These are static relationships, not a timeline or a guarantee that a branch executes.'),
    h('ul',null,edge.data.calls.map(call=>h('li',{key:call.id},
      h('button',{className:'text-button',onClick:()=>open(call.caller_id)},`${call.caller_name} → ${call.target_name}`),
      h('span',{className:'mono'},` ${call.path}:${call.span.start.line}:${call.span.start.column}`)))));
}

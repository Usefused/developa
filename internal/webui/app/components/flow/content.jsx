import {lazy,Suspense} from 'react';
import {flowTitle} from '../../../flow-source/model.js';
import {usePageNavigation} from '../../hooks/use-workspace.jsx';
import {EmptyState} from '../common.jsx';
import {AskAIButton} from '../intelligence/ask-ai-button.jsx';

const Diagram = lazy(()=>import('../../../flow-source/entry.js').then(module=>({default:module.FlowDiagram})));

export function FlowContent({flow,feature}) {
  const {openSymbol,go} = usePageNavigation();
  const actions = {symbol:openSymbol,trace:id=>go('flow',{root:id})};
  return <><h2 className="flow-title">{flowTitle(flow,feature?.title)}</h2><p className="flow-description">Resolved callers are traced upward; connections are arranged from roots to callees. Shared functions appear once, with links to their callers.</p>
    <div className="flow-summary"><span className="detail-tag">{flow.mode === 'feature' ? 'INFERRED FEATURE · STATIC CALLS' : 'STATIC CALLS · NO AI'}</span><span>{flow.nodes.length} declarations · {flow.edges.length} resolved call sites · snapshot {flow.snapshot_id.slice(0,8)}</span>
      {flow.truncated && <p className="flow-warning">Partial view: a traversal or size limit was reached. Increase depth or trace from a function.</p>}</div>
    {flow.nodes.length ? <Suspense fallback={<p role="status">Loading flow renderer…</p>}><Diagram key={JSON.stringify(flow.options)} flow={flow} actions={actions}/></Suspense> : <EmptyState title="No flow evidence found">Open a function from Code blocks to trace it.</EmptyState>}
    <FlowLimitations flow={flow}/><AskAIButton target={{type:'flow',options:flow.options,title:feature?.title || flowTitle(flow)}}/>
  </>;
}

function FlowLimitations({flow}) {
  return <details className="flow-limitations"><summary>What this flow can and cannot show</summary><ul><li>Arrows show statically resolved calls, not execution order, branches or a runtime trace. Recursive links are dashed.</li><li>Candidate roots have no resolved callers in the index; they are not proven application entrypoints. Unresolved callbacks and interface dispatch can leave gaps.</li>{(flow.limitations || []).map((text,index)=><li key={index}>{text}</li>)}</ul></details>;
}

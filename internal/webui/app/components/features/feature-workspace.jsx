import {sourceSummary} from '../../../assets/documentation.js';
import {kindLabel} from '../../../assets/model.js';
import {usePageNavigation} from '../../hooks/use-workspace.jsx';
import {Button} from '../common.jsx';
import {EditorLink} from '../editor-link.jsx';
import {AskAIButton} from '../intelligence/ask-ai-button.jsx';

export function FeatureWorkspace({bundle,onBack,onFlow}) {
  const {openSymbol} = usePageNavigation();
  const {feature,source,flow} = bundle;
  const nodes = new Map(flow.nodes.map(node=>[node.symbol.id,node]));
  return <article className="feature-workspace">
    <div className="feature-workspace-nav"><Button className="text-button" onClick={onBack}>← All features</Button><div><AskAIButton target={{type:'feature',id:feature.id,title:feature.title}}/><Button onClick={()=>onFlow(feature.id)}>Open technical flow →</Button></div></div>
    <header className="feature-hero"><span className="detail-tag">INFERRED CAPABILITY · SOURCE GROUNDED</span><h2>{feature.title}</h2><p>{feature.summary}</p>
      <div className="feature-metrics"><Metric value={source.total} label="supporting declarations"/><Metric value={flow.nodes.length} label="functions in bounded flow"/><Metric value={flow.edges.length} label="resolved call sites"/></div>
    </header>
    <section className="feature-context-section"><div className="feature-section-heading"><div><span className="section-kicker">IMPLEMENTATION EVIDENCE</span><h3>How the code supports this feature</h3></div><p>Descriptions come from source comments or deterministic signatures. Open a declaration to inspect its captured body.</p></div>
      <div className="feature-support-list">{source.items.map(item=><SupportingDeclaration key={item.symbol.id} item={item} node={nodes.get(item.symbol.id)} open={openSymbol}/>)}</div>
      {!source.items.length && <p className="muted-note">This inferred feature has no canonical declaration evidence in the current generation.</p>}
    </section>
    <section className="feature-context-section"><div className="feature-section-heading"><div><span className="section-kicker">RESOLVED FLOW</span><h3>Entry evidence and shared dependencies</h3></div><p>Static calls show code relationships, not runtime order.</p></div>
      <FlowOverview flow={flow} open={openSymbol}/><Button onClick={()=>onFlow(feature.id)}>Explore the full call flow →</Button>
    </section>
    <FeatureLimitations bundle={bundle}/>
  </article>;
}

function Metric({value,label}) { return <div><strong>{value}</strong><span>{label}</span></div>; }

function SupportingDeclaration({item,node,open}) {
  const symbol = item.symbol;
  const description = node?.description || sourceSummary(symbol) || signatureDescription(symbol);
  return <article className="feature-support-card"><div className="feature-support-copy"><div className="feature-support-title"><span className="detail-tag">{kindLabel(symbol.kind)}</span><h4>{symbol.name}</h4></div><p className="mono">{item.path}:{symbol.span.start.line}</p><p>{description}</p>
    {node && <span className="feature-call-counts">{node.incoming_count} callers · {node.outgoing_count} local callees · {node.unresolved_count} unresolved or external calls</span>}</div>
    <div className="feature-support-actions"><Button onClick={()=>open(symbol.id)}>Inspect declaration</Button><EditorLink path={item.path} position={symbol.span.start}>Open in editor ↗</EditorLink></div></article>;
}

function signatureDescription(symbol) {
  const signature = symbol.signature || symbol.name;
  return `Declared as ${signature}. No behavior description was found in captured source comments.`;
}

function FlowOverview({flow,open}) {
  const seeds = flow.nodes.filter(node=>node.seed);
  const shared = flow.nodes.filter(node=>node.incoming_count > 1 && !node.seed).slice(0,6);
  return <div className="feature-flow-overview"><NodeGroup title="Feature evidence seeds" nodes={seeds} open={open}/><NodeGroup title="Shared dependencies" nodes={shared} open={open} empty="No shared dependency is visible inside this bounded flow."/></div>;
}

function NodeGroup({title,nodes,open,empty='No entry evidence is visible inside this bounded flow.'}) {
  return <div className="feature-node-group"><h4>{title}</h4>{nodes.length ? nodes.map(node=><Button className="feature-node" key={node.symbol.id} onClick={()=>open(node.symbol.id)}><strong>{node.symbol.name}</strong><span>{node.path}:{node.symbol.span.start.line}</span><small>{node.description}</small></Button>) : <p className="muted-note">{empty}</p>}</div>;
}

function FeatureLimitations({bundle}) {
  const items = [...(bundle.limitations || []),...(bundle.flow.limitations || [])];
  return <details className="feature-limitations"><summary>Evidence boundaries</summary><ul>{items.map((text,index)=><li key={index}>{text}</li>)}</ul></details>;
}

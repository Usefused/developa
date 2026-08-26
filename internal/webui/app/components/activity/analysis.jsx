import {count} from '../../../assets/model.js';
import {EvidenceTable} from './evidence-table.jsx';

export function Analysis({details}) {
  const {snapshot} = details;
  return <><section className="analysis-overview"><div className="analysis-heading"><h2>Structural facts. Clear boundaries.</h2><span className="detail-tag">{snapshot.completeness}</span></div>
    <p>Declarations come from Go syntax. Call analysis uses captured local packages and conservative type checking; it never executes the repository. AI feature descriptions are separate, cited inferences.</p>
    <p>Source capture: {snapshot.source_complete ? 'complete within capture policy' : 'partial'}. {count(snapshot.diagnostic_count)} parser diagnostics · {count(snapshot.exclusion_count)} excluded paths.</p>
    <Limitations items={details.limitations}/></section><CallAnalysis analysis={details.call_analysis}/><Diagnostics items={details.diagnostics || []}/><Exclusions items={details.exclusions || []}/></>;
}

function Limitations({items=[]}) { return <ul>{items.map((text,index)=><li key={index}>{text}</li>)}</ul>; }

function Diagnostics({items}) {
  return <section className="analysis-section"><h3>Parser diagnostics ({items.length})</h3>{!items.length && <p className="section-description">No syntax diagnostics in this snapshot. This is not a type-check or build result.</p>}
    <div className="diagnostic-list">{items.map((item,index)=><article key={index} className="diagnostic-item"><div className="mono">{item.path}:{item.position.line}</div><p>{item.message}</p></article>)}</div></section>;
}

function Exclusions({items}) {
  return <section className="analysis-section"><h3>Capture exclusions ({items.length})</h3>{items.length ? <EvidenceTable headers={['Reason','Repository path']} rows={items.map(item=>[item.reason.replaceAll('_',' '),item.path])}/> : <p className="section-description">No additional exclusions beyond Git’s ignore rules.</p>}</section>;
}

function CallAnalysis({analysis}) {
  if (!analysis?.status) return <section className="analysis-overview"><h3>Call analysis not available</h3><p>This older snapshot predates call resolution. Select a newly indexed snapshot.</p></section>;
  return <section className="analysis-overview"><h3>Call resolution · {analysis.status}</h3><p>{count(analysis.resolved)} resolved · {count(analysis.unresolved)} unresolved call sites</p><Limitations items={analysis.limitations}/>{!!analysis.diagnostics?.length && <Diagnostics items={analysis.diagnostics}/>}</section>;
}

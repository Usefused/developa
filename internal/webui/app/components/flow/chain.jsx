import {chainLevels} from '../../../assets/chains.js';
import {usePageNavigation} from '../../hooks/use-workspace.jsx';
import {Button,EmptyState} from '../common.jsx';

export function Chain({chain,calls}) {
  const {params,update,go,openSymbol} = usePageNavigation();
  return <><div className="chain-controls"><Button className="text-button" onClick={()=>go('blocks',{file:params.get('file')})}>← Back to file</Button>
    {[['out','Calls →'],['in','← Used by']].map(([direction,label])=><Button key={direction} className={chain.direction === direction ? 'primary-button' : 'secondary-button'} aria-pressed={chain.direction === direction} onClick={()=>update({direction})}>{label}</Button>)}</div>
    <p className="section-description">Up to {chain.depth} call steps. Only resolved captured targets are traversed. Cycles share one block.</p>
    <div className="chain-levels">{chainLevels(chain).map((items,index)=><section key={index} className="chain-level"><h3 className="section-kicker">{index ? `STEP ${index}` : 'START HERE'}</h3>{items.map(item=><Button key={item.symbol.id} className="chain-block" onClick={()=>openSymbol(item.symbol.id)}><strong>{item.symbol.name}</strong><span className="mono">{item.path}:{item.symbol.span.start.line}</span></Button>)}</section>)}</div>
    {chain.truncated && <p className="coverage-note">This view reached its node, edge or depth limit. Open a block and follow its chain to continue.</p>}
    {!chain.edges.length && <EmptyState title="No resolved connections in this direction">This is not proof the declaration is unused; unresolved targets may leave gaps.</EmptyState>}
    <section className="analysis-section"><h3>{calls.total} direct call sites</h3>{calls.items.map((call,index)=><CallRow key={index} call={call} incoming={chain.direction === 'in'}/>)}{calls.total > calls.items.length && <p className="muted-note">Showing the first {calls.items.length} call sites. The API supports pagination.</p>}</section>
  </>;
}

function CallRow({call,incoming}) {
  const {openSymbol} = usePageNavigation();
  const id = incoming ? call.caller_id : call.target_id;
  const label = incoming ? call.caller_name : call.target_name;
  return <div className="call-row">{id ? <Button className="text-button" onClick={()=>openSymbol(id)}>{label || id.slice(0,8)}</Button> : <span className="mono">{label || 'Unknown target'}</span>}<span className="detail-tag">{call.resolution}</span><span className="mono">{call.path}:{call.span.start.line}</span>{call.reason && <p className="muted-note">{call.reason.replaceAll('_',' ')}</p>}</div>;
}

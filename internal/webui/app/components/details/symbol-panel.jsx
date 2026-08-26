import {kindLabel,shortHash} from '../../../assets/model.js';
import {sourceSummary,documentationNote} from '../../../assets/documentation.js';
import {reviewable} from '../../../assets/reviews.js';
import {useSession} from '../../hooks/use-session.jsx';
import {useWorkspace,usePageNavigation} from '../../hooks/use-workspace.jsx';
import {useResource} from '../../hooks/use-resource.js';
import {Resource,Button,Section} from '../common.jsx';
import {Members} from './members.jsx';
import {EditorActions} from './editor-actions.jsx';
import {SourceViewer} from './source-viewer.jsx';
import {Explanation} from '../intelligence/explanation.jsx';
import {FunctionReviews,SavedReview} from '../intelligence/function-reviews.jsx';

export function SymbolPanel({id}) {
  const {api} = useSession();
  const {snapshot} = useWorkspace();
  const {update,openSymbol,go} = usePageNavigation();
  const record = useResource(`symbol:${snapshot.id}:${id}`,signal=>api.symbol(snapshot.id,id,signal));
  return <><Button className="quiet-button detail-close" aria-label="Close symbol details" onClick={()=>update({symbol:null})}>×</Button><Resource state={record}>{item=><SymbolDetails item={item} snapshot={snapshot} open={openSymbol} go={go} updated={record.refresh}/>}</Resource></>;
}

function SymbolDetails({item,snapshot,open,go,updated}) {
  const {symbol} = item;
  return <><span className="section-kicker">INSIDE THE BLOCK</span><h2 className="detail-name">{symbol.name}</h2><p className="detail-location">{item.path}:{symbol.span.start.line}:{symbol.span.start.column}</p>
    <div className="detail-tags">{[kindLabel(symbol.kind),symbol.visibility,'Go'].map(tag=><span className="detail-tag" key={tag}>{tag}</span>)}</div><pre className="signature">{symbol.signature}</pre>
    <Section title="Source summary"><p className="source-summary">{sourceSummary(symbol) || 'No source comments available for this declaration.'}</p><p className="muted-note">{documentationNote(symbol)}</p></Section>
    {item.review && <SavedReview review={item.review}/>}<Explanation target={{type:'symbol',id:symbol.id}}/><Members symbol={symbol} review={item.review} open={open}/>
    {reviewable(symbol) && <FunctionReviews item={item} updated={updated}/>}<EditorActions item={item}/><Relationships item={item} go={go}/>
    {symbol.source && <details className="source-disclosure"><summary>Captured implementation</summary><SourceViewer symbol={symbol} path={item.path}/></details>}
    <div className="detail-foot">SNAPSHOT {shortHash(snapshot.id)} · SOURCE {shortHash(symbol.source_id)}<br/>Physical UTF-8 byte columns · syntax analysis</div>
  </>;
}

function Relationships({item,go}) {
  if (!reviewable(item.symbol)) return null;
  return <div className="relationship-note"><strong>Where does this lead?</strong><p>Follow resolved calls or captured callers. Dynamic dispatch remains explicitly unresolved.</p><div className="detail-actions">
    <Button className="primary-button" onClick={()=>go('flow',{root:item.symbol.id,symbol:item.symbol.id})}>View code flow ↓</Button>
    <Button onClick={()=>go('chain',{root:item.symbol.id,direction:'out',file:item.path,symbol:item.symbol.id})}>Follow chain →</Button>
    <Button onClick={()=>go('chain',{root:item.symbol.id,direction:'in',file:item.path,symbol:item.symbol.id})}>View callers ←</Button>
  </div></div>;
}

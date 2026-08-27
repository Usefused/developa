import {useEffect,useState} from 'react';
import {useNavigate} from 'react-router';
import {loadFeaturePage} from '../../assets/feature-state.js';
import {useSession} from '../hooks/use-session.jsx';
import {useWorkspace,usePageNavigation} from '../hooks/use-workspace.jsx';
import {useResource} from '../hooks/use-resource.js';
import {useCapabilities} from '../hooks/use-capabilities.js';
import {offsetOf,pageURL} from '../lib/routes.js';
import {ExplorerFrame} from '../components/explorer-frame.jsx';
import {FeatureCard} from '../components/features/feature-card.jsx';
import {FeatureProgress} from '../components/features/progress.jsx';
import {FeatureWorkspace} from '../components/features/feature-workspace.jsx';
import {Resource,Pagination,EmptyState,Button} from '../components/common.jsx';

export default function FeaturesRoute() {
  const {api} = useSession();
  const {snapshot,repositoryID} = useWorkspace();
  const {params,update,go} = usePageNavigation();
  const navigate = useNavigate();
  const offset = offsetOf(params);
  const selected = params.get('feature') || '';
  const query = params.get('q') || '';
  const preferSaved = params.get('saved') === '1';
  const capabilities = useCapabilities();
  const resource = useResource(selected ? '' : `features:${snapshot.id}:${query}:${offset}:${preferSaved}`,signal=>loadFeaturePage(api,snapshot,offset,{signal,current:()=>!signal.aborted},preferSaved,query));
  const context = useResource(selected ? `feature-context:${snapshot.id}:${selected}` : '',signal=>api.featureContext(snapshot.id,selected,{source_limit:20,depth:6,flow_limit:80},signal));
  useEffect(()=>{
    const saved = resource.data?.snapshot;
    if (saved && saved.id !== snapshot.id) navigate(pageURL('features',saved.id,{repo:repositoryID}),{replace:true,preventScrollReset:true});
  },[resource.data,snapshot.id,repositoryID,navigate]);
  if (selected) return <ExplorerFrame><Resource state={context}>{bundle=><FeatureWorkspace bundle={bundle} onBack={()=>update({feature:null,symbol:null})} onFlow={id=>go('flow',{mode:'feature',feature:id})}/>}</Resource></ExplorerFrame>;
  return <ExplorerFrame><Resource state={resource}>{result=>result.snapshot.id === snapshot.id && <>
    <FeatureProgress key={snapshot.id} page={result.page} capabilities={capabilities.data || {}} refresh={resource.refresh} refreshing={resource.pending}/>
    <FeatureSearch value={query} search={value=>update({q:value,offset:null})}/><FeatureList page={result.page} onOpen={id=>update({feature:id,symbol:null})} onSaved={id=>navigate(pageURL('features',id,{repo:repositoryID}))}/>
    <Pagination page={result.page} onChange={next=>update({offset:next,symbol:null})}/>
  </>}</Resource></ExplorerFrame>;
}

function FeatureSearch({value,search}) {
  const [draft,setDraft] = useState(value);
  useEffect(()=>setDraft(value),[value]);
  return <form className="feature-search" onSubmit={event=>{event.preventDefault();search(draft.trim());}}><label htmlFor="feature-search">Search saved features</label><div><input id="feature-search" value={draft} onChange={event=>setDraft(event.target.value)} placeholder="Authentication, workspace indexing, API access…"/><Button type="submit">Search</Button>{value && <Button className="text-button" onClick={()=>{setDraft('');search('');}}>Clear</Button>}</div></form>;
}

function FeatureList({page,onOpen,onSaved}) {
  if (!page.items.length) return <><EmptyState title="No saved features for this snapshot">Queue analysis explicitly to analyze this source; browsing never invokes the model.</EmptyState>{page.saved_snapshot && <Button onClick={()=>onSaved(page.saved_snapshot.id)}>Open saved analysis · {page.saved_snapshot.id.slice(0,8)}</Button>}</>;
  return <><p className="section-description">{page.total} inferred features · Select a capability to inspect its evidence</p><div className="feature-grid">{page.items.map(feature=><FeatureCard key={feature.id} feature={feature} onOpen={onOpen}/>)}</div></>;
}

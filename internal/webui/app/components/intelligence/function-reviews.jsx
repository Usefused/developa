import {useState} from 'react';
import {reviewRequest,reviewRange} from '../../../assets/reviews.js';
import {sourceSummary} from '../../../assets/documentation.js';
import {useSession} from '../../hooks/use-session.jsx';
import {useWorkspace,usePageNavigation} from '../../hooks/use-workspace.jsx';
import {useResource} from '../../hooks/use-resource.js';
import {useCapabilities} from '../../hooks/use-capabilities.js';
import {useAction} from '../../hooks/use-action.js';
import {Section,Button,ErrorNotice} from '../common.jsx';

export function SavedReview({review}) {
  return <Section title="Saved card description" className="saved-review"><span className="detail-tag">AI · SAVED</span><p>{review.summary}</p><p className="muted-note">{review.model}</p>{review.context_truncated && <p className="muted-note">Based on incomplete captured evidence.</p>}</Section>;
}

export function FunctionReviews({item,updated}) {
  const {api,cache} = useSession();
  const {snapshot} = useWorkspace();
  const capabilities = useCapabilities();
  const [offset,setOffset] = useState(0);
  const action = useAction();
  const page = useResource(`reviews:${snapshot.id}:${item.symbol.id}:${offset}`,signal=>api.reviews(snapshot.id,reviewRequest(item.symbol.id,true,offset),signal));
  const enabled = !!capabilities.data?.function_review_generation;
  function generate(callees) {
    action.run(async signal=>{
      const result = await api.reviewStream(snapshot.id,reviewRequest(item.symbol.id,callees,callees ? offset : 0),signal);
      if (!signal.aborted) { cache.clear(); page.refresh(); updated(); }
      return result;
    });
  }
  return <Section title="AI card metadata" className="ai-review-section">
    <Button disabled={action.pending || !enabled} onClick={()=>generate(false)}>{item.review ? 'Update card description + parameter notes' : 'Generate card description + parameter notes'}</Button>
    <ErrorNotice error={action.error}/>{action.pending && <p role="status">Reviewing this batch. Results appear after validation and saving…</p>}
    {page.data?.total > 0 && <CalleeReviews page={page.data} pending={action.pending || page.pending} enabled={enabled} generate={()=>generate(true)} onPage={setOffset}/>}
  </Section>;
}

function CalleeReviews({page,pending,enabled,generate,onPage}) {
  return <div className="callee-reviews"><h4>Called functions</h4>
    {page.items.map(item=><CalleeReview key={item.symbol.id} item={item}/>)}
    {!!page.items.length && <Button disabled={pending || !enabled} onClick={generate}>Summarize these functions with AI</Button>}
    <div className="review-pagination"><span className="muted-note">Showing {reviewRange(page)}</span>
      {page.options.offset > 0 && <Button disabled={pending} onClick={()=>onPage(Math.max(0,page.options.offset-4))}>Previous</Button>}
      {page.next_offset !== null && <Button disabled={pending} onClick={()=>onPage(page.next_offset)}>Next batch →</Button>}</div>
  </div>;
}

function CalleeReview({item}) {
  const {openSymbol} = usePageNavigation();
  return <div className="callee-review"><Button className="text-button" onClick={()=>openSymbol(item.symbol.id)}>{item.symbol.name}</Button><span className="mono">{item.path}:{item.symbol.span.start.line}</span>
    {sourceSummary(item.symbol) && <><span className="detail-tag">SOURCE SUMMARY</span><p className="callee-comment">{sourceSummary(item.symbol)}</p></>}{item.review && <SavedReview review={item.review}/>}</div>;
}

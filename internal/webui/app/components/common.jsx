import {pageLabel} from '../../assets/model.js';

export function Button({children,className='secondary-button',...props}) { return <button type="button" className={className} {...props}>{children}</button>; }
export function EmptyState({title,children}) { return <div className="empty-state"><h2>{title}</h2><p>{children}</p></div>; }
export function ErrorNotice({error}) {
  if (!error) return null;
  return <div className="error-banner" role="alert">{error.message}{error.traceID && <span> · Trace {error.traceID}</span>}</div>;
}
export function Resource({state,children}) {
  return <><ErrorNotice error={state.error}/>{state.data === undefined ? <EmptyState title={state.pending ? 'Loading source records…' : 'This view is unavailable'}>Your saved index is unchanged.</EmptyState> : children(state.data)}</>;
}
export function Section({title,children}) { return <section className="detail-section"><h3>{title}</h3>{children}</section>; }
export function Pagination({page,onChange}) {
  if (!page || page.total <= page.limit) return null;
  return <div className="pagination"><span>{pageLabel(page)}</span><div className="page-actions">
    <Button disabled={!page.offset} onClick={()=>onChange(Math.max(0,page.offset-page.limit))}>← Previous</Button>
    <Button disabled={page.offset+page.limit >= page.total} onClick={()=>onChange(page.offset+page.limit)}>Next →</Button>
  </div></div>;
}

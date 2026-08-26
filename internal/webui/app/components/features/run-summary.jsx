import {count,dateLabel} from '../../../assets/model.js';

export function RunSummary({run}) {
  if (!run) return null;
  return <div className="run-summary"><span className="detail-tag">{run.status}</span><p>{count(run.analyzed_symbols)} of {count(run.total_symbols)} symbol records analyzed · {run.model} · {dateLabel(run.created_at)}</p>
    <p>{count(run.cached_batches)} pages reused from cache · {count(run.model_calls)} model calls in this generation</p><ul>{(run.limitations || []).map((text,index)=><li key={index}>{text}</li>)}</ul></div>;
}

import {generationLabel,jobActive,analysisPolicy,modelDisclosure} from '../../../assets/intelligence.js';
import {featurePageChanged} from '../../../assets/feature-state.js';
import {useSession} from '../../hooks/use-session.jsx';
import {useWorkspace} from '../../hooks/use-workspace.jsx';
import {useAction} from '../../hooks/use-action.js';
import {useAnalysisJob} from '../../hooks/use-analysis-job.js';
import {Button,ErrorNotice} from '../common.jsx';
import {RunSummary} from './run-summary.jsx';

// SSE state stays in this component: saved cards and the page's scroll/layout
// do not unmount or refetch when the worker publishes progress.
export function FeatureProgress({page,capabilities,refresh,refreshing}) {
  const {api} = useSession();
  const {snapshot} = useWorkspace();
  const {job,setJob,error} = useAnalysisJob(snapshot.id);
  const queue = useAction();
  function generate() {
    queue.run(async signal=>{
      const result = await api.discover(snapshot.id,signal);
      if (!signal.aborted) setJob(result);
      return result;
    });
  }
  return <><section className="analysis-overview"><div className="analysis-heading"><h2>Capabilities with evidence</h2><Button className="primary-button" onClick={generate} disabled={queue.pending || !capabilities.analysis_jobs || jobActive(job)}>{queue.pending ? 'Queueing analysis…' : generationLabel(page.run,job)}</Button></div>
    <p>Model-generated descriptions are inferences. Review the cited implementation before relying on a claim.</p><p className="model-setup">{modelDisclosure(capabilities)}</p>
    <div className="feature-progress" role="status"><span className="detail-tag">{job?.status || 'not_queued'}</span><p>{analysisPolicy(capabilities)}</p><JobStats job={job}/></div>
    <RunSummary run={page.run}/><ErrorNotice error={queue.error || error}/></section>
    <div className="feature-refresh"><p className="muted-note">{featurePageChanged(page,job) ? 'New results are saved. Refresh when you are ready to load them.' : 'Showing saved results. Live progress does not move or replace these cards.'}</p><Button disabled={refreshing} onClick={refresh}>Refresh saved features</Button></div>
  </>;
}

function JobStats({job}) {
  if (!job?.id) return null;
  return <><p>{job.chunks} saved pages · {job.analyzed_symbols} of {job.total_symbols} symbols · {job.feature_count} inferred features</p>{job.error_code && <p>The worker encountered an error. Transient failures retry with a bounded delay; failed jobs can be queued again.</p>}</>;
}

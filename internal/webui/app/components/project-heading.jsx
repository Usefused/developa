import {useLocation,useNavigate} from 'react-router';
import {count,shortHash,dateLabel} from '../../assets/model.js';
import {headings,pageURL} from '../lib/routes.js';
import {useAction} from '../hooks/use-action.js';
import {useSession} from '../hooks/use-session.jsx';
import {useWorkspace} from '../hooks/use-workspace.jsx';
import {Button,ErrorNotice} from './common.jsx';

export function ProjectHeading() {
  const {api} = useSession();
  const {project,snapshot,repositoryID,refreshProject,scanQueued,scanning} = useWorkspace();
  const scan = useAction();
  const navigate = useNavigate();
  const page = useLocation().pathname.slice(1) || 'blocks';
  const [title,description] = headings[page] || headings.blocks;
  return <><div className="page-heading"><div><div className="eyebrow-line"><span className="section-kicker">REPOSITORY EXPLORER</span><span className="branch-label">{snapshot?.branch || 'detached HEAD'}</span></div>
    <h1>{title}</h1><p>{description}</p></div><div className="page-actions"><Button onClick={refreshProject}>Check for changes</Button><Button disabled={scan.pending || scanning} onClick={()=>scan.run(async signal=>{
      const result = await api.scan(signal);
      if (!signal.aborted) scanQueued();
      return result;
    })}>{scan.pending ? 'Queueing…' : '↻ Reindex'}</Button></div></div>
    <ErrorNotice error={scan.error}/>{scan.data && <p role="status">Scan queued · execution {shortHash(scan.data.id)}. The tracker publishes changes in the background.</p>}
    <SnapshotSummary snapshot={snapshot}/>
    <SnapshotUpdate latest={project.snapshot} snapshot={snapshot} show={()=>navigate(pageURL(page,project.snapshot.id,{repo:repositoryID}))}/>
  </>;
}

function SnapshotUpdate({latest,snapshot,show}) {
  if (!latest || !snapshot || latest.id === snapshot.id) return null;
  return <div className="update-banner"><span>A newer snapshot is ready. This page remains pinned to its source.</span><Button className="text-button" onClick={show}>Show latest →</Button></div>;
}

function SnapshotSummary({snapshot}) {
  if (!snapshot) return null;
  return <div className="project-summary">{[['Go files','file_count'],['symbols','symbol_count'],['packages','package_count']].map(([label,key])=><div key={key}><strong>{count(snapshot[key])}</strong><span>{label}</span></div>)}
    <span className="snapshot-label">{shortHash(snapshot.commit)} · indexed {dateLabel(snapshot.indexed_at)} · snapshot {shortHash(snapshot.id)}</span></div>;
}

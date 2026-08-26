import {EmptyState} from '../common.jsx';
import {EvidenceTable} from './evidence-table.jsx';

export function Changes({details}) {
  const changes = details.changes || [];
  if (!details.snapshot.changes_known) return <EmptyState title="A baseline, ready for what’s next">This capture has no earlier in-process source manifest to compare with. Future captured changes will appear here.</EmptyState>;
  if (!changes.length) return <EmptyState title="No file-content changes">The snapshot may have changed because of Git refs, staging or other repository metadata.</EmptyState>;
  return <><p className="section-description">{changes.length} changed files since the previous published capture. Renames appear as an addition and a deletion.</p><EvidenceTable headers={['Change','Repository path']} rows={changes.map(change=>[<span key="kind" className={`change-kind ${change.kind}`}>{change.kind}</span>,<span key="path" className="mono">{change.path}</span>])}/></>;
}

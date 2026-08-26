import {useSession} from '../../hooks/use-session.jsx';
import {useWorkspace} from '../../hooks/use-workspace.jsx';
import {useResource} from '../../hooks/use-resource.js';
import {ExplorerFrame} from '../explorer-frame.jsx';
import {Resource} from '../common.jsx';

export function SnapshotPage({children}) {
  const {api} = useSession();
  const {snapshot} = useWorkspace();
  const details = useResource(`details:${snapshot.id}`,signal=>api.details(snapshot.id,signal));
  return <ExplorerFrame><Resource state={details}>{children}</Resource></ExplorerFrame>;
}

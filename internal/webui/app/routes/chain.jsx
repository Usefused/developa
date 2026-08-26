import {useSession} from '../hooks/use-session.jsx';
import {useWorkspace,usePageNavigation} from '../hooks/use-workspace.jsx';
import {useResource} from '../hooks/use-resource.js';
import {ExplorerFrame} from '../components/explorer-frame.jsx';
import {Chain} from '../components/flow/chain.jsx';
import {Resource,EmptyState} from '../components/common.jsx';

export default function ChainRoute() {
  const {api} = useSession();
  const {snapshot} = useWorkspace();
  const {params} = usePageNavigation();
  const root = params.get('root');
  const direction = params.get('direction') === 'in' ? 'in' : 'out';
  const result = useResource(root ? `chain:${snapshot.id}:${root}:${direction}` : '',async signal=>{
    const [chain,calls] = await Promise.all([api.chain(snapshot.id,root,{direction,depth:2,limit:40},signal),api.calls(snapshot.id,{symbol_id:root,direction,limit:50,offset:0},signal)]);
    return {chain,calls};
  });
  return <ExplorerFrame>{root ? <Resource state={result}>{data=><Chain {...data}/>}</Resource> : <EmptyState title="Choose a function first">Open its source details to follow a call chain.</EmptyState>}</ExplorerFrame>;
}

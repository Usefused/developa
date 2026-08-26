import {useSession} from '../hooks/use-session.jsx';
import {useWorkspace,usePageNavigation} from '../hooks/use-workspace.jsx';
import {useResource} from '../hooks/use-resource.js';
import {flowOptions} from '../lib/routes.js';
import {ExplorerFrame} from '../components/explorer-frame.jsx';
import {FeatureDetails} from '../components/features/feature-details.jsx';
import {FlowToolbar} from '../components/flow/toolbar.jsx';
import {FlowContent} from '../components/flow/content.jsx';
import {Resource,EmptyState,ErrorNotice} from '../components/common.jsx';

export default function FlowRoute() {
  const {api} = useSession();
  const {snapshot} = useWorkspace();
  const {params} = usePageNavigation();
  const options = flowOptions(params);
  const featureMode = params.get('mode') === 'feature' || !!options.feature_id;
  const chooser = featureMode && !options.feature_id;
  const flow = useResource(chooser ? '' : `flow:${snapshot.id}:${JSON.stringify(options)}`,signal=>api.flow(snapshot.id,options,signal));
  const feature = useResource(options.feature_id ? `feature:${snapshot.id}:${options.feature_id}` : '',signal=>api.feature(snapshot.id,options.feature_id,signal));
  const evidence = feature.data && !params.has('hideEvidence') ? <FeatureDetails key={feature.data.id} feature={feature.data}/> : null;
  return <ExplorerFrame evidence={evidence}><section className="flow-view"><FlowToolbar featureMode={featureMode} feature={feature.data} depth={options.depth}/><ErrorNotice error={feature.error}/>
    {chooser ? <EmptyState title="Choose a feature to trace">Search saved features above to see their source call flow. This picker never invokes AI.</EmptyState> : <Resource state={flow}>{data=><FlowContent flow={data} feature={feature.data}/>}</Resource>}
  </section></ExplorerFrame>;
}

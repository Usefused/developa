import {useMemo} from 'react';
import {featureOptions} from '../../../assets/select-data.js';
import {useSession} from '../../hooks/use-session.jsx';
import {useWorkspace,usePageNavigation} from '../../hooks/use-workspace.jsx';
import {Button} from '../common.jsx';
import {SearchSelect} from '../search-select.jsx';

export function FlowToolbar({featureMode,feature,depth}) {
  const {api} = useSession();
  const {snapshot} = useWorkspace();
  const {update,go} = usePageNavigation();
  const loadPage = useMemo(()=>featureOptions(api,snapshot.id),[api,snapshot.id]);
  const selected = useMemo(()=>feature ? {value:feature.id,label:feature.title} : null,[feature]);
  return <div className="flow-toolbar"><div className="flow-modes" aria-label="Flow mode"><Button className={featureMode ? '' : 'active'} aria-pressed={!featureMode} onClick={()=>go('flow',{depth})}>Code flow</Button><Button className={featureMode ? 'active' : ''} aria-pressed={featureMode} onClick={()=>go('flow',{mode:'feature',depth})}>Feature flow</Button></div>
    {featureMode && <div className="flow-feature-picker"><span>Feature</span><SearchSelect label="Choose feature" selected={selected} placeholder="Search saved features…" loadPage={loadPage} onChange={id=>update({feature:id,root:null,symbol:null,hideEvidence:null})}/></div>}
    <label className="flow-depth"><span>Traversal depth</span><select aria-label="Traversal depth" value={depth} onChange={event=>update({depth:event.target.value})}>{[4,6,8,12].map(value=><option key={value} value={value}>{value}</option>)}</select></label>
    {featureMode && <Button className="text-button" onClick={()=>go('features')}>All features</Button>}
  </div>;
}

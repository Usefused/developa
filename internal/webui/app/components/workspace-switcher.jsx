import {useMemo,useState} from 'react';
import {useLocation,useNavigate} from 'react-router';
import {useSession} from '../hooks/use-session.jsx';
import {workspaceURL} from '../lib/routes.js';
import {SearchSelect} from './search-select.jsx';
import {Button} from './common.jsx';
import {AddWorkspaceDialog} from './workspaces/add-workspace-dialog.jsx';

export function WorkspaceSwitcher({repositoryID,repository,setRepositoryPreferences}) {
  const {api,unauthorized} = useSession();
  const {pathname} = useLocation();
  const navigate = useNavigate();
  const [adding,setAdding] = useState(false);
  const loadPage = useMemo(()=>repositoryOptions(api,unauthorized),[api,unauthorized]);
  const selected = useMemo(()=>({value:repositoryID,label:repository?.name || 'Choose workspace'}),[repositoryID,repository]);
  return <div className="workspace-picker"><span className="workspace-picker-label">Workspace</span>
    <SearchSelect label="Workspace" selected={selected} placeholder="Search repositories…" loadPage={loadPage}
      onChange={id=>{if (id !== repositoryID) navigate(workspaceURL(pathname,id));}}/>
    <Button className="quiet-button add-workspace-button" onClick={()=>setAdding(true)}>+ Add workspace</Button>
    {adding && <AddWorkspaceDialog close={()=>setAdding(false)} added={(id,root)=>{if (root) setRepositoryPreferences?.(id,current=>({...current,root}));setAdding(false);navigate(workspaceURL(pathname,id));}}/>}
  </div>;
}

function repositoryOptions(api, unauthorized) {
  return async(q,offset,signal)=>{
    try {
      const page = await api.repositories({q,offset,limit:24},signal);
      return {...page,items:page.items.map(repo=>({value:repo.id,label:`${repo.name} · ${repo.id.slice(0,8)}`}))};
    } catch (error) {
      if (!signal.aborted && error.status === 401) unauthorized?.(error);
      throw error;
    }
  };
}

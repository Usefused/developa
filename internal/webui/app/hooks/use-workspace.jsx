import {createContext,useCallback,useContext,useEffect,useRef,useState} from 'react';
import {useLocation,useNavigate,useSearchParams} from 'react-router';
import {snapshotPin,projectRefreshInterval} from '../../assets/model.js';
import {pageURL,updateSearch} from '../lib/routes.js';
import {useResource} from './use-resource.js';
import {useSession} from './use-session.jsx';

export const WorkspaceContext = createContext(null);
export const useWorkspace = ()=>useContext(WorkspaceContext);

export function useProject() {
  const {api} = useSession();
  const project = useResource('project',signal=>api.project(signal));
  const requested = useRef(null);
  const [scanning,setScanning] = useState(false);
  useEffect(()=>{
    if (project.data !== requested.current && project.data?.status !== 'scanning') setScanning(false);
  },[project.data]);
  useEffect(()=>{
    if (!project.data) return;
    const timer = setTimeout(project.refresh,projectRefreshInterval(project.data,scanning));
    return ()=>clearTimeout(timer);
  },[project.data,project.refresh,scanning]);
  const scanQueued = useCallback(()=>{
    requested.current = project.data;
    setScanning(true);
    project.refresh();
  },[project.data,project.refresh]);
  return {...project,scanQueued,scanning};
}

export function useSnapshot(project) {
  const {api} = useSession();
  const repositoryID = api.repositoryID;
  const [params,setParams] = useSearchParams();
  const pin = snapshotPin(params.toString());
  const details = useResource(pin ? `details:${pin}` : '',signal=>api.details(pin,signal));
  const snapshot = selectedSnapshot(pin,details.data?.snapshot,project?.snapshot);
  useEffect(()=>{
    const values = {};
    if (repositoryID && !params.get('repo')) values.repo = repositoryID;
    if (!pin && snapshot) values.snapshot = snapshot.id;
    if (!Object.keys(values).length) return;
    // A URL pins every page, including the initial latest view, before reads
    // can silently drift when the repository watcher publishes new source.
    setParams(previous=>updateSearch(previous,values),{replace:true,preventScrollReset:true});
  },[pin,snapshot,repositoryID,params,setParams]);
  return {snapshot,error:details.error,pending:details.pending};
}

function selectedSnapshot(pin, details, latest) {
  if (!pin) return latest;
  // Pinning the already-rendered latest snapshot must not unmount cards or
  // restart their SSE while the same metadata is being fetched by ID.
  return details || (latest?.id === pin ? latest : undefined);
}

export function usePageNavigation() {
  const {snapshot,repositoryID} = useWorkspace();
  const navigate = useNavigate();
  const location = useLocation();
  const [params,setParams] = useSearchParams();
  const update = (values,replace = false)=>setParams(previous=>updateSearch(previous,values),{replace,preventScrollReset:true});
  const go = (page,values = {})=>navigate(pageURL(page,snapshot.id,{...values,repo:repositoryID}));
  return {params,update,go,page:location.pathname.slice(1),openSymbol:id=>update({symbol:id})};
}

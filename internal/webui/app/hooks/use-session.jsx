import {createContext,useCallback,useContext,useEffect,useMemo,useRef,useState} from 'react';
import {API} from '../../assets/api.js';
import {ReadCache} from '../lib/cache.js';
import {savedAPIToken,rememberAPIToken} from '../lib/auth-storage.js';

export const SessionContext = createContext(null);
export const useSession = ()=>useContext(SessionContext);

export function SessionProvider({children}) {
  const [api] = useState(()=>{const client = new API();client.token = savedAPIToken();return client;});
  const [status,setStatus] = useState('connecting');
  const [error,setError] = useState(null);
  const [attempt,setAttempt] = useState(0);
  const [epoch,setEpoch] = useState(0);
  const [defaultRepositoryID,setDefaultRepositoryID] = useState('');
  const connection = useRef(null);
  const cache = useMemo(()=>new ReadCache(),[epoch]);
  const connect = useCallback(token=>{
    connection.current?.abort();
    api.token = token;
    cache.clear();
    setDefaultRepositoryID('');
    setEpoch(value=>value+1);
    setStatus('connecting');
    setAttempt(value=>value+1);
  },[api,cache]);
  const unauthorized = useCallback(reason=>{
    connection.current?.abort();
    api.token = '';
    rememberAPIToken('');
    cache.clear();
    setDefaultRepositoryID('');
    setEpoch(value=>value+1);
    setError(reason);
    setStatus('locked');
  },[api,cache]);
  const lock = useCallback(()=>unauthorized(null),[unauthorized]);
  useEffect(()=>{
    const controller = new AbortController();
    connection.current = controller;
    checkConnection(api,controller.signal,setStatus,setError,setDefaultRepositoryID);
    return ()=>controller.abort();
  },[api,attempt]);
  const value = useMemo(()=>({api,rootApi:api,cache,status,error,connect,lock,unauthorized,epoch,defaultRepositoryID}),[api,cache,status,error,connect,lock,unauthorized,epoch,defaultRepositoryID]);
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function ScopedSessionProvider({repositoryID = '',children}) {
  const session = useSession();
  const source = session.rootApi || session.api;
  const api = useMemo(()=>repositoryID ? source.forRepository(repositoryID) : source,[source,repositoryID]);
  const cache = useMemo(()=>new ReadCache(),[api,session.epoch]);
  useEffect(()=>()=>cache.clear(),[cache]);
  const value = useMemo(()=>({...session,rootApi:source,api,cache}),[session,source,api,cache]);
  // Recreate child hook state as well as cache: equal snapshot IDs are not a
  // shared scope, and a late response must never populate the next repository.
  return <SessionContext.Provider key={`${session.epoch || 0}:${repositoryID}`} value={value}>{children}</SessionContext.Provider>;
}

async function checkConnection(api, signal, status, fail, defaultRepository) {
  fail(null);
  try {
    const info = await api.get('/api/info',signal);
    if (signal.aborted) return;
    if (!info.configured && !info.workspace_management) return status('unconfigured');
    if (info.authentication_required && !api.token) return status('locked');
    const project = await api.project(signal);
    if (signal.aborted) return;
    defaultRepository(projectRepositoryID(project));
    // Persist only an authenticated, current attempt; late or rejected logins
    // must never replace the credential used after a page refresh.
    rememberAPIToken(api.token);
    status('ready');
  } catch (error) {
    if (signal.aborted) return;
    forgetRejectedToken(api,error);
    fail(error);
    status(error.status === 401 ? 'locked' : 'unavailable');
  }
}

function forgetRejectedToken(api,error) {
  if (error.status === 401) { api.token = '';rememberAPIToken(''); }
}

function projectRepositoryID(project) { return typeof project.repository?.id === 'string' ? project.repository.id : ''; }

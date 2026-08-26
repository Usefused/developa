import {useCallback,useEffect,useRef,useState} from 'react';
import {useSession} from './use-session.jsx';

export function useResource(key, load) {
  const {cache,unauthorized} = useSession();
  const loader = useRef(load);
  loader.current = load;
  const [revision,setRevision] = useState(0);
  const force = useRef(false);
  const [state,setState] = useState({key,data:cache.get(key),pending:true,error:null});
  useEffect(()=>{
    if (!key) return;
    const controller = new AbortController();
    const cached = force.current ? undefined : cache.get(key);
    force.current = false;
    if (cached !== undefined) { setState({key,data:cached,pending:false,error:null}); return; }
    setState(previous=>({key,data:previous.key === key ? previous.data : undefined,pending:true,error:null}));
    readResource(loader.current,controller.signal,key,cache,setState,unauthorized);
    return ()=>controller.abort();
  },[key,revision,cache,unauthorized]);
  const refresh = useCallback(()=>{ force.current = true; setRevision(value=>value+1); },[]);
  if (!key) return {data:undefined,pending:false,error:null,refresh};
  const current = state.key === key ? state : {data:cache.get(key),pending:true,error:null};
  return {...current,refresh};
}

async function readResource(load, signal, key, cache, update, unauthorized) {
  try {
    const data = await load(signal);
    if (signal.aborted) return;
    cache.set(key,data);
    update({key,data,pending:false,error:null});
  } catch (error) {
    if (signal.aborted) return;
    if (error.status === 401 && unauthorized) return unauthorized(error);
    update(previous=>({...previous,error,pending:false}));
  }
}

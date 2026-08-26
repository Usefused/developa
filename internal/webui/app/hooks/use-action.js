import {useCallback,useEffect,useRef,useState} from 'react';
import {useSession} from './use-session.jsx';

// Mutations run only from an explicit user action; no effect retries a POST.
export function useAction() {
  const session = useSession();
  const unauthorized = session?.unauthorized;
  const controller = useRef(null);
  const [state,setState] = useState({pending:false,error:null,data:null});
  useEffect(()=>()=>controller.current?.abort(),[]);
  const run = useCallback(async execute=>{
    if (controller.current && !controller.current.signal.aborted) return;
    const current = new AbortController();
    controller.current = current;
    setState(previous=>({...previous,pending:true,error:null}));
    try {
      const data = await execute(current.signal);
      if (!current.signal.aborted) setState({pending:false,error:null,data});
      return data;
    } catch (error) {
      if (!current.signal.aborted && error.status === 401 && unauthorized) unauthorized(error);
      if (!current.signal.aborted) setState(previous=>({...previous,pending:false,error}));
    } finally { current.abort(); }
  },[unauthorized]);
  return {...state,run};
}

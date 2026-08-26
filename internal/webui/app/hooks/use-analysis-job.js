import {useEffect,useState} from 'react';
import {watchAnalysis} from '../../assets/job-stream.js';
import {useSession} from './use-session.jsx';

export function useAnalysisJob(snapshot) {
  const {api} = useSession();
  const [job,setJob] = useState(null);
  const [error,setError] = useState(null);
  useEffect(()=>{
    const controller = new AbortController();
    const accept = value=>{ if (!controller.signal.aborted) {setJob(value);setError(null);} };
    const fail = value=>{ if (!controller.signal.aborted) setError(value); };
    watchAnalysis(api,snapshot,accept,fail,controller.signal).catch(fail);
    return ()=>controller.abort();
  },[api,snapshot]);
  return {job,setJob,error};
}

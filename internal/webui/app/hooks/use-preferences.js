import {useCallback,useEffect,useRef,useState} from 'react';
import {bindLegacyRepository,normalizePreferences,preferencesFor,preferencesKey,readPreferences,updatePreferences,writePreferences} from '../lib/preferences.js';

const changedEvent = 'denverr:preferences';

export function usePreferences(repositoryID = '', defaultRepositoryID = '') {
  const [stored,setStored] = useState(readPreferences);
  const current = useRef(stored);
  current.current = stored;
  const preferences = preferencesFor(stored,repositoryID,defaultRepositoryID);
  const setPreferences = useCallback(update=>{
    // Merge the latest record so another mounted scope or tab keeps its overrides.
    const next = updatePreferences(readPreferences(current.current),repositoryID,defaultRepositoryID,update);
    publishPreferences(next,current,setStored);
  },[repositoryID,defaultRepositoryID]);
  const setRepositoryPreferences = useCallback((id,update)=>{
    const next = updatePreferences(readPreferences(current.current),id,defaultRepositoryID,update);
    publishPreferences(next,current,setStored);
  },[defaultRepositoryID]);
  useEffect(()=>{
    const latest = readPreferences(current.current);
    const next = bindLegacyRepository(latest,defaultRepositoryID);
    if (next !== latest) publishPreferences(next,current,setStored);
  },[defaultRepositoryID]);
  useEffect(()=>{
    document.documentElement.dataset.theme = preferences.theme;
  },[preferences.theme]);
  useEffect(()=>{
    const receive = event=>{
      if (event.type === 'storage' && event.key !== preferencesKey && event.key !== null) return;
      const next = event.type === changedEvent ? normalizePreferences(event.detail) : readPreferences();
      current.current = next;
      setStored(next);
    };
    window.addEventListener('storage',receive);
    window.addEventListener(changedEvent,receive);
    return ()=>{ window.removeEventListener('storage',receive); window.removeEventListener(changedEvent,receive); };
  },[]);
  return [preferences,setPreferences,setRepositoryPreferences];
}

function publishPreferences(next, current, setStored) {
  current.current = next;
  writePreferences(next);
  setStored(next);
  window.dispatchEvent(new window.CustomEvent(changedEvent,{detail:next}));
}

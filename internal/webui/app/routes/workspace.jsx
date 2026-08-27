import {useState} from 'react';
import {Outlet,useSearchParams} from 'react-router';
import {SessionProvider,ScopedSessionProvider,useSession} from '../hooks/use-session.jsx';
import {WorkspaceContext,useProject,useSnapshot} from '../hooks/use-workspace.jsx';
import {usePreferences} from '../hooks/use-preferences.js';
import {Shell} from '../components/shell.jsx';
import {Access} from '../components/access.jsx';
import {Settings} from '../components/settings.jsx';
import {ProjectHeading} from '../components/project-heading.jsx';
import {AskAIProvider} from '../components/intelligence/ask-ai.jsx';
import {ErrorNotice,EmptyState} from '../components/common.jsx';

export default function WorkspaceRoute() { return <SessionProvider><Workspace/></SessionProvider>; }

export function Workspace() {
  const {status,epoch,defaultRepositoryID} = useSession();
  const [params] = useSearchParams();
  const repositoryID = params.get('repo') || defaultRepositoryID;
  const [preferences,setPreferences,setRepositoryPreferences] = usePreferences(repositoryID,defaultRepositoryID);
  const [settings,setSettings] = useState(false);
  const shared = {repositoryID,preferences,setRepositoryPreferences,settings:()=>setSettings(true),toggleTheme:()=>setPreferences({...preferences,theme:preferences.theme === 'light' ? 'dark' : 'light'})};
  // Replacing the scope unmounts in-flight reads and streams before another
  // repository can reuse the same snapshot, function or cache key.
  return <>{status === 'ready' ? <ScopedSessionProvider key={`${epoch}:${repositoryID}`} repositoryID={repositoryID}><ConnectedWorkspace shared={shared}/></ScopedSessionProvider> : <Shell {...shared}><Access/></Shell>}
    {settings && <Settings key={repositoryID} preferences={preferences} save={setPreferences} close={()=>setSettings(false)}/>}</>;
}

function ConnectedWorkspace({shared}) {
  const project = useProject();
  const selection = useSnapshot(project.data);
  const value = {...shared,project:project.data,snapshot:selection.snapshot,refreshProject:project.refresh,scanQueued:project.scanQueued,scanning:project.scanning};
  return <WorkspaceContext.Provider value={value}><AskAIProvider><Shell {...shared} project={project.data} snapshot={selection.snapshot}>
    <ErrorNotice error={project.error || selection.error}/>
    {project.data && <section id="workspace">{project.data.repository?.id && <ProjectHeading/>}
      {selection.snapshot ? <Outlet/> : <WorkspaceWaiting project={project.data}/>}
      <footer className="workspace-footer"><span>Facts from source, not guesses.</span><span>Snapshot-pinned browsing</span></footer>
    </section>}
  </Shell></AskAIProvider></WorkspaceContext.Provider>;
}

function WorkspaceWaiting({project}) {
  if (!project.repository?.id) return <EmptyState title="Add your first workspace">Use Add workspace above to select a Git repository. Saved workspaces reopen after refresh and server restarts.</EmptyState>;
  return <EmptyState title="Waiting for a source snapshot">{project.last_error || 'The tracker captures and parses the checkout in the background.'}</EmptyState>;
}

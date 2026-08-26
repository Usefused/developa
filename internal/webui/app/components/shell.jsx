import {Link,NavLink} from 'react-router';
import {navigation,pageURL} from '../lib/routes.js';
import {count} from '../../assets/model.js';
import {Button} from './common.jsx';
import {useSession} from '../hooks/use-session.jsx';
import {WorkspaceSwitcher} from './workspace-switcher.jsx';

export function Shell({project,snapshot,repositoryID,preferences,settings,toggleTheme,children}) {
  const {status,lock} = useSession();
  return <><header className="topbar">
    <Link to={pageURL('blocks',snapshot?.id,{repo:repositoryID})} className="brand" aria-label="Developa home"><span className="brand-mark" aria-hidden="true"><i/><i/><i/><i/></span>developa<span className="brand-dot">.</span></Link>
    <div className={`project-crumb${status === 'ready' ? ' workspace-crumb' : ''}`}><span className="crumb-slash">/</span>{status === 'ready' ? <WorkspaceSwitcher repositoryID={repositoryID} repository={project?.repository}/> : <span>Your code, in context</span>}</div>
    <div className="header-actions"><span className="language-label">Go workspace</span><Button className="quiet-button" onClick={toggleTheme} aria-label={`Switch to ${preferences.theme === 'light' ? 'dark' : 'light'} theme`}>◐</Button><Button className="quiet-button" onClick={settings}>Editor settings</Button></div>
  </header><div className="app-layout"><nav className="navigation" aria-label="Workspace navigation">
    <p className="nav-eyebrow">WORKSPACE</p>
    {navigation.map(([page,label,icon])=><NavLink key={page} to={pageURL(page,snapshot?.id,{repo:repositoryID,saved:page === 'features' ? 1 : null})} className={({isActive})=>`nav-item${isActive ? ' selected' : ''}`}>
      <span className="nav-icon" aria-hidden="true">{icon}</span>{label}{page === 'blocks' && <span className="nav-count">{count(snapshot?.file_count)}</span>}{page === 'features' && <span className="planned-label">INFERRED</span>}
    </NavLink>)}
    <div className="nav-bottom"><div className="connection"><span className={`status-dot ${project ? 'live' : ''}`}/><span>{project ? 'Watching repository' : status}</span></div>
      <p>A private view of your repository.</p>{status === 'ready' && <Button className="text-button" onClick={lock}>Lock workspace</Button>}<span className="build-label">GO CODE INDEX · v0.3</span>
    </div></nav><main id="main">{children}</main></div></>;
}

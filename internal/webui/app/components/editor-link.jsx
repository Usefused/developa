import {useState} from 'react';
import {editorHref,editorLocation} from '../../assets/model.js';
import {useWorkspace} from '../hooks/use-workspace.jsx';
import {Button} from './common.jsx';

export function EditorLink({path,position,className='text-button',children}) {
  const {preferences,settings} = useWorkspace();
  const args = [preferences.root,path,position.line,position.column,preferences.editor];
  const href = editorHref(...args);
  if (!href) return null;
  const name = preferences.editor === 'cursor' ? 'Cursor' : 'VS Code';
  return <EditorLaunch key={href} href={href} location={editorLocation(...args)} name={name} settings={settings} className={className}>{children || `Open in ${name} ↗`}</EditorLaunch>;
}

function EditorLaunch({href,location,name,settings,className,children}) {
  const [attempted,setAttempted] = useState(false);
  const [status,setStatus] = useState('');
  async function copy() {
    try { await navigator.clipboard.writeText(location); setStatus('Local editor location copied.'); }
    catch { setStatus('Clipboard unavailable. Select and copy the location above.'); }
  }
  // Browsers cannot confirm an external app opened. Keep the native user gesture
  // and offer a manual fallback instead of claiming success or retrying launches.
  return <div className="editor-launch"><a href={href} className={className} onClick={()=>setAttempted(true)}>{children}</a>
    <details className="editor-help" open={attempted || undefined}>
      <summary>Editor didn’t open?</summary>
      <p>If {name} did not open, check whether your browser allows external-app links and that {name} is installed.</p>
      <p>You can also open the file manually in your editor. This location uses your configured local checkout, not the server’s filesystem.</p>
      <input aria-label="Local editor location" readOnly value={location}/>
      <div className="editor-help-actions"><Button onClick={copy}>Copy local location</Button><Button onClick={settings}>Editor settings</Button></div>
      <p role="status">{status}</p>
    </details>
  </div>;
}

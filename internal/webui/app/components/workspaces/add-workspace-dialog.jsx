import {useEffect,useRef,useState} from 'react';
import {useSession} from '../../hooks/use-session.jsx';
import {useAction} from '../../hooks/use-action.js';
import {Button,ErrorNotice} from '../common.jsx';
import {FolderPicker} from './folder-picker.jsx';

export function AddWorkspaceDialog({close,added}) {
  const {rootApi,api,cache} = useSession();
  const client = rootApi || api;
  const dialog = useRef(null);
  const [selection,setSelection] = useState(null);
  const [name,setName] = useState('');
  const action = useAction();
  useEffect(()=>{dialog.current.showModal();},[]);
  async function submit(event) {
    event.preventDefault();
    if (!selection) return;
    await action.run(async signal=>{
      const result = await client.addWorkspace({...selection,name:name.trim()},signal);
      if (!signal.aborted) { cache.clear();added(result.id); }
      return result;
    });
  }
  return <dialog ref={dialog} className="add-workspace-dialog" onCancel={close} aria-labelledby="add-workspace-title"><form onSubmit={submit}>
    <div className="dialog-heading"><h2 id="add-workspace-title">Add workspace</h2><Button className="quiet-button" onClick={close} aria-label="Close add workspace">×</Button></div>
    <p>Select a Git repository from the folders allowed when Denverr started.</p>
    <fieldset disabled={action.pending} className="workspace-fields">
      <FolderPicker api={client} onSelect={setSelection}/>
      <label htmlFor="workspace-name">Workspace name <span className="muted">(optional)</span></label>
      <input id="workspace-name" value={name} maxLength={200} onChange={event=>setName(event.target.value)} placeholder="Use the folder name"/>
    </fieldset>
    <ErrorNotice error={action.error}/>
    <p className="form-note">Saved in PostgreSQL and monitored in the background. Git must already be initialized; adding a workspace does not modify your files.</p>
    <div className="workspace-dialog-actions"><Button onClick={close}>Cancel</Button><button className="primary-button" type="submit" disabled={!selection || action.pending}>{action.pending ? 'Adding workspace…' : 'Add selected folder'}</button></div>
  </form></dialog>;
}

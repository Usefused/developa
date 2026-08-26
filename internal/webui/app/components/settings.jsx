import {useEffect,useRef,useState} from 'react';
import {editorHref} from '../../assets/model.js';
import {Button} from './common.jsx';
import {SearchSelect} from './search-select.jsx';

const editors = [{value:'vscode',label:'Visual Studio Code'},{value:'cursor',label:'Cursor'}];

export function Settings({preferences,save,close}) {
  const dialog = useRef(null);
  const [root,setRoot] = useState(preferences.root);
  const [editor,setEditor] = useState(preferences.editor);
  useEffect(()=>{ dialog.current.showModal(); },[]);
  function submit(event) {
    event.preventDefault();
    const input = event.currentTarget.elements.root;
    const valid = !root.trim() || editorHref(root,'example.go',1,1,editor);
    input.setCustomValidity(valid ? '' : 'Enter an absolute local checkout path.');
    if (!valid) return input.reportValidity();
    save({...preferences,root:root.trim(),editor});
    close();
  }
  return <dialog ref={dialog} onCancel={close} aria-labelledby="settings-title"><form onSubmit={submit}>
    <div className="dialog-heading"><h2 id="settings-title">Open code in your editor</h2><Button className="quiet-button" onClick={close} aria-label="Close settings">×</Button></div>
    <p>Map this workspace’s repository-relative paths to the checkout on your computer.</p>
    <label htmlFor="editor-root">Your local checkout root</label><input id="editor-root" name="root" value={root} onChange={event=>setRoot(event.target.value)} placeholder="/Users/you/projects/my-repo"/>
    <label>Editor</label><SearchSelect label="Editor" options={editors} selected={editors.find(item=>item.value === editor)} onChange={setEditor} placeholder="Choose your editor…"/>
    <p className="form-note">Editor paths are saved separately for each workspace in this browser. Newer local edits can shift the saved snapshot’s line numbers.</p>
    <button className="primary-button" type="submit">Save settings</button>
  </form></dialog>;
}

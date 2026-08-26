import {useEffect,useState} from 'react';
import {useResource} from '../../hooks/use-resource.js';
import {Button,ErrorNotice} from '../common.jsx';
import {SearchSelect} from '../search-select.jsx';

export function FolderPicker({api,onSelect}) {
  const roots = useResource('workspace-roots',signal=>api.workspaceRoots(signal));
  const [rootID,setRootID] = useState('');
  const [path,setPath] = useState('.');
  const options = (roots.data || []).map(root=>({value:root.id,label:root.path}));
  const selected = options.find(root=>root.value === rootID) || options[0];
  const id = selected?.value;
  useEffect(()=>{onSelect(id ? {root_id:id,path} : null);},[id,path,onSelect]);
  function selectRoot(value) { setRootID(value);setPath('.'); }
  return <div className="folder-picker"><ErrorNotice error={roots.error}/>
    <label>Filesystem location</label><SearchSelect label="Filesystem location" options={options} selected={selected} onChange={selectRoot} placeholder="Search allowed folders…"/>
    {id ? <FolderContents key={`${id}:${path}`} api={api} rootID={id} path={path} navigate={setPath}/> : <p className="form-note">{roots.pending ? 'Loading allowed folders…' : 'No folders are available. Configure WORKSPACE_ROOTS on the server to enable folder selection.'}</p>}
  </div>;
}

function FolderContents({api,rootID,path,navigate}) {
  const [offset,setOffset] = useState(0);
  const page = useResource(`workspace-folders:${rootID}:${path}:${offset}`,signal=>api.workspaceFolders({root_id:rootID,path,offset},signal));
  const parent = path.split('/').slice(0,-1).join('/') || '.';
  return <section className="folder-contents" aria-label="Folder browser">
    <div className="folder-toolbar"><Button disabled={path === '.'} onClick={()=>navigate(parent)}>↑ Parent folder</Button><Button onClick={page.refresh}>Refresh folders</Button></div>
    <p className="selected-folder">Selected folder: <code>{path === '.' ? 'Filesystem location root' : path}</code></p>
    <ErrorNotice error={page.error}/>
    <div className="folder-list" aria-busy={page.pending}>
      {page.pending ? <p>Loading folders…</p> : <FolderItems items={page.data?.items} navigate={navigate}/>}
    </div>
    <div className="folder-toolbar"><Button disabled={!offset || page.pending} onClick={()=>setOffset(Math.max(0,offset-100))}>Previous folders</Button><Button disabled={!Number.isInteger(page.data?.next_offset) || page.pending} onClick={()=>setOffset(page.data.next_offset)}>More folders</Button></div>
  </section>;
}

function FolderItems({items = [],navigate}) {
  if (!items.length) return <p>No subfolders on this page. You can select the current folder.</p>;
  return items.map(folder=><Button key={folder.path} className="folder-option" onClick={()=>navigate(folder.path)}><span aria-hidden="true">▱</span><span>{folder.name}</span><span aria-hidden="true">→</span></Button>);
}

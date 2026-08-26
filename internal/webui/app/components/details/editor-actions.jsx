import {useState} from 'react';
import {editorHref} from '../../../assets/model.js';
import {useWorkspace} from '../../hooks/use-workspace.jsx';
import {Button,Section} from '../common.jsx';
import {EditorLink} from '../editor-link.jsx';

export function EditorActions({item}) {
  const {preferences,settings} = useWorkspace();
  const [status,setStatus] = useState('');
  const position = item.symbol.span.start;
  const href = editorHref(preferences.root,item.path,position.line,position.column,preferences.editor);
  async function copy() {
    try { await navigator.clipboard.writeText(`${item.path}:${position.line}:${position.column}`); setStatus('Source location copied.'); }
    catch { setStatus('Clipboard unavailable. Select the location above to copy it.'); }
  }
  return <Section title="Source location" className="source-location-section"><div className="detail-actions">{href ? <EditorLink path={item.path} position={position} className="primary-button"/> : <Button onClick={settings}>Set up editor ↗</Button>}<Button onClick={copy}>Copy location</Button></div>{status && <p role="status">{status}</p>}</Section>;
}

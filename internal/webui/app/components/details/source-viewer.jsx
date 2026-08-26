import {Fragment,useEffect,useMemo,useRef,useState} from 'react';
import {goSourceLines} from '../../../code-source/go.js';
import {Button} from '../common.jsx';

export function SourceViewer({symbol,path,expanded=false}) {
  const lines = useMemo(()=>goSourceLines(symbol.source,symbol.span.start.line),[symbol]);
  const [wrapped,setWrapped] = useState(false);
  const [open,setOpen] = useState(false);
  const [status,setStatus] = useState('');
  async function copy() {
    try { await navigator.clipboard.writeText(symbol.source); setStatus('Captured source copied.'); }
    catch { setStatus('Clipboard unavailable. Select the code to copy it.'); }
  }
  return <section className={`source-viewer${wrapped ? ' source-wrapped' : ''}`}><div className="source-toolbar"><div className="source-language"><strong>Go</strong><span>Read only</span></div>
    <div className="source-controls"><Button className="quiet-button" aria-label="Wrap code lines" aria-pressed={wrapped} onClick={()=>setWrapped(!wrapped)}>Wrap</Button><Button className="quiet-button" aria-label="Copy captured source" onClick={copy}>Copy</Button>{!expanded && <Button className="quiet-button" onClick={()=>setOpen(true)}>Expand</Button>}</div></div>
    <pre className="source-viewport" tabIndex={0} role="region" aria-label={`Captured Go source for ${symbol.name}`}><code className="source-code language-go">{lines.map((line,index)=><Fragment key={line.number}>{index > 0 && '\n'}<SourceLine line={line}/></Fragment>)}</code></pre>
    <p className={`source-caption${symbol.source_truncated ? ' source-truncated' : ''}`}>Lines {lines[0].number}–{lines.at(-1).number} · starts at column {symbol.span.start.column}{symbol.source_truncated && <span>Partial excerpt — truncated at the index limit.</span>}</p>
    <p className="source-status" role="status">{status}</p>{open && <SourceDialog symbol={symbol} path={path} close={()=>setOpen(false)}/>}
  </section>;
}

function SourceLine({line}) {
  return <span className="source-line"><span className="source-line-number" data-line={line.number} aria-hidden="true"/><span className="source-line-text">{line.tokens.map((token,index)=><span key={index} className={token.className}>{token.text}</span>)}</span></span>;
}

function SourceDialog({symbol,path,close}) {
  const dialog = useRef(null);
  useEffect(()=>dialog.current.showModal(),[]);
  return <dialog ref={dialog} className="source-dialog" onCancel={close} aria-label={`Captured implementation of ${symbol.name}`}><div className="source-dialog-heading"><h2>{symbol.name}</h2><Button onClick={close}>Close</Button></div><p className="source-dialog-path">{path}</p><SourceViewer symbol={symbol} path={path} expanded/></dialog>;
}

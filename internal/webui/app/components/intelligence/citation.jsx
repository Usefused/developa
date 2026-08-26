import {usePageNavigation} from '../../hooks/use-workspace.jsx';
import {Button} from '../common.jsx';
import {EditorLink} from '../editor-link.jsx';

export function Citation({citation}) {
  const {openSymbol} = usePageNavigation();
  const {line,column} = citation.span.start;
  return <div className="citation-card"><Button className="text-button" onClick={()=>openSymbol(citation.symbol_id)}>{citation.name}</Button><span className="mono">{citation.path}:{line}:{column}</span><EditorLink path={citation.path} position={citation.span.start}>Open source ↗</EditorLink></div>;
}

import {kindGroup,kindLabel} from '../../../assets/model.js';
import {sourceSummary} from '../../../assets/documentation.js';
import {Button} from '../common.jsx';

export function SymbolRow({item,onOpen,selected}) {
  const {symbol} = item;
  return <Button className={`symbol-row${selected ? ' selected' : ''}`} onClick={()=>onOpen(symbol.id)} aria-label={`Inspect ${symbol.name}, ${kindLabel(symbol.kind)}`}>
    <span className={`symbol-glyph ${kindGroup(symbol.kind)}`}>{kindGroup(symbol.kind) === 'function' ? 'ƒ' : '{}'}</span>
    <div className="symbol-row-label"><span className="symbol-name">{symbol.name}</span><span className="symbol-signature">{symbol.signature}</span>{sourceSummary(symbol) && <span className="symbol-summary">{sourceSummary(symbol)}</span>}</div>
    <div className="symbol-row-meta"><span>{kindLabel(symbol.kind)}</span><span className="mono">L{symbol.span.start.line}</span></div>
  </Button>;
}

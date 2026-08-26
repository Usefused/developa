import {baseName,count} from '../../../assets/model.js';
import {EmptyState} from '../common.jsx';
import {SymbolRow} from './symbol-row.jsx';

export function FileContents({file,page,onOpen,selected}) {
  return <><div className="file-heading"><span className="section-kicker">PACKAGE {file.package}</span><h2 className="mono">{baseName(file.path)}</h2><p className="file-doc">{file.doc}</p><p className="file-overview">{file.overview}</p></div>
    {!!file.imports?.length && <details className="imports-list"><summary>{file.imports.length} imports</summary><ul>{file.imports.map((item,index)=><li key={index} className="mono">{item.alias} {item.path}</li>)}</ul></details>}
    <p className="section-description">{count(page.total)} declarations</p><div className="symbol-list">{page.items.map(item=><SymbolRow key={item.symbol.id} item={item} selected={selected === item.symbol.id} onOpen={onOpen}/>)}</div>
    {!page.items.length && <EmptyState title="No matching declarations">Try another name or kind.</EmptyState>}
  </>;
}

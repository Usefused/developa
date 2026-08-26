import {useEffect,useRef} from 'react';
import {usePageNavigation} from '../hooks/use-workspace.jsx';
import {SymbolPanel} from './details/symbol-panel.jsx';

export function ExplorerFrame({children,evidence}) {
  const {params,update} = usePageNavigation();
  const symbol = params.get('symbol');
  const panel = useRef(null);
  const visible = !!symbol || !!evidence;
  useEffect(()=>{
    if (symbol && window.innerWidth <= 1250) panel.current?.scrollIntoView({block:'start',behavior:'smooth'});
  },[symbol]);
  useEffect(()=>{
    const close = event=>{
      if (event.key !== 'Escape' || document.querySelector('dialog[open]')) return;
      update({symbol:null});
    };
    document.addEventListener('keydown',close);
    return ()=>document.removeEventListener('keydown',close);
  },[update]);
  return <div className={`explorer${visible ? ' with-detail' : ''}`}><section className="explorer-main route-content">{children}</section>
    {visible && <aside ref={panel} className="detail-panel" tabIndex={0} aria-label={symbol ? 'Symbol details' : 'Feature evidence'}>{symbol ? <SymbolPanel key={symbol} id={symbol}/> : evidence}</aside>}
  </div>;
}

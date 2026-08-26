import {createElement as h,useEffect,useRef} from 'react';
import {SearchSelect as Control} from '../assets/search-select.js';

// React owns the lifetime of the shared, tested accessible combobox. Pages and
// flow controls share its keyboard handling and bounded remote pagination.
export function SearchSelect({label,options,selected,onChange,placeholder,loadPage}) {
  const host = useRef(null);
  const control = useRef(null);
  const changed = useRef(onChange);
  changed.current = onChange;
  useEffect(()=>{
    const instance = new Control({label,options,placeholder,loadPage,onChange:value=>changed.current(value)});
    control.current = instance;
    host.current.append(instance.element);
    return ()=>instance.destroy();
  },[label,options,placeholder,loadPage]);
  useEffect(()=>control.current?.setSelected(selected),[selected,label,options,placeholder,loadPage]);
  return h('div',{ref:host});
}

export const FlowSelect = SearchSelect;

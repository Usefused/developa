import {useEffect,useState} from 'react';
import {kindLabel} from '../../../assets/model.js';
import {SearchSelect} from '../search-select.jsx';

const kinds = [{value:'',label:'All symbols'},...['function','method','struct','interface','named_type','alias','constant','variable','field','closure'].map(value=>({value,label:kindLabel(value)}))];

export function FilterBar({query,kind,onChange}) {
  const [text,setText] = useState(query);
  useEffect(()=>setText(query),[query]);
  useEffect(()=>{
    if (text === query) return;
    const timer = setTimeout(()=>onChange({q:text,offset:0}),280);
    return ()=>clearTimeout(timer);
  },[text,query,onChange]);
  return <form className="search-bar" role="search" onSubmit={event=>{event.preventDefault();onChange({q:text,offset:0});}}>
    <label className="search-field"><span aria-hidden="true">⌕</span><input type="search" aria-label="Search files or symbols" value={text} maxLength={200} onChange={event=>setText(event.target.value)} placeholder="Search files or symbols…"/></label>
    <SearchSelect label="Filter by symbol kind" options={kinds} selected={kinds.find(item=>item.value === kind)} onChange={value=>onChange({kind:value,offset:0})}/>
    <button type="submit" className="search-submit">Search</button>
  </form>;
}

import {baseName,directory,roomGroups,count} from '../../../assets/model.js';
import {Button} from '../common.jsx';

export function FileCard({file,onOpen}) {
  const groups = roomGroups(file.kinds);
  const labels = {function:'callables',type:'types',value:'fields & values'};
  const summary = Object.entries(groups).filter(([,amount])=>amount > 0).map(([kind,amount])=>`${amount} ${labels[kind]}`).join(' · ');
  return <Button className="file-block" onClick={()=>onOpen(file.path)} aria-label={`Open ${file.path}, ${file.symbol_count} symbols`}>
    <div className="block-top"><span className="go-mark">go</span><span className="block-package">{file.package}</span></div>
    <span className="file-name">{baseName(file.path)}</span><span className="file-directory">{directory(file.path)}</span><Rooms groups={groups}/>
    <span className="block-summary">{summary || 'No declarations'}</span><div className="block-footer"><span>{count(file.symbol_count)} declarations</span>
      {file.completeness === 'partial' && <span className="partial-badge">Partial syntax</span>}<span className="arrow">↗</span></div>
  </Button>;
}

function Rooms({groups}) {
  const rooms = Object.entries(groups).flatMap(([kind,amount])=>Array.from({length:Math.min(amount,3)},()=>kind)).slice(0,8);
  const extra = Object.values(groups).reduce((sum,value)=>sum+value,0)-rooms.length;
  return <div className="block-rooms" aria-hidden="true">{rooms.map((kind,index)=><span key={index} className={`room ${kind}`}>{{function:'f',type:'{}',value:'·'}[kind]}</span>)}{extra > 0 && <span className="room-extra">+{extra}</span>}</div>;
}

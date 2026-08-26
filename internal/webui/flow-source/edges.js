import {createElement as h} from 'react';
import {BaseEdge} from '@xyflow/react';

// Dagre allocates a route and label position for each connection. Using them
// prevents parallel edges from sharing React Flow's default middle segment.
export function callPath(points, source, target) {
  const route = [source,{x:source.x,y:source.y+18},...points.slice(1,-1),{x:target.x,y:target.y-18},target];
  let path = `M ${source.x} ${source.y}`;
  for (let index=1; index<route.length-1; index++) {
    const here = route[index];
    const next = route[index+1];
    path += ` Q ${here.x} ${here.y} ${(here.x+next.x)/2} ${(here.y+next.y)/2}`;
  }
  return `${path} L ${target.x} ${target.y}`;
}

export function SourceCallEdge({id,sourceX,sourceY,targetX,targetY,markerEnd,data,label,style}) {
  const path = callPath(data.route,{x:sourceX,y:sourceY},{x:targetX,y:targetY});
  return h(BaseEdge,{id,path,markerEnd,style,label,labelX:data.labelPosition.x,labelY:data.labelPosition.y,
    labelShowBg:true,labelBgPadding:[8,5],labelBgBorderRadius:6,interactionWidth:20});
}

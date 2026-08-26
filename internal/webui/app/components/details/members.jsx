import {parameterText} from '../../../assets/model.js';
import {reviewable} from '../../../assets/reviews.js';
import {Button,Section} from '../common.jsx';

export function Members({symbol,review,open}) {
  return <><Parameters title="Type parameters" values={symbol.type_parameters}/><Parameters title="Parameters" values={symbol.parameters} notes={review?.parameters} empty={reviewable(symbol) ? 'No parameters.' : ''}/><Parameters title="Returns" values={symbol.results} empty={reviewable(symbol) ? 'No return values.' : ''}/>
    {symbol.receiver && <Section title="Receiver"><p className="mono">{symbol.receiver}</p></Section>}
    {!!symbol.fields?.length && <Section title="Fields & embedded types"><ul className="parameter-list">{symbol.fields.map((field,index)=><li key={index}>{field.embedded ? 'embedded' : field.name} {field.type} {field.tag_literal}</li>)}</ul></Section>}
    {!!symbol.values?.length && <Section title="Initializer expressions"><p className="mono">{symbol.values.join('\n')}</p></Section>}
    {symbol.parent_id && <Section title="Contained in"><Button className="text-button" onClick={()=>open(symbol.parent_id)}>Open parent declaration →</Button></Section>}
  </>;
}

function Parameters({title,values=[],notes=[],empty=''}) {
  const descriptions = new Map(notes.map(note=>[note.position,note.description]));
  if (!values.length) return empty ? <Section title={title}><p>{empty}</p></Section> : null;
  return <Section title={title}><ul className="parameter-list">{values.map((parameter,index)=><li key={index}>{parameterText(parameter)}{descriptions.has(parameter.position) && <span className="parameter-review">AI · {descriptions.get(parameter.position)}</span>}</li>)}</ul></Section>;
}

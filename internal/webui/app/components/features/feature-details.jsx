import {usePageNavigation} from '../../hooks/use-workspace.jsx';
import {Button,Section} from '../common.jsx';
import {AskAIButton} from '../intelligence/ask-ai-button.jsx';
import {Citation} from '../intelligence/citation.jsx';

export function FeatureDetails({feature}) {
  const {update} = usePageNavigation();
  return <><div className="detail-top"><span className="section-kicker">INFERRED CAPABILITY</span><Button className="quiet-button" aria-label="Close feature details" onClick={()=>update({hideEvidence:1})}>×</Button></div>
    <h2 className="detail-name">{feature.title}</h2><p className="feature-summary">{feature.summary}</p><AskAIButton target={{type:'feature',id:feature.id,title:feature.title}}/>
    <p className="muted-note">Citations identify the source used to infer this feature. They do not independently verify the description.</p><Section title="Source evidence">{feature.evidence.map((citation,index)=><Citation key={index} citation={citation}/>)}</Section>
  </>;
}

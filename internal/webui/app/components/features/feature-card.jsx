import {Button} from '../common.jsx';

export function FeatureCard({feature,onOpen}) {
  return <Button className="feature-card" onClick={()=>onOpen(feature.id)} aria-label={`Inspect feature: ${feature.title}`}><span className="detail-tag">INFERRED</span><h3>{feature.title}</h3><p>{feature.summary}</p><span className="feature-evidence-count">{feature.evidence.length} source references ↗</span></Button>;
}

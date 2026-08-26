import {explanationRequest} from '../../../assets/explanation.js';
import {useSession} from '../../hooks/use-session.jsx';
import {useWorkspace} from '../../hooks/use-workspace.jsx';
import {useCapabilities} from '../../hooks/use-capabilities.js';
import {useAction} from '../../hooks/use-action.js';
import {useResource} from '../../hooks/use-resource.js';
import {Button,ErrorNotice,Section} from '../common.jsx';
import {Citation} from './citation.jsx';

export function Explanation({target}) {
  const {snapshot} = useWorkspace();
  return <ExplanationAction key={`${snapshot.id}:${JSON.stringify(target)}`} target={target}/>;
}

function ExplanationAction({target}) {
  const {api,cache} = useSession();
  const {snapshot} = useWorkspace();
  const capabilities = useCapabilities();
  const action = useAction();
  const flow = target.type === 'flow';
  const request = explanationRequest(target);
  const key = `answer:${snapshot.id}:${JSON.stringify(request)}`;
  const saved = useResource(key,signal=>api.savedAnswer(snapshot.id,request,signal));
  const answer = action.data || saved.data?.answer;
  function explain() {
    action.run(async signal=>{
      const result = await api.answerStream(snapshot.id,request,signal);
      if (!signal.aborted) cache.set(key,{answer:result});
      return result;
    });
  }
  return <ExplanationView {...{flow,action,saved,capabilities,answer,explain}}/>;
}

function ExplanationView({flow,action,saved,capabilities,answer,explain}) {
  return <Section title={flow ? 'AI flow explanation' : 'AI explanation'} className="ai-action-section" aria-label={flow ? 'AI flow explanation' : 'AI explanation'}>
    <Button disabled={action.pending || saved.pending || !!answer || !capabilities.data?.answers} onClick={explain} aria-busy={action.pending}>{explanationLabel(action.pending,flow,answer)}</Button>
    <ErrorNotice error={action.error}/>{answer && <Answer answer={answer}/>}</Section>;
}

function explanationLabel(pending, flow, answer) {
  if (answer) return 'Explanation saved';
  if (pending) return 'Explaining…';
  return flow ? 'Explain this flow' : 'Explain function behavior';
}

function Answer({answer}) {
  return <div className="explanation-result" role="status"><span className="detail-tag">{answer.cached ? 'REUSED' : 'SAVED'} · INFERRED</span><p className="feature-summary">{answer.text}</p><p className="muted-note">{answer.model}</p>
    {answer.context_truncated && <p className="muted-note">Context is incomplete: some source or related declarations were omitted.</p>}
    {(answer.limitations || []).map((text,index)=><p className="muted-note" key={index}>{text}</p>)}{answer.evidence.map((citation,index)=><Citation key={index} citation={citation}/>)}</div>;
}

import {Button} from '../common.jsx';
import {useAskAI} from './ask-ai.jsx';

export function AskAIButton({target}) {
  const chat = useAskAI();
  if (!chat) return null;
  return <Button className="ask-ai-button" onClick={()=>chat.open(target)}>{label(target.type)}</Button>;
}

function label(type) {
  if (type === 'feature') return 'Ask AI about this feature';
  if (type === 'flow') return 'Ask AI about this flow';
  return 'Ask AI about this function';
}

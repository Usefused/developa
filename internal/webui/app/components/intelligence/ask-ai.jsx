import {createContext,useCallback,useContext,useEffect,useMemo,useRef,useState} from 'react';
import {useSession} from '../../hooks/use-session.jsx';
import {useWorkspace} from '../../hooks/use-workspace.jsx';
import {Button} from '../common.jsx';
import {Citation} from './citation.jsx';

const AskAIContext = createContext(null);
export const useAskAI = ()=>useContext(AskAIContext);

export function AskAIProvider({children}) {
  const {api} = useSession();
  const {snapshot} = useWorkspace();
  const [chats,setChats] = useState([]);
  const chatsRef = useRef(chats);
  const requests = useRef(new Map());
	const lookups = useRef(new Map());
  chatsRef.current = chats;
  useEffect(()=>()=>{abortAll(requests.current);abortAll(lookups.current);},[]);
  const open = useCallback(target=>{
	const id = targetKey(target);
	const exists = chatsRef.current.some(chat=>chat.id === id);
	setChats(items=>openTarget(items,target));
	if (!exists) restoreSaved({id,target,api,snapshot,setChats,lookups});
  },[api,snapshot]);
  const close = useCallback(id=>{abortOne(requests.current,id);abortOne(lookups.current,id);setChats(items=>items.filter(chat=>chat.id !== id));},[]);
  const minimize = useCallback(id=>setChats(items=>items.map(chat=>chat.id === id ? {...chat,minimized:!chat.minimized} : chat)),[]);
  const ask = useCallback((id,question)=>sendQuestion({id,question,api,snapshot,setChats,chatsRef,requests}),[api,snapshot]);
  const value = useMemo(()=>({open}),[open]);
  return <AskAIContext.Provider value={value}>{children}<AskAIDock chats={chats} ask={ask} close={close} minimize={minimize}/></AskAIContext.Provider>;
}

function abortAll(records) {
  for (const controller of records.values()) controller.abort();
}

function abortOne(records,id) {
  records.get(id)?.abort();
  records.delete(id);
}

async function restoreSaved({id,target,api,snapshot,setChats,lookups}) {
  const controller = new AbortController();
  lookups.current.set(id,controller);
  const question = defaultQuestion(target);
  try {
    const result = await api.savedAnswer(snapshot.id,answerRequest(target,question),controller.signal);
    if (!controller.signal.aborted && result.answer) restoreSavedMessages(setChats,id,question,result.answer);
  } catch {
	// A lookup is optional and read-only; surface errors only after the user asks.
  } finally {
    if (lookups.current.get(id) === controller) lookups.current.delete(id);
  }
}

function restoreSavedMessages(setChats,id,question,answer) {
  setChats(items=>items.map(chat=>chat.id === id && chat.messages.length === 0 ? {...chat,messages:[{role:'user',text:question},{role:'assistant',text:answer.text,answer}]} : chat));
}

function openTarget(items,target) {
  const id = targetKey(target);
  const current = items.find(chat=>chat.id === id);
  if (current) return [...items.filter(chat=>chat.id !== id),{...current,minimized:false}];
  return [...items,{id,target,title:target.title || targetLabel(target),messages:[],pending:false,error:'',minimized:false}];
}

async function sendQuestion({id,question,api,snapshot,setChats,chatsRef,requests}) {
  const value = question.trim();
  const chat = chatsRef.current.find(item=>item.id === id);
  if (!chat || !value || chat.pending) return;
  const messages = [...chat.messages,{role:'user',text:value}];
  const controller = new AbortController();
  requests.current.set(id,controller);
  updateChat(setChats,id,{messages,pending:true,error:''});
  try {
    const request = answerRequest(chat.target,conversationQuestion(messages));
    const answer = await api.answerStream(snapshot.id,request,controller.signal);
    if (!controller.signal.aborted) updateChat(setChats,id,{messages:[...messages,{role:'assistant',text:answer.text,answer}],pending:false});
  } catch (error) {
    if (!controller.signal.aborted) updateChat(setChats,id,{pending:false,error:error.message || 'AI answer failed.'});
  } finally {
    if (requests.current.get(id) === controller) requests.current.delete(id);
  }
}

function updateChat(setChats,id,values) {
  setChats(items=>items.map(chat=>chat.id === id ? {...chat,...values} : chat));
}

export function answerRequest(target,question) {
  if (target.type === 'feature') return {question,feature_id:target.id};
  if (target.type === 'flow') return {question,flow:target.options};
  return {question,symbol_id:target.id};
}

// Follow-up context is intentionally small because the source evidence must
// remain the dominant input to the model, not an ever-growing model transcript.
export function conversationQuestion(messages) {
  const recent = messages.slice(-5);
  if (recent.length === 1) return clipUTF8(recent[0].text,2000,false);
  const transcript = recent.map(message=>`${message.role === 'assistant' ? 'Previous answer' : 'User'}: ${message.text}`).join('\n\n');
  return clipUTF8(transcript,2000,true);
}

function clipUTF8(value,limit,fromEnd) {
  const characters = [...value];
  if (fromEnd) characters.reverse();
  const selected = [];
  const encoder = new globalThis.TextEncoder();
  let size = 0;
  for (const character of characters) {
    const width = encoder.encode(character).length;
    if (size+width > limit) break;
    selected.push(character);
    size += width;
  }
  if (fromEnd) selected.reverse();
  return selected.join('');
}

function targetKey(target) {
  const scope = target.type === 'flow' ? JSON.stringify(target.options) : target.id;
  return `${target.type}:${scope}`;
}

function targetLabel(target) {
  if (target.type === 'feature') return 'Feature';
  if (target.type === 'flow') return 'Code flow';
  return 'Function';
}

function AskAIDock({chats,ask,close,minimize}) {
  if (!chats.length) return null;
  return <aside className="ask-ai-dock" aria-label="Open AI chats">{chats.map(chat=><AskAIWindow key={chat.id} chat={chat} ask={ask} close={close} minimize={minimize}/>)}</aside>;
}

function AskAIWindow({chat,ask,close,minimize}) {
  const [draft,setDraft] = useState('');
  function submit(event) {
    event.preventDefault();
    const value = draft.trim();
    if (!value) return;
    setDraft('');
    ask(chat.id,value);
  }
  return <section className={`ask-ai-window${chat.minimized ? ' minimized' : ''}`} aria-label={`Ask AI about ${chat.title}`}>
    <header><button type="button" onClick={()=>minimize(chat.id)} aria-label={chat.minimized ? 'Expand chat' : 'Minimize chat'}><span>Ask AI</span><strong>{chat.title}</strong></button><Button className="quiet-button" aria-label="Close chat" onClick={()=>close(chat.id)}>×</Button></header>
    {!chat.minimized && <><div className="ask-ai-messages">{!chat.messages.length && <Starter target={chat.target} ask={question=>ask(chat.id,question)}/>} {chat.messages.map((message,index)=><ChatMessage key={index} message={message}/>)}{chat.pending && <p className="ask-ai-thinking" role="status">Reviewing saved source context…</p>}{chat.error && <p className="inline-error" role="alert">{chat.error}</p>}</div>
      <form onSubmit={submit}><textarea aria-label={`Question about ${chat.title}`} value={draft} onChange={event=>setDraft(event.target.value)} maxLength={2000} placeholder="Ask about behavior, flow, inputs, or evidence…"/><Button className="primary-button" type="submit" disabled={chat.pending || !draft.trim()}>Send</Button></form></>}
  </section>;
}

function Starter({target,ask}) {
  return <div className="ask-ai-starter"><span className="detail-tag">{targetLabel(target).toUpperCase()} CONTEXT</span><p>What would you like to understand?</p><Button onClick={()=>ask(defaultQuestion(target))}>Explain this {target.type === 'symbol' ? 'function' : target.type}</Button></div>;
}

function defaultQuestion(target) {
  if (target.type === 'feature') return 'Explain how this feature works from its source evidence and static flow. Call out important boundaries or unresolved behavior.';
  if (target.type === 'flow') return 'Explain the important paths, entry evidence, shared dependencies, and unresolved gaps in this static flow.';
  return 'Explain what this function does, what its parameters mean, what it returns, and the important calls or side effects supported by the source.';
}

function ChatMessage({message}) {
  return <div className={`ask-ai-message ${message.role}`}><span>{message.role === 'assistant' ? 'Denverr' : 'You'}</span><p>{message.text}</p>{message.answer && <AnswerEvidence answer={message.answer}/>}</div>;
}

function AnswerEvidence({answer}) {
  return <details><summary>{answer.evidence.length} source references · {answer.cached ? 'reused' : 'saved'}</summary>{answer.evidence.map((citation,index)=><Citation key={index} citation={citation}/>)}{(answer.limitations || []).map((text,index)=><p className="muted-note" key={index}>{text}</p>)}</details>;
}

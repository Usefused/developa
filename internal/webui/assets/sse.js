const MAX_EVENT_BYTES = 256 * 1024;

export class StreamError extends Error {
  constructor() { super('The live response was incomplete or invalid. Retry the request when ready.'); this.name = 'StreamError'; }
}

// Keep only one bounded line and event. CRLF may straddle reads, and a UTF-8
// character may straddle network chunks; decoding and framing remain separate.
export class EventParser {
  constructor(deliver, limit = MAX_EVENT_BYTES) {
    Object.assign(this,{deliver,limit,line:'',lineBytes:0,skipLF:false,stopped:false,event:'',data:[],eventBytes:0});
  }

  feed(text) {
    for (const character of text) {
      if (this.stopped) return;
      this.character(character);
    }
  }

  character(character) {
    if (this.skipLF && character === '\n') { this.skipLF = false; return; }
    this.skipLF = false;
    if (character === '\r' || character === '\n') {
      this.acceptLine();
      this.line = '';
      this.lineBytes = 0;
      this.skipLF = character === '\r';
      return;
    }
    this.lineBytes += characterBytes(character);
    if (this.lineBytes > this.limit) throw new StreamError();
    this.line += character;
  }

  acceptLine() {
    if (!this.line) return this.dispatch();
    if (this.line.startsWith(':')) return;
    const colon = this.line.indexOf(':');
    const field = colon < 0 ? this.line : this.line.slice(0,colon);
    const value = colon < 0 ? '' : this.line.slice(colon+1).replace(/^ /,'');
    if (field === 'event') this.event = value;
    if (field === 'data') this.appendData(value);
  }

  appendData(value) {
    this.eventBytes += this.lineBytes + 1;
    if (this.eventBytes > this.limit) throw new StreamError();
    this.data.push(value);
  }

  dispatch() {
    if (this.data.length) this.stopped = this.deliver({event:this.event || 'message',data:this.data.join('\n')}) === false;
    this.event = '';
    this.data = [];
    this.eventBytes = 0;
  }
}

function characterBytes(character) {
  const code = character.codePointAt(0);
  if (code <= 0x7f) return 1;
  if (code <= 0x7ff) return 2;
  return code <= 0xffff ? 3 : 4;
}

export function parseEventData(event) {
  try { return JSON.parse(event.data); }
  catch { throw new StreamError(); }
}

function checkCanceled(signal) {
  if (!signal?.aborted) return;
  const error = new Error('Request canceled.');
  error.name = 'AbortError';
  throw error;
}

export async function consumeEvents(response, deliver, signal) {
  if (!response.body?.getReader) throw new StreamError();
  const reader = response.body.getReader();
  const cancel = ()=>reader.cancel().catch(()=>{});
  const parser = new EventParser(event=>{ checkCanceled(signal); return deliver(event); });
  signal?.addEventListener('abort',cancel,{once:true});
  try { await readEvents(reader,parser,signal); }
  finally {
    signal?.removeEventListener('abort',cancel);
    await cancel();
    reader.releaseLock();
  }
}

async function readEvents(reader, parser, signal) {
  const decoder = new globalThis.TextDecoder('utf-8',{fatal:true});
  while (!parser.stopped) {
    checkCanceled(signal);
    const {value,done} = await reader.read();
    checkCanceled(signal);
    if (done) { parser.feed(decode(decoder)); return; }
    feedBytes(parser,decoder,value);
  }
}

function feedBytes(parser, decoder, bytes) {
  // A large transport chunk must not create an unbounded intermediate string.
  for (let offset = 0; offset < bytes.length && !parser.stopped; offset += 16384) {
    parser.feed(decode(decoder,bytes.subarray(offset,offset+16384),true));
  }
}

function decode(decoder, bytes, stream = false) {
  try { return decoder.decode(bytes,{stream}); }
  catch { throw new StreamError(); }
}

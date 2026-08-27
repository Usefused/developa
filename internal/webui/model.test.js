import test from 'node:test';
import assert from 'node:assert/strict';
import {chainLevels} from './assets/chains.js';
import {generationLabel,jobActive} from './assets/intelligence.js';
import {editorHref,editorLocation,roomGroups,parameterText,query,pageLabel,projectRefreshInterval,snapshotPin} from './assets/model.js';
import {goSourceLines} from './code-source/go.js';
import {reviewRequest,reviewable,reviewRange} from './assets/reviews.js';
import {sourceSummary,documentationWarning} from './assets/documentation.js';

test('source summaries use compiled comments without consulting AI reviews',()=>{
  const symbol = {doc:'Declaration.',documentation:{summary:'Declaration.\n\nBody rationale.',origin:'indexed_source',truncated:false}};
  assert.equal(sourceSummary(symbol),'Declaration.\n\nBody rationale.');
  assert.equal(documentationWarning(symbol),'');
  assert.equal(sourceSummary({documentation:{summary:''},doc:'Outdated fallback'}),'');
  assert.equal(sourceSummary({doc:'Header',comment:'Trailing'}),'Header\n\nTrailing');
});

test('source summary only adds provenance copy when the capture is incomplete',()=>{
  const symbol = {documentation:{summary:'Captured.',origin:'captured_excerpt',truncated:true}};
  assert.match(documentationWarning(symbol),/Incomplete/);
  assert.equal(documentationWarning({documentation:{summary:'Complete.',origin:'captured_excerpt',truncated:false}}),'');
  assert.equal(documentationWarning({}),'');
});

test('review requests distinguish one function from a bounded direct-callee batch',()=>{
  assert.deepEqual(reviewRequest('f'),{symbol_id:'f',limit:4,offset:0});
  assert.deepEqual(reviewRequest('f',true,8),{callee_of:'f',limit:4,offset:8});
  assert.equal(reviewable({kind:'method'}),true);
  assert.equal(reviewable({kind:'interface_method'}),true);
  assert.equal(reviewable({kind:'struct'}),false);
});

test('review pagination labels the first and subsequent pages without implying analysis coverage',()=>{
  assert.equal(reviewRange({options:{offset:0},items:[{},{}],total:11}),'1–2 of 11');
  assert.equal(reviewRange({options:{offset:4},items:[{},{}],total:11}),'5–6 of 11');
  assert.equal(reviewRange({options:{},items:[{}],total:1}),'1–1 of 1');
});

test('captured Go highlights declarations, types, expressions, and literals',()=>{
  const lines = goSourceLines('func sum(value int) int { return value + 1 }',17);
  const tokens = new Map(lines[0].tokens.map(token=>[token.text,token.className]));
  assert.equal(tokens.get('func'),'go-keyword');
  assert.equal(tokens.get('sum'),'go-function');
  assert.equal(tokens.get('int'),'go-type');
  assert.equal(tokens.get('return'),'go-keyword');
  assert.equal(tokens.get('+'),'go-operator');
  assert.equal(tokens.get('1'),'go-literal');
});

test('Go comments and raw strings retain highlighting across physical lines',()=>{
  const source = 'func main() {\n\t/* first\nsecond */\n\tmessage := `one\ntwo`\n\tprintln(message)\n}';
  const lines = goSourceLines(source,42);
  assert.deepEqual(lines.map(line=>line.number),[42,43,44,45,46,47,48]);
  assert.equal(lines[1].tokens.at(-1).className,'go-comment');
  assert.equal(lines[2].tokens[0].className,'go-comment');
  assert.equal(lines[3].tokens.at(-1).className,'go-string');
  assert.equal(lines[4].tokens[0].className,'go-string');
  assert.equal(sourceText(lines),source);
});

test('highlighting preserves tabs, CRLF, Unicode, blank lines, and HTML-like source',()=>{
  const source = '\r\nfunc example() {\r\n\tvalue := "<script>海 & 😀</script>"\r\n\r\n\t_ = value\r\n}\r\n';
  const lines = goSourceLines(source,100);
  assert.equal(sourceText(lines),source);
  assert.equal(lines.length,7);
  assert.equal(lines.at(-1).number,106);
  assert.deepEqual(lines.at(-1).tokens,[]);
});

test('incomplete excerpts and nested declarations never discard captured text',()=>{
  const sources = ['', 'func broken() {\n\treturn "partial', 'Name string `json:"name"`', 'func(value int) { return value }', '/* unfinished\ncomment'];
  for (const source of sources) assert.equal(sourceText(goSourceLines(source,8)),source);
});

test('structs and interfaces use Go syntax without requiring a package header',()=>{
  const source = 'type Reader interface { Read([]byte) (int, error) }\ntype Value struct { Name string }';
  const lines = goSourceLines(source);
  const types = lines.flatMap(line=>line.tokens).filter(token=>token.className === 'go-type').map(token=>token.text);
  assert.ok(types.includes('Reader'));
  assert.ok(types.includes('Value'));
  assert.equal(sourceText(lines),source);
});

function sourceText(lines) {
  return lines.map(line=>line.tokens.map(token=>token.text).join('')).join('\n');
}

test('editor URLs encode filenames and retain exact physical positions',()=>{
  assert.equal(editorHref('/Users/a/My Project','internal/a #?.go',17,4,'vscode'),'vscode://file/Users/a/My%20Project/internal/a%20%23%3F.go:17:4');
  assert.equal(editorHref('C:\\code\\repo','main.go',1,1,'cursor'),'cursor://file/C%3A/code/repo/main.go:1:1');
});

test('manual editor locations preserve local paths and coordinates without URL escapes',()=>{
  assert.equal(editorLocation('/Users/a/My Project','internal/a #?.go',17,4,'vscode'),'/Users/a/My Project/internal/a #?.go:17:4');
  assert.equal(editorLocation('C:\\code\\repo','main.go',1,1,'cursor'),'C:/code/repo/main.go:1:1');
  assert.equal(editorLocation('/repo','../outside.go',1,1,'vscode'),'');
});

test('editor links reject traversal, injected schemes, and invalid coordinates',()=>{
  const cases = [
    ['javascript:alert(1)','main.go',1,1,'vscode'],
    ['/repo','../private.go',1,1,'vscode'],
    ['/repo','/private.go',1,1,'vscode'],
    ['/repo','folder\\private.go',1,1,'vscode'],
    ['/repo','main.go',0,1,'vscode'],
    ['/repo','main.go',1,1,'javascript'],
  ];
  for (const args of cases) assert.equal(editorHref(...args),'');
});

test('building rooms derive from actual kinds rather than decorative counts',()=>{
  assert.deepEqual(roomGroups({function:2,method:3,struct:1,field:4}),{function:5,type:1,value:4});
  assert.deepEqual(roomGroups(null),{function:0,type:0,value:0});
});

test('parameters preserve unnamed positions and variadic syntax',()=>{
  assert.equal(parameterText({name:'items',type:'string',variadic:true}),'items  ...string');
  assert.equal(parameterText({name:'',type:'error'}),'—  error');
});

test('API query encoding preserves literal source names and numeric pagination',()=>{
  assert.equal(query({q:'a&b.go',kind:'',offset:0,limit:24}),'q=a%26b.go&offset=0&limit=24');
  assert.equal(pageLabel({offset:24,limit:24,total:30}),'25–30 of 30');
});
test('call chain levels preserve direction and terminate cycles without duplicating nodes',()=>{
  const nodes = ['a','b','c'].map(id=>({symbol:{id}}));
  const edges = [{caller_id:'a',target_id:'b'},{caller_id:'b',target_id:'c'},{caller_id:'c',target_id:'a'}];
  const summarize = levels=>levels.map(items=>items.map(item=>item.symbol.id));
  assert.deepEqual(summarize(chainLevels({nodes,edges,root_id:'a',direction:'out',depth:5})),[['a'],['b'],['c']]);
  assert.deepEqual(summarize(chainLevels({nodes,edges,root_id:'a',direction:'in',depth:5})),[['a'],['c'],['b']]);
  assert.deepEqual(summarize(chainLevels({nodes,edges,root_id:'a',direction:'out',depth:1})),[['a'],['b']]);
});

test('feature action resumes incomplete coverage rather than implying a full rerun',()=>{
  assert.equal(generationLabel(null),'Queue analysis');
  assert.equal(generationLabel({analyzed_symbols:8,total_symbols:20}),'Resume analysis');
  assert.equal(generationLabel({analyzed_symbols:20,total_symbols:20}),'Rebuild analysis');
  assert.equal(generationLabel(null,{status:'running'}),'Indexing in background');
  assert.equal(generationLabel(null,{status:'failed'}),'Retry analysis');
  assert.equal(jobActive({status:'queued'}),true);
  assert.equal(jobActive({status:'completed'}),false);
});

test('routine UI refresh stays at two minutes independently of engine scans',()=>{
  assert.equal(projectRefreshInterval({status:'ready',snapshot:{id:'saved'}}),120000);
  assert.equal(projectRefreshInterval({status:'scanning',snapshot:{id:'saved'}}),120000);
  assert.equal(projectRefreshInterval({status:'scanning',snapshot:{id:'saved'}},true),1000);
  assert.equal(projectRefreshInterval({status:'ready',snapshot:null}),1000);
});

test('snapshot pins accept only validated IDs',()=>{
  const id = 'a'.repeat(64);
  assert.equal(snapshotPin(`?snapshot=${id}`),id);
  assert.equal(snapshotPin('?snapshot=../other'),'');
  assert.equal(snapshotPin(''),'');
});

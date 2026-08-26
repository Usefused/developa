import test from 'node:test';
import assert from 'node:assert/strict';
import {localOptions,featureOptions,optionStep,nextSelectID} from './assets/select-data.js';
import {SearchSelect} from './assets/search-select.js';
import {RequestGate} from './assets/api.js';

// These nodes exercise the real shared control and its events. Browser checks
// cover native focus transitions, popup layout, and React component integration.
class SelectElement {
  constructor(tag = 'div') {
    this.tagName = tag.toUpperCase();
    this.children = []; this.attributes = {}; this.listeners = {};
    this.classList = {toggle:()=>{}};
  }
  append(...children) { children.forEach(child=>{ child.parent = this; this.children.push(child); }); }
  replaceChildren(...children) { this.children = []; this.append(...children); }
  setAttribute(name, value) { this.attributes[name] = value; }
  removeAttribute(name) { delete this.attributes[name]; }
  addEventListener(name, action) { this.listeners[name] = action; }
  focus() { document.activeElement = this; }
  contains(target) { return target === this || this.children.some(child=>child.contains(target)); }
  remove() { this.parent.children = this.parent.children.filter(child=>child !== this); }
}

function interactiveSelect(t) {
  const original = globalThis.document;
  const listeners = new Map();
  globalThis.document = {createElement:tag=>new SelectElement(tag),activeElement:null,
    createElementNS:(namespace,tag)=>Object.assign(new SelectElement(tag),{namespaceURI:namespace}),
    addEventListener:(name,action)=>listeners.set(name,action),removeEventListener:name=>listeners.delete(name)};
  t.after(()=>{ globalThis.document = original; });
  const options = [{value:'method',label:'Methods'},{value:'struct',label:'Structs'}];
  const changes = [];
  const control = new SearchSelect({label:'Kind',options,selected:options[0],onChange:value=>changes.push(value)});
  return {control,changes,listeners};
}

async function typeQuery(control, value) {
  control.input.value = value;
  control.input.listeners.input();
  await Promise.resolve();
}

test('reopening a selected dropdown provides an empty search without editing its value',async t=>{
  const {control,changes} = interactiveSelect(t);
  control.trigger.listeners.click();
  await typeQuery(control,'str');
  assert.equal(control.selection.textContent,'Methods');
  assert.deepEqual(control.items.map(item=>item.value),['struct']);
  control.list.children[0].listeners.click();
  assert.deepEqual(changes,['struct']);
  assert.strictEqual(document.activeElement,control.trigger);
  control.trigger.listeners.click();
  await Promise.resolve();
  assert.equal(control.input.value,'');
  assert.equal(control.selection.textContent,'Structs');
  assert.equal(control.items.length,2);
  await typeQuery(control,'meth');
  assert.deepEqual(control.items.map(item=>item.value),['method']);
  control.close();
});

test('search cancellation keeps the committed selection and restores trigger focus',async t=>{
  const {control,changes,listeners} = interactiveSelect(t);
  control.open();
  assert.strictEqual(document.activeElement,control.input);
  await typeQuery(control,'no match');
  control.input.listeners.keydown(key('Enter'));
  assert.deepEqual(changes,[]);
  control.input.listeners.keydown(key('Escape'));
  assert.equal(control.popup.hidden,true);
  assert.equal(control.selection.textContent,'Methods');
  assert.strictEqual(document.activeElement,control.trigger);
  assert.equal(listeners.size,0);
});

test('selection synchronization does not replace an in-progress query',async t=>{
  const {control} = interactiveSelect(t);
  control.open();
  await typeQuery(control,'str');
  control.setSelected({value:'method',label:'Updated method label'});
  assert.equal(control.input.value,'str');
  assert.equal(control.query,'str');
  assert.deepEqual(control.items.map(item=>item.value),['struct']);
  control.close();
});

test('keyboard typing on the trigger starts a search but preserves modifier shortcuts',async t=>{
  const {control} = interactiveSelect(t);
  const modified = key('s',{metaKey:true});
  control.trigger.listeners.keydown(modified);
  assert.equal(control.opened,false);
  assert.equal(modified.prevented,false);
  control.trigger.listeners.keydown(key('s'));
  await Promise.resolve();
  assert.equal(control.input.value,'s');
  assert.equal(control.query,'s');
  assert.equal(control.selection.textContent,'Methods');
  control.close();
});

test('combobox IDs stay unique across separate module copies without secure-context crypto',async()=>{
  const second = await import('./assets/select-data.js?separate-bundle');
  const ids = [nextSelectID(),second.nextSelectID(),nextSelectID()];
  assert.equal(new Set(ids).size,3);
});

test('local dropdowns search labels without changing values and paginate all matches',()=>{
  const options = Array.from({length:30},(_,i)=>({value:`id-${i}`,label:`Function ${i} · path.go`}));
  const first = localOptions(options,' FUNCTION ',0);
  const next = localOptions(options,'function',24);
  assert.equal(first.total,30);
  assert.equal(first.items.length,24);
  assert.equal(next.items.length,6);
  assert.equal(next.items[0].value,'id-24');
  assert.deepEqual(localOptions(options,'nothing',0).items,[]);
  assert.equal(options.length,30);
});

test('feature dropdown searches the whole saved index with bounded snapshot-scoped reads',async()=>{
  const calls = [];
  const signal = new AbortController().signal;
  const api = {features:async(...args)=>{
    calls.push(args);
    return {items:[{id:'found',title:'Feature beyond the first page'}],total:25,offset:24,limit:24};
  }};
  const page = await featureOptions(api,'source-A')('worker',24,signal);
  assert.deepEqual(calls,[['source-A',{q:'worker',offset:24,limit:24},signal]]);
  assert.deepEqual(page.items,[{value:'found',label:'Feature beyond the first page'}]);
  assert.equal(page.total,25);
  assert.equal(page.offset,24);
});

test('keyboard option movement wraps and handles empty search results',()=>{
  assert.equal(optionStep(-1,1,3),0);
  assert.equal(optionStep(-1,-1,3),2);
  assert.equal(optionStep(2,1,3),0);
  assert.equal(optionStep(0,-1,3),2);
  assert.equal(optionStep(0,1,0),-1);
});

function key(key, extra = {}) {
  return {key,prevented:false,stopped:false,preventDefault(){ this.prevented = true; },stopPropagation(){ this.stopped = true; },...extra};
}

test('combobox Enter selects the active option and Escape does not close its surrounding panel',()=>{
  const chosen = [];
  const control = {opened:true,active:2,choose:index=>chosen.push(index),close(){ this.opened = false; }};
  const enter = key('Enter');
  SearchSelect.prototype.keydown.call(control,enter);
  assert.deepEqual(chosen,[2]);
  assert.equal(enter.prevented,true);
  const escape = key('Escape');
  SearchSelect.prototype.keydown.call(control,escape);
  assert.equal(control.opened,false);
  assert.equal(escape.stopped,true);
  assert.equal(escape.prevented,true);
});

test('composition and Tab do not commit typed text or steal normal focus movement',()=>{
  const control = {opened:true,active:0,choose:()=>assert.fail('unexpected choice'),close:()=>assert.fail('premature blur')};
  SearchSelect.prototype.keydown.call(control,key('Enter',{isComposing:true}));
  const tab = key('Tab');
  SearchSelect.prototype.keydown.call(control,tab);
  assert.equal(tab.prevented,false);
});

test('only an existing option can be committed, and selection closes before callbacks run',()=>{
  const order = [];
  const item = {value:'canonical-id',label:'Display label'};
  const control = {items:[item],setSelected:value=>order.push(value),close:()=>order.push('close'),onChange:value=>order.push(value)};
  SearchSelect.prototype.choose.call(control,-1);
  assert.deepEqual(order,[]);
  SearchSelect.prototype.choose.call(control,0);
  assert.deepEqual(order,[item,'close','canonical-id']);
});

function loaderFixture(loadPage) {
  const accepted = [];
  const errors = [];
  return {gate:new RequestGate(),loadPage,query:'',items:[],more:{setAttribute:()=>{}},retry:{},status:{},list:{setAttribute:()=>{}},
    refocus:()=>{},clearOptions(){ this.items = []; },accept:page=>accepted.push(page),loadError:offset=>errors.push(offset),accepted,errors};
}

test('late option responses cannot overwrite a newer search or a closed picker',async()=>{
  const pending = [];
  const control = loaderFixture((_query,_offset,signal)=>new Promise(resolve=>pending.push({resolve,signal})));
  const first = SearchSelect.prototype.load.call(control,0);
  const second = SearchSelect.prototype.load.call(control,0);
  assert.equal(pending[0].signal.aborted,true);
  pending[1].resolve({items:[{value:'new'}]});
  await second;
  pending[0].resolve({items:[{value:'old'}]});
  await first;
  assert.deepEqual(control.accepted,[{items:[{value:'new'}]}]);
  const closing = SearchSelect.prototype.load.call(control,0);
  control.gate.cancel();
  pending[2].resolve({items:[{value:'closed'}]});
  await closing;
  assert.equal(control.accepted.length,1);
});

test('a failed subsequent option page retains earlier results and exposes a retry offset',async()=>{
  const control = loaderFixture(async()=>{ throw new Error('offline'); });
  control.items = [{value:'first'}];
  await SearchSelect.prototype.load.call(control,24);
  assert.deepEqual(control.items,[{value:'first'}]);
  assert.deepEqual(control.errors,[24]);
  assert.deepEqual(control.accepted,[]);
});

test('loading more uses aria-disabled without disabling the focused button',async()=>{
  let finish;
  const control = loaderFixture(()=>new Promise(resolve=>{ finish = resolve; }));
  const states = [];
  control.more.setAttribute = (name,value)=>states.push([name,value]);
  const pending = SearchSelect.prototype.load.call(control,24);
  assert.equal(control.loading,true);
  assert.equal(control.more.disabled,undefined);
  finish({items:[]});
  await pending;
  assert.equal(control.loading,false);
  assert.deepEqual(states,[['aria-disabled','true'],['aria-disabled','false']]);
});

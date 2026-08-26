import test from 'node:test';
import assert from 'node:assert/strict';
import {bindLegacyRepository,normalizePreferences,preferencesFor,preferencesKey,readPreferences,updatePreferences,writePreferences} from './app/lib/preferences.js';

test('legacy editor settings apply only to the default repository or legacy unscoped view',()=>{
  const stored = normalizePreferences({root:'/legacy/project',editor:'cursor',theme:'dark'});
  assert.deepEqual(preferencesFor(stored,'default','default'),{root:'/legacy/project',editor:'cursor',theme:'dark'});
  assert.deepEqual(preferencesFor(stored),{root:'/legacy/project',editor:'cursor',theme:'dark'});
  assert.deepEqual(preferencesFor(stored,'other','default'),{root:'',editor:'vscode',theme:'dark'});
  assert.deepEqual(preferencesFor(stored,'other'),{root:'',editor:'vscode',theme:'dark'});
});

test('repository edits preserve other overrides and a global theme without modifying legacy paths',()=>{
  const original = normalizePreferences({root:'/legacy',editor:'cursor'});
  const first = updatePreferences(original,'repo-A','default',{root:'/projects/a',editor:'vscode'});
  const second = updatePreferences(first,'repo-B','default',previous=>({...previous,root:'/projects/b',editor:'cursor',theme:'dark'}));
  assert.deepEqual(preferencesFor(second,'repo-A','default'),{root:'/projects/a',editor:'vscode',theme:'dark'});
  assert.deepEqual(preferencesFor(second,'repo-B','default'),{root:'/projects/b',editor:'cursor',theme:'dark'});
  assert.deepEqual(preferencesFor(second,'default','default'),{root:'/legacy',editor:'cursor',theme:'dark'});
  assert.deepEqual(original.repositories,{});
  assert.equal(second.root,'/legacy');
});

test('theme-only updates do not materialize or transfer legacy repository roots',()=>{
  const stored = normalizePreferences({root:'/legacy',editor:'cursor'});
  const updated = updatePreferences(stored,'default','default',current=>({...current,theme:'dark'}));
  assert.deepEqual(updated.repositories,{});
  const other = updatePreferences(updated,'other','default',{theme:'light'});
  assert.deepEqual(other.repositories,{});
  assert.equal(preferencesFor(other,'other','default').root,'');
});

test('explicit empty repository roots override legacy fallback and no-argument setters remain compatible',()=>{
  const stored = normalizePreferences({root:'/legacy',editor:'cursor'});
  const cleared = updatePreferences(stored,'default','default',{root:'',editor:'vscode'});
  assert.equal(preferencesFor(cleared,'default','default').root,'');
  const legacy = updatePreferences(cleared,undefined,undefined,{root:'/new-legacy'});
  assert.equal(preferencesFor(legacy).root,'/new-legacy');
  assert.equal(preferencesFor(legacy,'default','default').root,'');
});

test('preference parsing ignores malformed values and prototype-shaped keys cannot pollute defaults',()=>{
  for (const value of [null,[],true,'invalid']) assert.deepEqual(normalizePreferences(value),normalizePreferences({}));
  const stored = normalizePreferences(JSON.parse('{"theme":"<script>","root":{},"editor":"remote","token":"SECRET","repositories":{"__proto__":{"root":"/safe","editor":"cursor"}}}'));
  assert.deepEqual(preferencesFor(stored,'other','default'),{root:'',editor:'vscode',theme:'light'});
  assert.equal(preferencesFor(stored,'__proto__','default').root,'/safe');
  assert.equal({}.root,undefined);
  assert.ok(!JSON.stringify(stored).includes('SECRET'));
});

test('storage denial and malformed records retain usable in-memory preferences',()=>{
  const fallback = normalizePreferences({root:'/memory',theme:'dark'});
  const denied = {getItem:()=>{throw new Error('denied');},setItem:()=>{throw new Error('denied');}};
  assert.equal(readPreferences(fallback,denied),fallback);
  assert.doesNotThrow(()=>writePreferences(fallback,denied));
  assert.equal(readPreferences(fallback,{getItem:()=>'{bad json'}),fallback);
  const records = new Map();
  const storage = {getItem:key=>records.get(key) ?? null,setItem:(key,value)=>records.set(key,value)};
  writePreferences(fallback,storage);
  assert.ok(records.has(preferencesKey));
  assert.deepEqual(readPreferences(undefined,storage),fallback);
});

test('changing the server default does not reassign a legacy editor path',()=>{
  const legacy = normalizePreferences({root:'/first-project',editor:'cursor',theme:'dark'});
  const bound = bindLegacyRepository(legacy,'first');
  const reordered = bindLegacyRepository(bound,'second');
  assert.equal(reordered,bound);
  assert.deepEqual(preferencesFor(reordered,'first','second'),{root:'/first-project',editor:'cursor',theme:'dark'});
  assert.deepEqual(preferencesFor(reordered,'second','second'),{root:'',editor:'vscode',theme:'dark'});
  const persisted = normalizePreferences(JSON.parse(JSON.stringify(reordered)));
  assert.equal(preferencesFor(persisted,'second','second').root,'');
});

test('legacy Developa preferences migrate once to Denverr storage',()=>{
  const records = new Map([['developa.preferences',JSON.stringify({theme:'dark'})]]);
  const storage = {getItem:key=>records.get(key) ?? null,setItem:(key,value)=>records.set(key,value),removeItem:key=>records.delete(key)};
  assert.equal(readPreferences(undefined,storage).theme,'dark');
  assert.ok(records.has(preferencesKey));
  assert.equal(records.has('developa.preferences'),false);
});

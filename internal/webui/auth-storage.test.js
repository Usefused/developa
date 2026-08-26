import test from 'node:test';
import assert from 'node:assert/strict';
import {apiTokenKey,savedAPIToken,rememberAPIToken} from './app/lib/auth-storage.js';

test('API credentials use one bounded localStorage entry and logout deletes it',()=>{
  const storage = new Map();
  const adapter = {getItem:key=>storage.get(key),setItem:(key,value)=>storage.set(key,value),removeItem:key=>storage.delete(key)};
  assert.equal(savedAPIToken(adapter),'');
  rememberAPIToken('secret',adapter);
  assert.equal(storage.get(apiTokenKey),'secret');
  assert.equal(savedAPIToken(adapter),'secret');
  rememberAPIToken('',adapter);
  assert.equal(storage.size,0);
  assert.equal(savedAPIToken({getItem:()=> 'x'.repeat(8193)}),'');
});

test('disabled browser storage does not prevent an in-memory session',()=>{
  const blocked = ()=>{throw new Error('storage denied');};
  const storage = {getItem:blocked,setItem:blocked,removeItem:blocked};
  assert.equal(savedAPIToken(storage),'');
  assert.doesNotThrow(()=>rememberAPIToken('secret',storage));
  assert.doesNotThrow(()=>rememberAPIToken('',storage));
});

test('a legacy Developa credential migrates once to Denverr storage',()=>{
  const storage = new Map([['developa.api-token','legacy-secret']]);
  const adapter = {getItem:key=>storage.get(key) ?? null,setItem:(key,value)=>storage.set(key,value),removeItem:key=>storage.delete(key)};
  assert.equal(savedAPIToken(adapter),'legacy-secret');
  assert.equal(storage.get(apiTokenKey),'legacy-secret');
  assert.equal(storage.has('developa.api-token'),false);
});

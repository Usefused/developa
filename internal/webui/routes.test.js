import test from 'node:test';
import assert from 'node:assert/strict';
import {homeURL,pageURL,workspaceURL,updateSearch,flowOptions,offsetOf} from './app/lib/routes.js';
import {ReadCache} from './app/lib/cache.js';

const snapshot = 'a'.repeat(64);

test('legacy snapshot links open saved features while an unpinned home opens blocks',()=>{
  assert.equal(homeURL(''),'/blocks');
  assert.equal(homeURL(`?snapshot=${snapshot}`),`/features?snapshot=${snapshot}`);
  assert.equal(homeURL('?snapshot=invalid'),'/blocks');
});

test('page links encode source paths and preserve snapshot identity',()=>{
  const url = pageURL('blocks',snapshot,{file:'internal/a & b.go',symbol:'id/one'});
  const params = new URLSearchParams(url.split('?')[1]);
  assert.equal(params.get('file'),'internal/a & b.go');
  assert.equal(params.get('snapshot'),snapshot);
  assert.equal(params.get('symbol'),'id/one');
});

test('switching a feature clears the old symbol without changing traversal depth',()=>{
  const params = updateSearch(`snapshot=${snapshot}&root=old&symbol=old&depth=8`,{feature:'new',root:null,symbol:null});
  assert.deepEqual(flowOptions(params),{feature_id:'new',depth:8,limit:80});
  assert.equal(params.get('snapshot'),snapshot);
  assert.equal(params.has('symbol'),false);
});

test('invalid pagination and traversal values use bounded defaults',()=>{
  for (const input of ['-1','NaN','1.5','9007199254740992']) assert.equal(offsetOf(new URLSearchParams({offset:input})),0);
  assert.equal(offsetOf(new URLSearchParams({offset:'48'})),48);
  assert.deepEqual(flowOptions(new URLSearchParams('depth=100000&root=start')),{symbol_id:'start',depth:6,limit:80});
});

test('read cache expires after two minutes and is bounded and clearable',()=>{
  let now = 0;
  const cache = new ReadCache(()=>now,2);
  cache.set('snapshot:a',{id:'a'});
  now = 119999;
  assert.deepEqual(cache.get('snapshot:a'),{id:'a'});
  now = 120000;
  assert.equal(cache.get('snapshot:a'),undefined);
  cache.set('snapshot:b',2); cache.set('snapshot:c',3);
  assert.equal(cache.items.has('snapshot:a'),false);
  cache.clear();
  assert.equal(cache.items.size,0);
});

test('repository switches retain the page but remove all source-scoped selections',()=>{
  assert.equal(workspaceURL('/flow','new-repo'),'/flow?repo=new-repo');
  assert.equal(workspaceURL('/features','new-repo'),'/features?repo=new-repo&saved=1');
  assert.equal(workspaceURL('/chain','new-repo'),'/blocks?repo=new-repo');
  assert.equal(homeURL(`?repo=other&snapshot=${snapshot}`),`/features?snapshot=${snapshot}&repo=other`);
  assert.equal(homeURL('?repo=other'),'/blocks?repo=other');
});

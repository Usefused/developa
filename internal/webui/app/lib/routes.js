import {snapshotPin} from '../../assets/model.js';

export const navigation = [
  ['blocks','Code blocks','▦'],['flow','Code flow','⑂'],['changes','Changes','↗'],
  ['analysis','Analysis','≡'],['features','Features','◇'],
];
export const headings = {
  blocks:['A place for every piece.','Explore your codebase, one building block at a time.'],
  flow:['From entrypoint to implementation.','Follow the code behind a function or feature, one connection at a time.'],
  changes:['Your code keeps moving.','See what changed between captured source snapshots.'],
  analysis:['Know what your index knows.','Inspect source coverage, parser diagnostics, and analysis boundaries.'],
  features:['The bigger picture.','Product capabilities, grounded in the code that supports them.'],
  chain:['Follow the connections.','Trace resolved calls across the same captured snapshot.'],
};

export function pageURL(page, snapshot, values = {}) {
  const params = new URLSearchParams();
  if (snapshot) params.set('snapshot',snapshot);
  for (const [key,value] of Object.entries(values)) if (value !== '' && value !== null && value !== undefined) params.set(key,String(value));
  return `/${page}${params.size ? `?${params}` : ''}`;
}

export function updateSearch(search, values) {
  const params = new URLSearchParams(search);
  for (const [key,value] of Object.entries(values)) {
    if (value === null || value === '') params.delete(key);
    else params.set(key,String(value));
  }
  return params;
}

export function homeURL(search) {
  const snapshot = snapshotPin(search);
  return pageURL(snapshot ? 'features' : 'blocks',snapshot,{repo:new URLSearchParams(search).get('repo')});
}

export function workspaceURL(pathname, repositoryID) {
  const page = navigation.find(([name])=>pathname === `/${name}`)?.[0] || 'blocks';
  // Source selections are meaningful only within their original repository.
  return pageURL(page,null,{repo:repositoryID,saved:page === 'features' ? 1 : null});
}

export function offsetOf(params) {
  const value = Number(params.get('offset'));
  return Number.isSafeInteger(value) && value >= 0 ? value : 0;
}

export function flowOptions(params) {
  const depth = Number(params.get('depth'));
  const options = {depth:[4,6,8,12].includes(depth) ? depth : 6,limit:80};
  if (params.get('feature')) options.feature_id = params.get('feature');
  else if (params.get('root')) options.symbol_id = params.get('root');
  return options;
}

const kindNames = {function:'Function',method:'Method',struct:'Struct',interface:'Interface',interface_method:'Interface method',alias:'Alias',named_type:'Named type',field:'Field',constant:'Constant',variable:'Variable',closure:'Closure'};
const kindGroups = {function:'function',method:'function',interface_method:'function',closure:'function',struct:'type',interface:'type',alias:'type',named_type:'type',field:'value',constant:'value',variable:'value'};
export const kindLabel = kind => kindNames[kind] || kind;
export const kindGroup = kind => kindGroups[kind] || 'value';
export const baseName = path => path.split('/').at(-1);
export const directory = path => path.includes('/') ? path.slice(0,path.lastIndexOf('/')) : 'Repository root';
export const shortHash = hash => hash ? hash.slice(0,8) : 'uncommitted';
export const count = value => new Intl.NumberFormat().format(value ?? 0);
export const dateLabel = value => value ? new Date(value).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}) : '—';
export const query = values => new URLSearchParams(Object.entries(values).filter(([,value]) => value !== '' && value !== undefined)).toString();

export function snapshotPin(search) {
  const id = new URLSearchParams(search).get('snapshot') || '';
  return /^[a-f0-9]{64}$/.test(id) ? id : '';
}

export function roomGroups(kinds) {
  const groups = {function:0,type:0,value:0};
  for (const [kind, amount] of Object.entries(kinds || {})) groups[kindGroup(kind)] += amount;
  return groups;
}

export function editorHref(root, path, line, column, editor) {
  const scheme = {vscode:'vscode',cursor:'cursor'}[editor];
  const normalized = root.trim().replaceAll('\\','/').replace(/\/+$/,'');
  if (!scheme || !validRoot(normalized) || !validRelativePath(path)) return '';
  if (!Number.isInteger(line) || !Number.isInteger(column) || line < 1 || column < 1) return '';
  const absolute = `${normalized}/${path}`.split('/').map(encodeURIComponent).join('/');
  const prefix = absolute.startsWith('/') ? '' : '/';
  return `${scheme}://file${prefix}${absolute}:${line}:${column}`;
}

export function editorLocation(root, path, line, column, editor) {
  const href = editorHref(root,path,line,column,editor);
  if (!href) return '';
  const location = decodeURIComponent(new globalThis.URL(href).pathname);
  return /^\/[A-Za-z]:\//.test(location) ? location.slice(1) : location;
}

function validRoot(root) {
  return root.startsWith('/') || /^[A-Za-z]:\//.test(root);
}

function validRelativePath(path) {
  if (!path || path.startsWith('/') || path.includes('\\')) return false;
  return !path.split('/').some(part => part === '..' || part === '.' || part === '');
}

export function parameterText(parameter) {
  const variadic = parameter.variadic ? '...' : '';
  return `${parameter.name || '—'}  ${variadic}${parameter.type}`;
}

export function pageLabel(page) {
  if (page.total === 0) return 'No results';
  return `${count(page.offset + 1)}–${count(Math.min(page.offset + page.limit,page.total))} of ${count(page.total)}`;
}
// Engine polling must not shorten routine UI refreshes; only explicit work or
// waiting for the first snapshot needs a temporarily faster progress check.
export function projectRefreshInterval(project, requested = false) {
  return !project.snapshot || requested ? 1000 : 120000;
}

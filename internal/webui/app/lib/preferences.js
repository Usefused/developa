export const preferencesKey = 'developa.preferences';
const defaults = {root:'',editor:'vscode',theme:'light'};

function record(value) { return value !== null && typeof value === 'object' && !Array.isArray(value) ? value : {}; }

function editorValues(value, fallback = defaults) {
  const input = record(value);
  return {
    root:typeof input.root === 'string' && input.root.length <= 4096 ? input.root : fallback.root,
    editor:['vscode','cursor'].includes(input.editor) ? input.editor : fallback.editor,
  };
}

export function normalizePreferences(value) {
  const input = record(value);
  const repositories = Object.fromEntries(Object.entries(record(input.repositories)).map(([id,entry])=>[id,editorValues(entry)]));
  const legacyRepositoryID = typeof input.legacyRepositoryID === 'string' && input.legacyRepositoryID.length <= 256 ? input.legacyRepositoryID : '';
  return {...editorValues(input),theme:['light','dark'].includes(input.theme) ? input.theme : defaults.theme,repositories,legacyRepositoryID};
}

export function readPreferences(fallback = normalizePreferences({}), storage) {
  try {
    const value = (storage || localStorage).getItem(preferencesKey);
    return value === null ? fallback : normalizePreferences(JSON.parse(value));
  } catch { return fallback; }
}

export function writePreferences(value, storage) {
  try { (storage || localStorage).setItem(preferencesKey,JSON.stringify(value)); }
  catch { /* Optional editor settings must not prevent browsing when storage is denied. */ }
}

export function preferencesFor(stored, repositoryID = '', defaultRepositoryID = '') {
  const legacy = !repositoryID || repositoryID === (stored.legacyRepositoryID || defaultRepositoryID);
  const fallback = legacy ? stored : defaults;
  const override = repositoryID && Object.hasOwn(stored.repositories,repositoryID) ? stored.repositories[repositoryID] : null;
  return {...editorValues(override,fallback),theme:stored.theme};
}

export function bindLegacyRepository(stored, defaultRepositoryID) {
  if (!defaultRepositoryID || stored.legacyRepositoryID) return stored;
  // An operator can reorder the server's default. That must not reassign a
  // workstation path saved before repository selection existed.
  return {...stored,legacyRepositoryID:defaultRepositoryID};
}

export function updatePreferences(stored, repositoryID, defaultRepositoryID, update) {
  stored = bindLegacyRepository(stored,defaultRepositoryID);
  const current = preferencesFor(stored,repositoryID,defaultRepositoryID);
  const change = record(typeof update === 'function' ? update(current) : update);
  const editor = editorValues(change,current);
  const next = {...stored,theme:['light','dark'].includes(change.theme) ? change.theme : stored.theme};
  // A theme-only update must not create an editor override or copy a legacy root.
  if (editor.root === current.root && editor.editor === current.editor) return next;
  if (!repositoryID) return {...next,...editor};
  return {...next,repositories:{...stored.repositories,[repositoryID]:editor}};
}

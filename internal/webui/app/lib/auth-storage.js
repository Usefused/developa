export const apiTokenKey = 'denverr.api-token';
const legacyAPITokenKey = 'developa.api-token';

export function savedAPIToken(storage) {
  try {
    const target = storage || globalThis.localStorage;
    let token = target.getItem(apiTokenKey);
    if (token === null) token = migrateToken(target);
    return typeof token === 'string' && token.length <= 8192 ? token : '';
  } catch { return ''; }
}

export function rememberAPIToken(token, storage) {
  try {
    const target = storage || globalThis.localStorage;
    if (token) target.setItem(apiTokenKey,token);
    else { target.removeItem(apiTokenKey);target.removeItem(legacyAPITokenKey); }
  } catch { /* Storage may be disabled; the current in-memory session still works. */ }
}

function migrateToken(storage) {
  const token = storage.getItem(legacyAPITokenKey);
  if (token === null) return null;
  storage.setItem(apiTokenKey,token);
  storage.removeItem(legacyAPITokenKey);
  return token;
}

export const apiTokenKey = 'developa.api-token';

export function savedAPIToken(storage) {
  try {
    const token = (storage || globalThis.localStorage).getItem(apiTokenKey);
    return typeof token === 'string' && token.length <= 8192 ? token : '';
  } catch { return ''; }
}

export function rememberAPIToken(token, storage) {
  try {
    const target = storage || globalThis.localStorage;
    if (token) target.setItem(apiTokenKey,token);
    else target.removeItem(apiTokenKey);
  } catch { /* Storage may be disabled; the current in-memory session still works. */ }
}

let scope = '';
export function setStorageScope(owner: string, app: string) {
  if (!owner || owner.length > 256 || !/^[a-z]+$/.test(app)) throw new Error('Invalid canvas storage scope');
  const next = `picpilot:canvas:${encodeURIComponent(owner)}:${app}:`;
  if (scope && scope !== next) throw new Error('Account changed. Reload the canvas.');
  scope = next;
}
export function storageName(name: string): string {
  if (!scope) throw new Error('Canvas identity is not ready');
  return scope + name;
}
export const canvasStorage = {
  getItem: (key: string) => localStorage.getItem(storageName(key)),
  setItem: (key: string, value: string) => localStorage.setItem(storageName(key), value),
  removeItem: (key: string) => localStorage.removeItem(storageName(key)),
};

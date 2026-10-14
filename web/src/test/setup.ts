import '@testing-library/jest-dom'

// jsdom does not implement scrollIntoView — stub it globally
globalThis.HTMLElement.prototype.scrollIntoView = () => {}

// jsdom without a proper URL origin doesn't expose a working localStorage.
// Provide a minimal in-memory implementation so tests can exercise code that
// calls localStorage.getItem / setItem / removeItem / clear.
if (typeof localStorage === 'undefined' || typeof localStorage.removeItem !== 'function') {
  const store: Record<string, string> = {}
  const stubStorage: Storage = {
    getItem: (key: string) => Object.prototype.hasOwnProperty.call(store, key) ? store[key] : null,
    setItem: (key: string, value: string) => { store[key] = String(value) },
    removeItem: (key: string) => { delete store[key] },
    clear: () => { for (const k of Object.keys(store)) delete store[k] },
    key: (index: number) => Object.keys(store)[index] ?? null,
    get length() { return Object.keys(store).length },
  }
  Object.defineProperty(globalThis, 'localStorage', { value: stubStorage, writable: true })
}

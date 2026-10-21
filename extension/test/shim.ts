// In-memory stand-in for the chrome.storage API, so the content and map
// scripts can run injected into a live page during development.
type Listener = (changes: Record<string, { oldValue?: unknown; newValue?: unknown }>, area: string) => void;

const store: Record<string, unknown> = {};
const listeners: Listener[] = [];

(globalThis as unknown as { chrome: unknown }).chrome = {
  storage: {
    sync: {
      get: async (key: string) => ({ [key]: store[key] }),
      set: async (items: Record<string, unknown>) => {
        for (const [k, v] of Object.entries(items)) {
          const old = store[k];
          store[k] = v;
          for (const l of listeners) l({ [k]: { oldValue: old, newValue: v } }, 'sync');
        }
      },
    },
    onChanged: { addListener: (l: Listener) => listeners.push(l) },
  },
};

export {};


import { canvasStorage, storageName } from '@/lib/picpilot-storage';
import localforage from "localforage";
import type { StateStorage } from "zustand/middleware";

localforage.config({
  name: storageName("nova-image"),
  storeName: "canvas_app_state",
});

export const localForageStorage: StateStorage = {
  getItem: async (name) => {
    if (typeof window === "undefined") return null;
    try {
      return (await localforage.getItem<string>(name)) || null;
    } catch {
      return canvasStorage.getItem(name);
    }
  },
  setItem: async (name, value) => {
    if (typeof window === "undefined") return;
    try {
      await localforage.setItem(name, value);
    } catch {
      canvasStorage.setItem(name, value);
    }
  },
  removeItem: async (name) => {
    if (typeof window === "undefined") return;
    try {
      await localforage.removeItem(name);
    } catch {
      canvasStorage.removeItem(name);
    }
  },
};

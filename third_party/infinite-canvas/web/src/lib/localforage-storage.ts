import { canvasStorage as ownerStorage, storageName } from '@/lib/picpilot';
import localforage from "localforage";
import type { StateStorage } from "zustand/middleware";

localforage.config({
    name: storageName("infinite-canvas"),
    storeName: "app_state",
});

export const localForageStorage: StateStorage = {
    getItem: async (name) => {
        if (typeof window === "undefined") return null;
        try {
            return (await localforage.getItem<string>(name)) || null;
        } catch {
            return ownerStorage.getItem(name);
        }
    },
    setItem: async (name, value) => {
        if (typeof window === "undefined") return;
        try {
            await localforage.setItem(name, value);
        } catch {
            ownerStorage.setItem(name, value);
        }
    },
    removeItem: async (name) => {
        if (typeof window === "undefined") return;
        try {
            await localforage.removeItem(name);
        } catch {
            ownerStorage.removeItem(name);
        }
    },
};

import { canvasStorage } from '@/lib/picpilot';
import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";

export type ThemeName = "light" | "dark";

type ThemeStore = {
    theme: ThemeName;
    setTheme: (theme: ThemeName) => void;
};

export const useThemeStore = create<ThemeStore>()(
    persist(
        (set) => ({
            theme: "dark",
            setTheme: (theme) => set({ theme }),
        }),
        { storage: createJSONStorage(() => canvasStorage), name: "infinite-canvas:theme_store" },
    ),
);

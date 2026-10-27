import type { CanvasPlugin } from "@/types/canvas-plugin";

function disabled(): never { throw new Error("Executable canvas plugins are disabled in PicPilot"); }
export function activatePlugin(_plugin: CanvasPlugin) { disabled(); }
export function deactivatePlugin(_pluginId: string) {}
export async function installPluginFromUrl(_url: string, _opts?: { official?: boolean; bustCache?: boolean }): Promise<never> { return disabled(); }
export async function updatePlugin(_record: unknown): Promise<never> { return disabled(); }
export async function setPluginEnabled(_record: unknown, enabled: boolean) { if (enabled) disabled(); }
export function uninstallPlugin(_id: string) {}
export async function ensurePluginsLoaded() {
    // Imported and previously installed source is untrusted data, never an executable module.
}

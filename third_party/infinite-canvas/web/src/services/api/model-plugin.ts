import type { AiConfig, ModelCapability } from "@/stores/use-config-store";

export type RunPluginArgs = {
    capability: ModelCapability; script: string; config: AiConfig;
    prompt?: string; images?: string[]; messages?: unknown[]; params?: Record<string, unknown>;
    signal?: AbortSignal; onDelta?: (text: string) => void;
};
export async function runModelPlugin<T = unknown>(_args: RunPluginArgs): Promise<T> {
    throw new Error("Executable model plugins are disabled in PicPilot. Use an enabled PicPilot model.");
}
export type PluginVariable = { name: string; type: string; desc: string; capabilities?: ModelCapability[] };
export type PluginTemplate = { label: string; script: string };
export function getPluginVariables(): PluginVariable[] { return []; }
export function getPluginReturn(_capability: ModelCapability) { return ""; }
export function getPluginTemplates(): Record<ModelCapability, PluginTemplate[]> { return { image: [], text: [], video: [], audio: [] }; }
export function normalizePluginImages(result: unknown): string[] {
    if (typeof result === "string") return [result];
    return Array.isArray(result) ? result.filter((item): item is string => typeof item === "string") : [];
}

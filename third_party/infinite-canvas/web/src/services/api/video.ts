import { getBridge, getRuntime, canvasStorage, abortableDelay, safeAssetUrl } from "@/lib/picpilot";
import { uploadMediaFile, type UploadedFile } from "@/services/file-storage";
import { imageToDataUrl } from "@/services/image-storage";
import { modelOptionName, type AiConfig } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";

type RequestOptions = { signal?: AbortSignal };
export type VideoGenerationResult = { blob?: Blob; url?: string; mimeType?: string };
export type VideoGenerationTask = { id: string; provider: "picpilot"; model: string };
export type VideoGenerationTaskState = { status: "pending" } | { status: "completed"; result: VideoGenerationResult } | { status: "failed"; error: string };
type Entry = { key: string; id?: string };
const journalKey = "video-submissions";
function journal(): Record<string, Entry> { return JSON.parse(canvasStorage.getItem(journalKey) || "{}"); }

export async function createVideoGenerationTask(config: AiConfig, prompt: string, references: ReferenceImage[] = [], videoReferences: ReferenceVideo[] = [], audioReferences: ReferenceAudio[] = [], options?: RequestOptions): Promise<VideoGenerationTask> {
    const model = modelOptionName(config.model || config.videoModel);
    if (!getRuntime().capabilities.video.enabled || !getRuntime().capabilities.video.models.some(item => item.id === model)) throw new Error("The selected video model is not available in PicPilot");
    if (videoReferences.length || audioReferences.length) throw new Error("The Go video contract does not currently accept video or audio references");
    const images = await Promise.all(references.map(image => imageToDataUrl(image)));
    const payload = { prompt, images, modelCode: model, params: { seconds: config.videoSeconds, ratio: config.size, resolution: config.vquality, generateAudio: config.videoGenerateAudio } };
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(JSON.stringify(payload)));
    const fingerprint = Array.from(new Uint8Array(digest), value => value.toString(16).padStart(2, "0")).join("");
    const saved = journal();
    const entry = saved[fingerprint] || { key: crypto.randomUUID() };
    canvasStorage.setItem(journalKey, JSON.stringify({ ...saved, [fingerprint]: entry }));
    if (!entry.id) {
        const result = await getBridge().request("video.submit", { ...payload, idempotencyKey: entry.key }, options?.signal);
        entry.id = result.id;
        canvasStorage.setItem(journalKey, JSON.stringify({ ...journal(), [fingerprint]: entry }));
    }
    return { id: entry.id, provider: "picpilot", model };
}
export async function pollVideoGenerationTask(_config: AiConfig, task: VideoGenerationTask, options?: RequestOptions): Promise<VideoGenerationTaskState> {
    if (task.provider !== "picpilot") throw new Error("This task belongs to a different backend and cannot be resumed here");
    const result = await getBridge().request("video.status", { taskId: task.id }, options?.signal);
    if (["completed", "failed", "cancelled", "expired"].includes(result.status)) {
        const saved = journal();
        for (const [key, value] of Object.entries(saved)) if (value.id === task.id) delete saved[key];
        canvasStorage.setItem(journalKey, JSON.stringify(saved));
        if (result.status === "completed" && result.result_url) return { status: "completed", result: { url: safeAssetUrl(result.result_url), mimeType: "video/mp4" } };
        return { status: "failed", error: result.safe_error || result.status };
    }
    return { status: "pending" };
}
export async function requestVideoGeneration(config: AiConfig, prompt: string, references: ReferenceImage[] = [], videoReferences: ReferenceVideo[] = [], audioReferences: ReferenceAudio[] = [], options?: RequestOptions): Promise<VideoGenerationResult> {
    const task = await createVideoGenerationTask(config, prompt, references, videoReferences, audioReferences, options);
    for (;;) {
        const state = await pollVideoGenerationTask(config, task, options);
        if (state.status === "completed") return state.result;
        if (state.status === "failed") throw new Error(state.error);
        await abortableDelay(3000, options?.signal);
    }
}
export async function storeGeneratedVideo(result: VideoGenerationResult): Promise<UploadedFile> {
    if (result.blob) return uploadMediaFile(result.blob, "video");
    if (result.url) return uploadMediaFile(safeAssetUrl(result.url), "video");
    throw new Error("No playable video returned");
}

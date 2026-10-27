import { audioMimeType } from "@/lib/audio-generation";
import { uploadMediaFile, type UploadedFile } from "@/services/file-storage";
import type { AiConfig } from "@/stores/use-config-store";

export async function requestAudioGeneration(_config: AiConfig, _prompt: string, _options?: { signal?: AbortSignal }): Promise<Blob> {
    throw new Error("PicPilot does not currently expose an audio generation capability");
}
export async function storeGeneratedAudio(blob: Blob, format = "mp3"): Promise<UploadedFile> {
    const audio = blob.type.startsWith("audio/") ? blob : new Blob([blob], { type: audioMimeType(format) });
    return uploadMediaFile(audio, "audio");
}

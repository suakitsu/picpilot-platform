import { nanoid } from "nanoid";
import { getModels, getRuntime } from "@/lib/picpilot";
import { modelOptionName, type AiConfig, type ModelChannel } from "@/stores/use-config-store";
import { buildImageReferencePromptText } from "@/lib/image-reference-prompt";
import { imageToDataUrl } from "@/services/image-storage";
import type { ReferenceImage } from "@/types/image";

export type AiTextMessage = {
    role: "system" | "user" | "assistant";
    content: string | Array<{ type: "text"; text: string } | { type: "image_url"; image_url: { url: string } }>;
};
type RequestOptions = { signal?: AbortSignal };
function imageOptions(config: AiConfig, prompt: string) {
    const model = modelOptionName(config.model || config.imageModel);
    return { model, prompt: [config.systemPrompt, prompt].filter(Boolean).join("\n\n"), count: Number(config.count) || 1, size: config.size, quality: config.quality, background: config.background };
}
export async function requestGeneration(config: AiConfig, prompt: string, options?: RequestOptions) {
    return (await getModels().image(imageOptions(config, prompt), options?.signal)).map(dataUrl => ({ id: nanoid(), dataUrl }));
}
export async function requestEdit(config: AiConfig, prompt: string, references: ReferenceImage[], mask?: ReferenceImage, options?: RequestOptions) {
    if (mask) throw new Error("Dedicated mask editing is not supported by the Go API yet. Reference editing and annotations remain available.");
    const images = await Promise.all(references.map(image => imageToDataUrl(image)));
    return (await getModels().image({ ...imageOptions(config, buildImageReferencePromptText(prompt, references)), images }, options?.signal)).map(dataUrl => ({ id: nanoid(), dataUrl }));
}
export async function requestImageQuestion(config: AiConfig, messages: AiTextMessage[], onDelta: (text: string) => void, options?: RequestOptions) {
    const input: AiTextMessage[] = config.systemPrompt ? [{ role: "system", content: config.systemPrompt }, ...messages] : messages;
    const answer = await getModels().text(modelOptionName(config.model || config.textModel), input, options?.signal);
    onDelta(answer);
    return answer;
}
export async function fetchImageModels(_config: Pick<AiConfig, "baseUrl" | "apiKey" | "apiFormat">) {
    return getRuntime().capabilities.image.models.map(model => model.id);
}
export async function fetchChannelModels(_channel: ModelChannel) {
    return Object.values(getRuntime().capabilities).flatMap(capability => capability.models.map(model => model.id));
}

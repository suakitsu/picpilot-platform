import { App, Button, Form, Input, Modal, Select } from "antd";
import { ExternalLink } from "lucide-react";
import { useTranslation } from "react-i18next";
import { getBridge } from "@/lib/picpilot";
import { ModelPicker } from "@/components/model-picker";
import { selectableModelsByCapability, useConfigStore, type ConfigTabKey } from "@/stores/use-config-store";

export function AppConfigPanel({ showDoneButton = false }: { showDoneButton?: boolean; initialTab?: ConfigTabKey }) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const config = useConfigStore(state => state.config);
    const update = useConfigStore(state => state.updateConfig);
    const close = useConfigStore(state => state.setConfigDialogOpen);
    return <Form layout="vertical">
        <Button className="mb-4" icon={<ExternalLink size={16} />} onClick={() => void getBridge().request("settings.open", {}).catch(error => message.error(String(error)))}>PicPilot</Button>
        {(["image", "text", "video"] as const).map(capability => {
            const key = `${capability}Model` as const;
            return <Form.Item key={capability} label={t(`config.preferences.default${capability[0].toUpperCase() + capability.slice(1)}Model`)}>
                <ModelPicker config={config} value={config[key]} capability={capability} onChange={value => update(key, value)} />
            </Form.Item>;
        })}
        <Form.Item label={t("config.preferences.reasoningEffort")}>
            <Select value={config.reasoningEffort} options={["auto", "low", "medium", "high"].map(value => ({ value, label: value }))} onChange={value => update("reasoningEffort", value)} />
        </Form.Item>
        <Form.Item label={t("config.preferences.systemPrompt")}><Input.TextArea rows={4} value={config.systemPrompt} onChange={event => update("systemPrompt", event.target.value)} /></Form.Item>
        {showDoneButton && <Button type="primary" onClick={() => close(false)}>{t("common.done")}</Button>}
    </Form>;
}
export function AppConfigModal() {
    const { t } = useTranslation();
    const open = useConfigStore(state => state.isConfigOpen);
    const close = useConfigStore(state => state.setConfigDialogOpen);
    return <Modal title={t("config.title")} open={open} onCancel={() => close(false)} footer={null} width={720} styles={{ body: { maxHeight: "72vh", overflowY: "auto" } }}><AppConfigPanel showDoneButton /></Modal>;
}

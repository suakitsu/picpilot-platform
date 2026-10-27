import { Modal } from "antd";
export function CanvasPluginManagerModal({ open, onClose }: { open: boolean; onClose: () => void }) {
    return <Modal open={open} onCancel={onClose} footer={null} title="PicPilot"><p role="alert">Executable canvas plugins are disabled.</p></Modal>;
}

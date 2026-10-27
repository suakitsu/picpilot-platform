import { initializePicPilot } from "@/lib/picpilot";
import "antd/dist/reset.css";
import "streamdown/styles.css";
import "./styles/globals.css";

async function boot() {
    await initializePicPilot();
    const { mount } = await import("./mount");
    mount();
}
boot().catch(error => {
    const root = document.getElementById("root")!;
    const message = document.createElement("p");
    message.setAttribute("role", "alert");
    message.textContent = error instanceof Error ? error.message : "PicPilot connection failed";
    const retry = document.createElement("button");
    retry.textContent = "Retry";
    retry.onclick = () => location.reload();
    root.replaceChildren(message, retry);
});

import { useState } from "react";
import { APP_VERSION } from "@/constant/env";
export function useVersionCheck() {
    const [open, setOpen] = useState(false);
    return { open, setOpen, openReleaseModal: () => setOpen(true), latestVersion: APP_VERSION, releases: __APP_RELEASES__ || [], checking: false, hasNewVersion: false, checkLatestRelease: async (_showMessage = false) => true };
}

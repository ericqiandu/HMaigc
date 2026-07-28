import { message } from "antd";

import { useUserStore } from "@/stores/use-user-store";

export type SettingsSection = "preferences" | "storage";

export function settingsPath(section: SettingsSection = "preferences", continueCreation = false) {
    const params = new URLSearchParams({ section });
    if (continueCreation) params.set("continue", "1");
    return `/settings?${params.toString()}`;
}

/**
 * 画布深层组件没有路由上下文出口时统一跳转到正式设置页，避免重新引入全局配置弹窗。
 */
export function navigateToSettings(options?: { section?: SettingsSection; continueCreation?: boolean }) {
    const to = settingsPath(options?.section, options?.continueCreation);
    navigateWithinWorkspace(to);
}

function navigateWithinWorkspace(to: string) {
    const event = new CustomEvent<{ to: string }>("workspace:navigate", { detail: { to }, cancelable: true });
    if (window.dispatchEvent(event)) window.location.assign(to);
}

/**
 * 生成流程缺少系统模型时，只有管理员可进入系统模型后台。
 * 普通用户不再被引导到个人设置，也不允许配置自定义 API。
 */
export function handleMissingSystemModel() {
    if (useUserStore.getState().user?.role === "admin") {
        navigateWithinWorkspace("/admin/models");
        return;
    }
    message.error("系统暂未配置可用模型，请联系管理员");
}

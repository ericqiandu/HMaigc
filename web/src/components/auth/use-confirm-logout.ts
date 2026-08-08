import { App } from "antd";
import { useCallback } from "react";
import { useNavigate } from "react-router";

import { applyUserSession } from "@/lib/user-session";
import { logout } from "@/services/api/auth";

import "./logout-confirm.css";

export function useConfirmLogout() {
    const { message, modal } = App.useApp();
    const navigate = useNavigate();

    return useCallback(() => {
        modal.confirm({
            rootClassName: "account-logout-confirm",
            centered: true,
            width: 400,
            icon: null,
            title: "您确定要退出登录吗？",
            okText: "确认退出",
            cancelText: "取消",
            autoFocusButton: "cancel",
            keyboard: true,
            maskClosable: true,
            onOk: async () => {
                try {
                    await logout();
                    await applyUserSession({ user: null, systemChannels: [] });
                    message.success("已退出登录");
                    navigate("/login", { replace: true });
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "退出失败");
                    throw error;
                }
            },
        });
    }, [message, modal, navigate]);
}

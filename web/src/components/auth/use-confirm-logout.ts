import { App } from "antd";
import { createElement, useCallback } from "react";
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
            width: 344,
            icon: null,
            title: "您确定要退出登录吗？",
            okText: createElement("span", null, "确", "认"),
            cancelText: createElement("span", null, "取", "消"),
            autoFocusButton: null,
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

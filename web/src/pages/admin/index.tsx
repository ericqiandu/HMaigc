import { useUserStore } from "@/stores/use-user-store";
import { Button } from "antd";
import { ShieldX } from "lucide-react";
import { useNavigate } from "react-router";
import { AdminProvider } from "./admin-context";
import { AdminLayoutSettingsProvider } from "./admin-layout-settings";
import { AdminThemeProvider } from "./admin-theme";
import { AdminShell } from "./components/admin-shell";
import { AdminStatePanel } from "./components/admin-ui";

export default function AdminPage() {
    const navigate = useNavigate();
    const actor = useUserStore((state) => state.user);
    const hydrated = useUserStore((state) => state.hydrated);

    if (!hydrated) return null;
    if (actor?.role !== "admin") {
        return (
            <main className="admin-access-denied workspace-ui-scope grid min-h-dvh place-items-center bg-background px-6 py-10 text-foreground">
                <AdminStatePanel
                    icon={<ShieldX className="admin-access-denied-icon size-5" />}
                    title="无法访问管理后台"
                    description="当前账号不具备管理员权限。后台数据、模型配置和运营设置仅对管理员开放。"
                    action={
                        <Button className="admin-access-denied-action" type="primary" onClick={() => navigate("/")}>
                            返回创作台
                        </Button>
                    }
                />
            </main>
        );
    }

    return (
        <AdminLayoutSettingsProvider>
            <AdminThemeProvider>
                <AdminProvider>
                    <AdminShell />
                </AdminProvider>
            </AdminThemeProvider>
        </AdminLayoutSettingsProvider>
    );
}

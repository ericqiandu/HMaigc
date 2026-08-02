import { lazy, Suspense } from "react";
import { LoaderCircle } from "lucide-react";

import { useAdminContext } from "./admin-context";
import { AdminPageFrame } from "./components/admin-shell";
import { AdminStatePanel } from "./components/admin-ui";

const AnalyticsPanel = lazy(() => import("./components/analytics-panel"));
const AdminAnnouncementsPanel = lazy(() => import("./components/admin-announcements-panel"));
const CreditOperationsPanel = lazy(() => import("./components/credit-operations-panel"));
const AccessSettingsPanel = lazy(() => import("./components/access-settings-panel"));
const EmailSettingsPanel = lazy(() => import("./components/email-settings-panel"));

function PageFallback({ label }: { label: string }) {
    return <AdminStatePanel loading icon={<LoaderCircle className="admin-page-fallback-icon size-5" />} title={`正在读取${label}`} description="页面资源加载完成后会自动显示，无需重复刷新。" />;
}

export function AnalyticsPage() {
    const { references } = useAdminContext();
    return <AdminPageFrame title="数据概览" description="活跃、调用与成本趋势"><Suspense fallback={<PageFallback label="统计数据" />}><AnalyticsPanel users={references.users} channels={references.channels} /></Suspense></AdminPageFrame>;
}

export function AnnouncementsPage() {
    return <AdminPageFrame title="系统公告" description="发布、关闭与历史公告"><Suspense fallback={<PageFallback label="系统公告" />}><AdminAnnouncementsPanel /></Suspense></AdminPageFrame>;
}

export function CreditOperationsPage() {
    const { references } = useAdminContext();
    return <AdminPageFrame title="积分管理" description="积分规则、人工调账与异常计费"><Suspense fallback={<PageFallback label="积分管理数据" />}><CreditOperationsPanel users={references.users} /></Suspense></AdminPageFrame>;
}

export function AccessSettingsPage() {
    return <AdminPageFrame title="登录与注册" description="管理账号准入策略与 Linux.do OAuth 登录"><Suspense fallback={<PageFallback label="登录与注册配置" />}><AccessSettingsPanel /></Suspense></AdminPageFrame>;
}

export function EmailSettingsPage() {
    return <AdminPageFrame title="邮件服务" description="注册验证码 SMTP"><div className="admin-settings-page"><Suspense fallback={<PageFallback label="邮件配置" />}><EmailSettingsPanel /></Suspense></div></AdminPageFrame>;
}

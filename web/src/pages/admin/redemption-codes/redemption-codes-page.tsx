import { lazy, Suspense } from "react";
import { LoaderCircle } from "lucide-react";

import { AdminPageFrame } from "../components/admin-shell";
import { AdminStatePanel } from "../components/admin-ui";

const RedemptionCodesPanel = lazy(() => import("../components/redemption-codes-panel"));

export default function RedemptionCodesPage() {
    return (
        <AdminPageFrame title="兑换码" description="生成与查看兑换码批次">
            <Suspense fallback={<AdminStatePanel loading icon={<LoaderCircle className="admin-redemption-fallback-icon size-5" />} title="正在读取兑换码批次" description="批次与核销数据加载完成后会自动显示。" />}>
                <RedemptionCodesPanel />
            </Suspense>
        </AdminPageFrame>
    );
}

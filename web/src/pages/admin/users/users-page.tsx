import { AdminPageFrame } from "../components/admin-shell";
import { useAdminContext } from "../admin-context";
import UsersPanel from "./users-panel";

export default function UsersPage() {
    const { updateUserReference } = useAdminContext();
    return (
        <AdminPageFrame title="用户管理" description="统一维护账号身份、访问角色、启停状态与使用事实">
            <UsersPanel onUserChanged={updateUserReference} />
        </AdminPageFrame>
    );
}

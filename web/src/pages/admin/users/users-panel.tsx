import { App, Button, Checkbox, Dropdown, Input, Table, Tag } from "antd";
import { Ban, ChevronDown, Search, Settings2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { ListToolbar, PaginationBar, TableSurface } from "@/components/layout/workspace-page";
import { useDebouncedValue } from "@/hooks/use-debounced-value";
import { bulkDisableAdminUsers, deleteAdminUser, listAdminUsers, updateAdminUser, type AdminUser, type LocalUser } from "@/services/api/auth";
import { useUserStore } from "@/stores/use-user-store";
import { AdminBatchBar, AdminContentError, AdminTableEmpty, AdminTableSkeleton } from "../components/admin-ui";
import { useTableUrlState } from "../lib/use-table-url-state";
import { AdminUserDetailDrawer } from "../components/admin-user-detail-drawer";
import { createUserColumns, userColumnOptions, type UserColumnKey } from "./users-columns";
import { AdminUserEditDrawer } from "./users-drawer";

const columnStorageKey = "admin-users-visible-columns";
const allColumnKeys = userColumnOptions.map((item) => item.key);

export default function UsersPanel({ onUserChanged }: { onUserChanged?: (user: LocalUser) => void }) {
    const actor = useUserStore((state) => state.user);
    const { message, modal } = App.useApp();
    const { state, update } = useTableUrlState();
    const debouncedFilter = useDebouncedValue(state.filter);
    const [users, setUsers] = useState<AdminUser[]>([]);
    const [total, setTotal] = useState(0);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState<string | null>(null);
    const [reloadToken, setReloadToken] = useState(0);
    const [detailUserId, setDetailUserId] = useState<string | null>(null);
    const [editingUser, setEditingUser] = useState<AdminUser | null>(null);
    const [selectedUserIds, setSelectedUserIds] = useState<string[]>([]);
    const [bulkDisabling, setBulkDisabling] = useState(false);
    const [visibleColumns, setVisibleColumns] = useState<Set<UserColumnKey>>(() => {
        if (typeof window === "undefined") return new Set(allColumnKeys);
        try {
            const saved = JSON.parse(window.localStorage.getItem(columnStorageKey) || "[]") as UserColumnKey[];
            const valid = saved.filter((key) => allColumnKeys.includes(key));
            return new Set(valid.length ? [...valid, "user", "actions"] : allColumnKeys);
        } catch {
            return new Set(allColumnKeys);
        }
    });
    const requestSequence = useRef(0);
    const hasFilters = Boolean(state.filter || state.role !== "all" || state.status !== "all");
    const selectableUsers = users.filter((user) => user.id !== actor?.id && user.status !== "disabled");

    useEffect(() => {
        window.localStorage.setItem(columnStorageKey, JSON.stringify([...visibleColumns]));
    }, [visibleColumns]);

    useEffect(() => {
        const sequence = ++requestSequence.current;
        setLoading(true);
        setLoadError(null);
        void listAdminUsers({
            keyword: debouncedFilter || undefined,
            role: state.role === "all" ? undefined : state.role,
            status: state.status === "all" ? undefined : state.status,
            page: state.page,
            limit: state.pageSize,
        })
            .then((result) => {
                if (sequence !== requestSequence.current) return;
                setUsers(result.users);
                setTotal(result.total);
                setSelectedUserIds([]);
                if (result.total > 0 && result.users.length === 0 && state.page > 1) update({ page: 1 }, true);
            })
            .catch((error) => {
                if (sequence !== requestSequence.current) return;
                setLoadError(error instanceof Error ? error.message : "读取用户失败");
            })
            .finally(() => {
                if (sequence === requestSequence.current) setLoading(false);
            });
    }, [debouncedFilter, reloadToken, state.page, state.pageSize, state.role, state.status, update]);

    const replaceUser = useCallback((nextUser: LocalUser) => {
        setUsers((items) => items.map((item) => item.id === nextUser.id ? { ...item, ...nextUser } : item));
        onUserChanged?.(nextUser);
    }, [onUserChanged]);

    const toggleStatus = useCallback(async (user: AdminUser) => {
        try {
            if (user.status === "active") {
                await deleteAdminUser(user.id);
                replaceUser({ ...user, status: "disabled" });
                message.success("用户已停用并清除登录状态");
                return;
            }
            const result = await updateAdminUser(user.id, { status: "active" });
            replaceUser(result.user);
            message.success("用户已重新启用");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "更新用户状态失败");
        }
    }, [message, replaceUser]);

    const columns = useMemo(() => createUserColumns({
        actorId: actor?.id,
        visibleColumns,
        onView: (user) => setDetailUserId(user.id),
        onEdit: setEditingUser,
        onToggleStatus: toggleStatus,
    }), [actor?.id, toggleStatus, visibleColumns]);

    const resetFilters = () => update({ filter: "", role: "all", status: "all", page: 1 });

    const bulkDisable = () => {
        modal.confirm({
            className: "admin-operation-modal workspace-ui-scope",
            title: `停用选中的 ${selectedUserIds.length} 个用户？`,
            content: "这些用户的全部登录态会被清除，身份、任务和积分流水继续保留。操作会整体成功或整体回滚。",
            okText: "确认批量停用",
            cancelText: "取消",
            okButtonProps: { danger: true },
            onOk: async () => {
                setBulkDisabling(true);
                try {
                    const result = await bulkDisableAdminUsers(selectedUserIds);
                    result.users.forEach(replaceUser);
                    setSelectedUserIds([]);
                    message.success(`已停用 ${result.disabledCount} 个用户`);
                } catch (error) {
                    message.error(error instanceof Error ? error.message : "批量停用用户失败");
                } finally {
                    setBulkDisabling(false);
                }
            },
        });
    };

    return (
        <>
            <ListToolbar
                className="admin-users-list-toolbar"
                active={hasFilters}
                onReset={resetFilters}
                trailing={
                    <div className="admin-users-toolbar-trailing flex items-center gap-3">
                        <span className="admin-users-result-count whitespace-nowrap">共 {total} 位用户</span>
                        <Dropdown
                            rootClassName="admin-column-picker-dropdown workspace-ui-scope"
                            trigger={["click"]}
                            dropdownRender={() => (
                                <div className="admin-column-picker w-52 p-2">
                                    <div className="admin-column-picker-title px-2 pb-2">显示列</div>
                                    <div className="admin-column-picker-options space-y-0.5">
                                        {userColumnOptions.map((option) => (
                                            <label key={option.key} className="admin-column-picker-option flex cursor-pointer items-center gap-2 px-2">
                                                <Checkbox
                                                    className="admin-column-picker-checkbox"
                                                    checked={visibleColumns.has(option.key)}
                                                    disabled={option.locked}
                                                    onChange={(event) => setVisibleColumns((current) => {
                                                        const next = new Set(current);
                                                        if (event.target.checked) next.add(option.key);
                                                        else next.delete(option.key);
                                                        return next;
                                                    })}
                                                />
                                                <span className="admin-column-picker-label">{option.label}</span>
                                            </label>
                                        ))}
                                    </div>
                                </div>
                            )}
                        >
                            <Button className="admin-users-column-button" icon={<Settings2 className="size-4" />}>列设置</Button>
                        </Dropdown>
                    </div>
                }
            >
                <Input
                    allowClear
                    className="app-list-search"
                    prefix={<Search className="size-4 text-foreground/40" />}
                    value={state.filter}
                    placeholder="搜索用户名、名称或邮箱"
                    onChange={(event) => update({ filter: event.target.value, page: 1 }, true)}
                />
                <FilterMenu
                    label="角色"
                    value={state.role}
                    options={[{ value: "all", label: "全部角色" }, { value: "admin", label: "管理员" }, { value: "user", label: "普通用户" }]}
                    onChange={(role) => update({ role, page: 1 })}
                />
                <FilterMenu
                    label="状态"
                    value={state.status}
                    options={[{ value: "all", label: "全部状态" }, { value: "active", label: "已启用" }, { value: "disabled", label: "已停用" }]}
                    onChange={(status) => update({ status, page: 1 })}
                />
                {state.role !== "all" ? <Tag closable onClose={(event) => { event.preventDefault(); update({ role: "all", page: 1 }); }}>角色：{state.role === "admin" ? "管理员" : "普通用户"}</Tag> : null}
                {state.status !== "all" ? <Tag closable onClose={(event) => { event.preventDefault(); update({ status: "all", page: 1 }); }}>状态：{state.status === "active" ? "已启用" : "已停用"}</Tag> : null}
            </ListToolbar>

            <div className="admin-users-selection-region">
            <AdminBatchBar count={selectedUserIds.length} onClear={() => setSelectedUserIds([])}>
                <Button danger size="small" icon={<Ban className="size-3.5" />} loading={bulkDisabling} onClick={bulkDisable}>批量停用</Button>
            </AdminBatchBar>
            </div>

            <TableSurface>
                {loading && users.length === 0 ? <AdminTableSkeleton rows={8} columns={Math.max(4, columns.length)} /> : loadError && users.length === 0 ? (
                    <div className="admin-users-load-error px-4 py-5">
                        <AdminContentError title="用户列表加载失败" description={loadError} onRetry={() => setReloadToken((value) => value + 1)} />
                    </div>
                ) : (
                    <>
                        {loadError ? (
                            <div className="admin-users-refresh-error px-4 pt-4">
                                <AdminContentError title="用户列表刷新失败" description={`${loadError}。当前仍显示上一次成功读取的数据。`} onRetry={() => setReloadToken((value) => value + 1)} />
                            </div>
                        ) : null}
                        <Table
                            className="app-data-table"
                            size="middle"
                            rowKey="id"
                            loading={loading}
                            rowSelection={selectableUsers.length > 0 ? {
                                selectedRowKeys: selectedUserIds,
                                preserveSelectedRowKeys: false,
                                onChange: (keys) => setSelectedUserIds(keys.map(String)),
                                getCheckboxProps: (user) => ({ disabled: user.id === actor?.id || user.status === "disabled", name: user.displayName || user.username }),
                            } : undefined}
                            columns={columns}
                            dataSource={users}
                            pagination={false}
                            scroll={{ x: "max-content" }}
                            locale={{ emptyText: <AdminTableEmpty filtered={hasFilters} /> }}
                        />
                        <PaginationBar
                            current={state.page}
                            pageSize={state.pageSize}
                            total={total}
                            onChange={(page, pageSize) => update({ page: pageSize !== state.pageSize ? 1 : page, pageSize })}
                        />
                    </>
                )}
            </TableSurface>

            <AdminUserDetailDrawer userId={detailUserId} onClose={() => setDetailUserId(null)} />
            <AdminUserEditDrawer user={editingUser} actorId={actor?.id} onClose={() => setEditingUser(null)} onSaved={replaceUser} />
        </>
    );
}

function FilterMenu({ label, value, options, onChange }: { label: string; value: string; options: Array<{ value: string; label: string }>; onChange: (value: string) => void }) {
    const selected = options.find((option) => option.value === value)?.label || label;
    return (
        <Dropdown
            trigger={["click"]}
            menu={{
                selectable: true,
                selectedKeys: [value],
                items: options.map((option) => ({ key: option.value, label: option.label })),
                onClick: ({ key }) => onChange(key),
            }}
        >
            <Button className="admin-users-filter-button">{value === "all" ? label : selected}<ChevronDown className="ml-1 size-3.5" /></Button>
        </Dropdown>
    );
}

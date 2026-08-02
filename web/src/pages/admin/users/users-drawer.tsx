import { App, Button, Drawer, Form, Input, Select } from "antd";
import { useEffect, useMemo, useState } from "react";

import { updateAdminUser, type AdminUser, type LocalUser } from "@/services/api/auth";
import { describeAccessChanges, normalizeUserFormValues, toUserFormValues, userFormValuesEqual, type UserFormValues } from "./user-form-domain";

export function AdminUserEditDrawer({
    user,
    actorId,
    onClose,
    onSaved,
}: {
    user: AdminUser | null;
    actorId?: string;
    onClose: () => void;
    onSaved: (user: LocalUser) => void;
}) {
    const { message, modal } = App.useApp();
    const [saving, setSaving] = useState(false);
    const [form] = Form.useForm<UserFormValues>();
    const watchedValues = Form.useWatch([], form);
    const editingSelf = user?.id === actorId;
    const initialValues = useMemo(() => user ? toUserFormValues(user) : null, [user]);
    const dirty = Boolean(initialValues && watchedValues && !userFormValuesEqual(initialValues, normalizeUserFormValues(watchedValues, initialValues)));

    useEffect(() => {
        if (!user) return;
        form.resetFields();
        form.setFieldsValue(toUserFormValues(user));
    }, [form, user]);

    const close = () => {
        if (saving) return;
        if (!dirty) {
            onClose();
            return;
        }
        modal.confirm({
            title: "放弃用户修改？",
            content: "尚未保存的账号、角色或状态修改将丢失。",
            okText: "放弃修改",
            cancelText: "继续编辑",
            okButtonProps: { danger: true },
            onOk: onClose,
        });
    };

    const save = async () => {
        if (!user) return;
        if (!initialValues) return;
        const values = normalizeUserFormValues(await form.validateFields(), initialValues);
        if (initialValues && userFormValuesEqual(initialValues, values)) return;
        const accessChanged = values.role !== user.role || values.status !== user.status;
        if (accessChanged) {
            const accessChanges = describeAccessChanges(user, values);
            const confirmed = await new Promise<boolean>((resolve) => {
                modal.confirm({
                    className: "admin-operation-modal workspace-ui-scope",
                    title: "确认变更用户访问权限？",
                    content: `目标用户：${user.displayName || user.username}（@${user.username}）。${accessChanges.join("；")}。`,
                    okText: "确认并保存",
                    cancelText: "返回检查",
                    okButtonProps: { danger: values.status === "disabled" || (user.role === "admin" && values.role === "user") },
                    onOk: () => resolve(true),
                    onCancel: () => resolve(false),
                });
            });
            if (!confirmed) return;
        }
        setSaving(true);
        try {
            const result = await updateAdminUser(user.id, {
                displayName: values.displayName,
                email: values.email,
                role: values.role,
                status: values.status,
            });
            onSaved(result.user);
            form.resetFields();
            onClose();
            message.success("用户信息已保存");
        } catch (error) {
            message.error(error instanceof Error ? error.message : "保存用户失败");
        } finally {
            setSaving(false);
        }
    };

    return (
        <Drawer
            className="admin-object-drawer admin-user-edit-drawer"
            title={user ? `编辑用户 · ${user.displayName || user.username}` : "编辑用户"}
            open={Boolean(user)}
            width="min(520px, 100vw)"
            onClose={close}
            maskClosable={!saving}
            destroyOnHidden
            extra={<Button className="admin-user-save-button" type="primary" loading={saving} disabled={!dirty} onClick={() => void save()}>保存</Button>}
        >
            <Form className="admin-user-edit-form" form={form} layout="vertical" requiredMark={false}>
                <Form.Item label="用户名">
                    <Input value={user ? `@${user.username}` : ""} disabled />
                </Form.Item>
                <Form.Item name="displayName" label="显示名称" rules={[{ required: true, whitespace: true, message: "请填写显示名称" }]}>
                    <Input placeholder="用户在产品内显示的名称" />
                </Form.Item>
                <Form.Item name="email" label="邮箱" rules={[{ type: "email", message: "请输入有效邮箱" }]}>
                    <Input placeholder="name@example.com" />
                </Form.Item>
                <Form.Item name="role" label="角色" extra={editingSelf ? "不能在此修改当前管理员自己的角色。" : "角色变更会立即影响后台访问权限。"}>
                    <Select disabled={editingSelf} options={[{ label: "管理员", value: "admin" }, { label: "普通用户", value: "user" }]} />
                </Form.Item>
                <Form.Item name="status" label="账号状态" extra={editingSelf ? "不能停用当前登录账号。" : "停用后会清除登录态，但保留身份、任务和积分流水。"}>
                    <Select disabled={editingSelf} options={[{ label: "已启用", value: "active" }, { label: "已停用", value: "disabled" }]} />
                </Form.Item>
            </Form>
        </Drawer>
    );
}

import type { AdminUser, LocalUser } from "@/services/api/auth";

export type UserFormValues = Pick<LocalUser, "displayName" | "email" | "role" | "status">;

export function toUserFormValues(user: AdminUser): UserFormValues {
    return {
        displayName: user.displayName.trim(),
        email: user.email?.trim() || "",
        role: user.role,
        status: user.status,
    };
}

export function normalizeUserFormValues(values: Partial<UserFormValues>, fallback: UserFormValues): UserFormValues {
    return {
        displayName: values.displayName?.trim() ?? fallback.displayName,
        email: values.email?.trim() ?? fallback.email,
        role: values.role ?? fallback.role,
        status: values.status ?? fallback.status,
    };
}

export function userFormValuesEqual(left: UserFormValues, right: UserFormValues) {
    return left.displayName === right.displayName
        && left.email === right.email
        && left.role === right.role
        && left.status === right.status;
}

export function describeAccessChanges(user: AdminUser, values: UserFormValues) {
    const changes: string[] = [];
    if (user.role !== values.role) changes.push(`角色：${user.role === "admin" ? "管理员" : "普通用户"} → ${values.role === "admin" ? "管理员" : "普通用户"}`);
    if (user.status !== values.status) changes.push(`状态：${user.status === "active" ? "启用" : "停用"} → ${values.status === "active" ? "启用" : "停用"}`);
    return changes;
}

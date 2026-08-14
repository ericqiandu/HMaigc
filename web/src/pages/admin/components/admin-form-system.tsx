import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

type AdminFormIntroProps = {
    title: string;
    description: string;
    aside?: ReactNode;
    className?: string;
};

type AdminFormSectionProps = {
    title: string;
    description?: string;
    children: ReactNode;
    className?: string;
};

export function formatCompactNumberInput(value: string | number | undefined): string {
    if (value === undefined || value === "") return "";
    const numericValue = Number(value);
    return Number.isFinite(numericValue) ? String(numericValue) : String(value);
}

export function AdminFormIntro({ title, description, aside, className }: AdminFormIntroProps) {
    return (
        <div className={cn("admin-form-intro", className)}>
            <div className="admin-form-intro-copy">
                <h3 className="admin-form-intro-title">{title}</h3>
                <p className="admin-form-intro-description">{description}</p>
            </div>
            {aside ? <div className="admin-form-intro-aside">{aside}</div> : null}
        </div>
    );
}

export function AdminFormSection({ title, description, children, className }: AdminFormSectionProps) {
    return (
        <section className={cn("admin-form-section", className)}>
            <div className="admin-form-section-heading">
                <h3 className="admin-form-section-title">{title}</h3>
                {description ? <p className="admin-form-section-description">{description}</p> : null}
            </div>
            <div className="admin-form-section-content">{children}</div>
        </section>
    );
}

export function AdminFormGrid({ children, className }: { children: ReactNode; className?: string }) {
    return <div className={cn("admin-form-grid", className)}>{children}</div>;
}

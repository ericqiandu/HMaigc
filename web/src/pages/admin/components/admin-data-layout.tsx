import { useId, type ReactNode } from "react";

type AdminMetricBandProps = {
    title: string;
    description: string;
    queue?: ReactNode;
    children: ReactNode;
};

type AdminMetricProps = {
    label: string;
    value: ReactNode;
    detail?: ReactNode;
};

type AdminFilterSectionProps = {
    label: string;
    children: ReactNode;
};

type AdminContentSectionProps = {
    className?: string;
    title: string;
    description?: string;
    actions?: ReactNode;
    children: ReactNode;
};

export function AdminDataLayout({ children }: { children: ReactNode }) {
    return <div className="admin-data-layout">{children}</div>;
}

export function AdminMetricBand({ title, description, queue, children }: AdminMetricBandProps) {
    const headingId = useId();

    return (
        <section className="admin-metric-band" role="region" aria-labelledby={headingId}>
            <header className="admin-data-section-header">
                <div className="admin-data-section-copy">
                    <h2 id={headingId} className="admin-data-section-title">
                        {title}
                    </h2>
                    <p className="admin-data-section-description">{description}</p>
                </div>
                {queue ? <div className="admin-metric-band-queue">{queue}</div> : null}
            </header>
            <dl className="admin-metric-band-list">{children}</dl>
        </section>
    );
}

export function AdminMetric({ label, value, detail }: AdminMetricProps) {
    return (
        <div className="admin-metric-item">
            <dt className="admin-metric-label">{label}</dt>
            <dd className="admin-metric-value">{value}</dd>
            {detail ? <dd className="admin-metric-detail">{detail}</dd> : null}
        </div>
    );
}

export function AdminFilterSection({ label, children }: AdminFilterSectionProps) {
    return (
        <section className="admin-filter-section" role="region" aria-label={label}>
            {children}
        </section>
    );
}

export function AdminContentSection({ className, title, description, actions, children }: AdminContentSectionProps) {
    const headingId = useId();

    return (
        <section className={["admin-content-section", className].filter(Boolean).join(" ")} role="region" aria-labelledby={headingId}>
            <header className="admin-data-section-header">
                <div className="admin-data-section-copy">
                    <h2 id={headingId} className="admin-data-section-title admin-content-section-title">
                        {title}
                    </h2>
                    {description ? <p className="admin-data-section-description">{description}</p> : null}
                </div>
                {actions ? <div className="admin-data-section-actions">{actions}</div> : null}
            </header>
            <div className="admin-content-section-body">{children}</div>
        </section>
    );
}

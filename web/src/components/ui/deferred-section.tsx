import { useEffect, useRef, useState, type ReactNode } from "react";

type DeferredSectionProps = {
    children: ReactNode;
    className: string;
    rootMargin?: string;
};

export function DeferredSection({ children, className, rootMargin = "600px 0px" }: DeferredSectionProps) {
    const containerRef = useRef<HTMLDivElement | null>(null);
    const [visible, setVisible] = useState(() => typeof IntersectionObserver === "undefined");

    useEffect(() => {
        if (visible) return;
        const container = containerRef.current;
        if (!container || typeof IntersectionObserver === "undefined") {
            setVisible(true);
            return;
        }

        const observer = new IntersectionObserver(
            (entries) => {
                if (!entries.some((entry) => entry.isIntersecting)) return;
                observer.disconnect();
                setVisible(true);
            },
            { rootMargin },
        );
        observer.observe(container);
        return () => observer.disconnect();
    }, [rootMargin, visible]);

    return (
        <div ref={containerRef} className={`deferred-section ${className}`.trim()}>
            {visible ? children : null}
        </div>
    );
}

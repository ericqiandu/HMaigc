import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { InfiniteCanvas } from "@/components/canvas/infinite-canvas";
import type { ViewportTransform } from "@/types/canvas";
import "@/styles/globals.css";

// 仅隔离生产视口组件，不请求或持久化用户画布。
function ViewportFixture() {
    const containerRef = useRef<HTMLDivElement>(null);
    const [viewport, setViewport] = useState<ViewportTransform>({ x: 55, y: -55, k: 1 });
    useEffect(() => {
        document.documentElement.dataset.viewportFixtureReady = "true";
        return () => { delete document.documentElement.dataset.viewportFixtureReady; };
    }, []);
    return <main className="viewport-test-page" style={{ height: "100vh" }}>
        <output className="viewport-test-state" hidden>{JSON.stringify(viewport)}</output>
        <InfiniteCanvas containerRef={containerRef} viewport={viewport} onViewportChange={setViewport}>
            <div className="viewport-test-marker" style={{ width: 100, height: 100 }}>视口验收标记</div>
        </InfiniteCanvas>
    </main>;
}

createRoot(document.getElementById("root")!).render(<ViewportFixture />);

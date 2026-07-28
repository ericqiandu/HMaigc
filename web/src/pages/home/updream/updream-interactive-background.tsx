import { useEffect, useRef } from "react";
import { useThemeStore, type ThemeName } from "@/stores/use-theme-store";

interface PointerPosition {
    active: boolean;
    targetX: number;
    targetY: number;
    x: number;
    y: number;
}

interface RibbonSpec {
    amplitude: number;
    baseY: number;
    color: string;
    phase: number;
    speed: number;
    width: number;
}

const BACKGROUND_HEIGHT = 760;
const MAX_PIXEL_RATIO = 1.75;
const RIBBON_SPECS: RibbonSpec[] = [
    { amplitude: 78, baseY: 0.24, color: "primary", phase: 0.2, speed: 0.00013, width: 126 },
    { amplitude: 96, baseY: 0.42, color: "secondary", phase: 2.3, speed: -0.0001, width: 164 },
    { amplitude: 70, baseY: 0.58, color: "tertiary", phase: 4.5, speed: 0.00008, width: 118 },
];

const THEME_COLORS: Record<ThemeName, Record<string, string>> = {
    dark: {
        primary: "34, 211, 238",
        secondary: "59, 130, 246",
        tertiary: "99, 102, 241",
        highlight: "186, 230, 253",
        lens: "125, 211, 252",
    },
    light: {
        primary: "14, 165, 233",
        secondary: "37, 99, 235",
        tertiary: "79, 70, 229",
        highlight: "255, 255, 255",
        lens: "56, 189, 248",
    },
};

function ribbonY(
    x: number,
    width: number,
    height: number,
    time: number,
    spec: RibbonSpec,
    pointer: PointerPosition,
    pointerEnergy: number,
    reducedMotion: boolean,
) {
    const normalizedX = width > 0 ? x / width : 0;
    const movement = reducedMotion ? 0 : time * spec.speed;
    const broadWave = Math.sin(normalizedX * Math.PI * 1.7 + spec.phase + movement) * spec.amplitude;
    const fineWave = Math.sin(normalizedX * Math.PI * 3.4 - spec.phase * 0.6 - movement * 0.75) * spec.amplitude * 0.2;
    const pointerDistance = Math.abs(x - pointer.x);
    const pointerInfluence = Math.max(0, 1 - pointerDistance / Math.max(260, width * 0.24)) * pointerEnergy;
    const pointerLift = (pointer.y - height * spec.baseY) * pointerInfluence * 0.17;

    return height * spec.baseY + broadWave + fineWave + pointerLift;
}

export function UpdreamInteractiveBackground() {
    const canvasRef = useRef<HTMLCanvasElement>(null);
    const theme = useThemeStore((state) => state.theme);

    useEffect(() => {
        const canvas = canvasRef.current;
        if (!canvas) {
            return;
        }

        const context = canvas.getContext("2d");
        if (!context) {
            return;
        }

        const colors = THEME_COLORS[theme];
        const pointer: PointerPosition = {
            active: false,
            targetX: window.innerWidth * 0.5,
            targetY: 280,
            x: window.innerWidth * 0.5,
            y: 280,
        };
        const reducedMotionQuery = window.matchMedia("(prefers-reduced-motion: reduce)");
        let reducedMotion = reducedMotionQuery.matches;
        let width = 0;
        let height = 0;
        let animationFrameId = 0;
        let previousTime = 0;
        let pointerEnergy = 0;

        const resizeCanvas = () => {
            width = window.innerWidth;
            height = Math.min(window.innerHeight, BACKGROUND_HEIGHT);
            const pixelRatio = Math.min(window.devicePixelRatio, MAX_PIXEL_RATIO);

            canvas.width = Math.round(width * pixelRatio);
            canvas.height = Math.round(height * pixelRatio);
            canvas.style.width = `${width}px`;
            canvas.style.height = `${height}px`;
            context.setTransform(pixelRatio, 0, 0, pixelRatio, 0, 0);
        };

        const drawRibbon = (time: number, spec: RibbonSpec, index: number) => {
            const points = Array.from({ length: 9 }, (_, pointIndex) => {
                const x = (pointIndex / 8) * width;
                return {
                    x,
                    y: ribbonY(x, width, height, time, spec, pointer, pointerEnergy, reducedMotion),
                };
            });
            const ribbonColor = colors[spec.color];
            const alpha = theme === "dark" ? 0.12 - index * 0.012 : 0.09 - index * 0.01;

            context.save();
            context.lineCap = "round";
            context.lineJoin = "round";
            context.beginPath();
            context.moveTo(points[0].x, points[0].y);
            for (let pointIndex = 1; pointIndex < points.length - 1; pointIndex += 1) {
                const current = points[pointIndex];
                const next = points[pointIndex + 1];
                context.quadraticCurveTo(current.x, current.y, (current.x + next.x) * 0.5, (current.y + next.y) * 0.5);
            }
            context.lineTo(points[points.length - 1].x, points[points.length - 1].y);

            context.filter = `blur(${theme === "dark" ? 34 : 42}px)`;
            context.strokeStyle = `rgba(${ribbonColor}, ${alpha})`;
            context.lineWidth = spec.width;
            context.stroke();

            context.filter = `blur(${theme === "dark" ? 8 : 12}px)`;
            context.strokeStyle = `rgba(${colors.highlight}, ${theme === "dark" ? 0.075 : 0.18})`;
            context.lineWidth = Math.max(1.25, spec.width * 0.028);
            context.stroke();
            context.restore();
        };

        const drawFrame = (time: number) => {
            context.clearRect(0, 0, width, height);

            const deltaTime = Math.min(32, time - previousTime || 16);
            previousTime = time;
            const pointerEase = 1 - Math.pow(0.001, deltaTime / 1000);
            pointer.x += (pointer.targetX - pointer.x) * pointerEase;
            pointer.y += (pointer.targetY - pointer.y) * pointerEase;
            pointerEnergy += ((pointer.active ? 1 : 0) - pointerEnergy) * pointerEase;

            const ambientX = width * 0.54 + (reducedMotion ? 0 : Math.sin(time * 0.00009) * width * 0.1);
            const ambientY = height * 0.28 + (reducedMotion ? 0 : Math.cos(time * 0.00011) * 42);
            const ambientGlow = context.createRadialGradient(ambientX, ambientY, 0, ambientX, ambientY, Math.min(width * 0.5, 620));
            ambientGlow.addColorStop(0, `rgba(${colors.primary}, ${theme === "dark" ? 0.11 : 0.085})`);
            ambientGlow.addColorStop(0.46, `rgba(${colors.secondary}, ${theme === "dark" ? 0.045 : 0.035})`);
            ambientGlow.addColorStop(1, `rgba(${colors.secondary}, 0)`);
            context.fillStyle = ambientGlow;
            context.fillRect(0, 0, width, height);

            RIBBON_SPECS.forEach((spec, index) => drawRibbon(time, spec, index));

            if (pointerEnergy > 0.01) {
                const lensRadius = Math.min(280, width * 0.24);
                const lensGlow = context.createRadialGradient(pointer.x, pointer.y, 0, pointer.x, pointer.y, lensRadius);
                lensGlow.addColorStop(0, `rgba(${colors.highlight}, ${pointerEnergy * (theme === "dark" ? 0.095 : 0.25)})`);
                lensGlow.addColorStop(0.18, `rgba(${colors.lens}, ${pointerEnergy * (theme === "dark" ? 0.08 : 0.105)})`);
                lensGlow.addColorStop(0.58, `rgba(${colors.primary}, ${pointerEnergy * 0.025})`);
                lensGlow.addColorStop(1, `rgba(${colors.primary}, 0)`);
                context.fillStyle = lensGlow;
                context.fillRect(0, 0, width, height);
            }

            if (!reducedMotion) {
                animationFrameId = window.requestAnimationFrame(drawFrame);
            }
        };

        const startAnimation = () => {
            window.cancelAnimationFrame(animationFrameId);
            previousTime = 0;
            if (reducedMotion) {
                drawFrame(0);
                return;
            }
            animationFrameId = window.requestAnimationFrame(drawFrame);
        };

        const handlePointerMove = (event: PointerEvent) => {
            if (event.pointerType === "touch") {
                return;
            }
            pointer.active = true;
            pointer.targetX = event.clientX;
            pointer.targetY = event.clientY;
            if (reducedMotion) {
                pointer.x = event.clientX;
                pointer.y = event.clientY;
                pointerEnergy = 1;
                drawFrame(0);
            }
        };

        const handlePointerLeave = () => {
            pointer.active = false;
            if (reducedMotion) {
                pointerEnergy = 0;
                drawFrame(0);
            }
        };

        const handleVisibilityChange = () => {
            if (document.hidden) {
                window.cancelAnimationFrame(animationFrameId);
                return;
            }
            startAnimation();
        };

        const handleReducedMotionChange = (event: MediaQueryListEvent) => {
            reducedMotion = event.matches;
            startAnimation();
        };

        const handleResize = () => {
            resizeCanvas();
            startAnimation();
        };

        resizeCanvas();
        startAnimation();
        window.addEventListener("resize", handleResize);
        window.addEventListener("pointermove", handlePointerMove, { passive: true });
        document.documentElement.addEventListener("pointerleave", handlePointerLeave);
        document.addEventListener("visibilitychange", handleVisibilityChange);
        reducedMotionQuery.addEventListener("change", handleReducedMotionChange);

        return () => {
            window.cancelAnimationFrame(animationFrameId);
            window.removeEventListener("resize", handleResize);
            window.removeEventListener("pointermove", handlePointerMove);
            document.documentElement.removeEventListener("pointerleave", handlePointerLeave);
            document.removeEventListener("visibilitychange", handleVisibilityChange);
            reducedMotionQuery.removeEventListener("change", handleReducedMotionChange);
        };
    }, [theme]);

    return <canvas ref={canvasRef} aria-hidden="true" className="updream-interactive-background" />;
}

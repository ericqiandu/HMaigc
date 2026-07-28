import { FolderKanban, Images, ListChecks, Maximize2, WandSparkles } from "lucide-react";

export const navigationTools = [
    {
        slug: "projects",
        label: "创作",
        icon: FolderKanban,
    },
    {
        slug: "canvas",
        label: "画布",
        icon: Maximize2,
    },
    {
        slug: "tasks",
        label: "任务",
        icon: ListChecks,
    },
    {
        slug: "assets",
        label: "素材",
        icon: Images,
    },
    {
        slug: "skills",
        label: "技能",
        icon: WandSparkles,
    },
] as const;

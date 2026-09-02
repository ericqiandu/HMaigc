export type RouteModuleKey = "home" | "projects" | "projectDetail" | "canvas" | "canvasProject" | "tasks" | "assets" | "skills" | "teams" | "wallet" | "settings";

export type RouteModuleLoaders = Record<RouteModuleKey, () => Promise<unknown>>;

export const loadHomePage = () => import("@/pages/home");
export const loadProjectsPage = () => import("@/pages/projects");
export const loadProjectDetailPage = () => import("@/pages/projects/detail");
export const loadCanvasPage = () => import("@/pages/canvas");
export const loadCanvasProjectPage = () => import("@/pages/canvas/project");
export const loadTasksPage = () => import("@/pages/tasks");
export const loadAssetsPage = () => import("@/pages/assets");
export const loadSkillsPage = () => import("@/pages/skills");
export const loadTeamsPage = () => import("@/pages/teams");
export const loadWalletPage = () => import("@/pages/wallet");
export const loadSettingsPage = () => import("@/pages/settings");

const routeModuleLoaders: RouteModuleLoaders = {
    home: loadHomePage,
    projects: loadProjectsPage,
    projectDetail: loadProjectDetailPage,
    canvas: loadCanvasPage,
    canvasProject: loadCanvasProjectPage,
    tasks: loadTasksPage,
    assets: loadAssetsPage,
    skills: loadSkillsPage,
    teams: loadTeamsPage,
    wallet: loadWalletPage,
    settings: loadSettingsPage,
};

function pathWithoutSearchOrHash(pathname: string) {
    const searchIndex = pathname.indexOf("?");
    const hashIndex = pathname.indexOf("#");
    const endIndex = [searchIndex, hashIndex].filter((index) => index >= 0).reduce((minimum, index) => Math.min(minimum, index), pathname.length);
    return pathname.slice(0, endIndex);
}

export function routeModuleKeyForPathname(pathname: string): RouteModuleKey | null {
    const path = pathWithoutSearchOrHash(pathname);
    if (path === "/") return "home";
    if (path === "/projects" || path === "/projects/") return "projects";
    if (path.startsWith("/projects/")) return "projectDetail";
    if (path === "/canvas" || path === "/canvas/") return "canvas";
    if (path.startsWith("/canvas/")) return "canvasProject";
    if (path === "/tasks" || path === "/tasks/") return "tasks";
    if (path === "/assets" || path === "/assets/") return "assets";
    if (path === "/skills" || path === "/skills/") return "skills";
    if (path === "/teams" || path === "/teams/") return "teams";
    if (path === "/wallet" || path === "/wallet/") return "wallet";
    if (path === "/settings" || path === "/settings/") return "settings";
    return null;
}

export function createRouteModulePrefetcher(loaders: RouteModuleLoaders) {
    const requests = new Map<RouteModuleKey, Promise<void>>();

    return (pathname: string): Promise<void> => {
        const key = routeModuleKeyForPathname(pathname);
        if (!key) return Promise.resolve();

        const activeRequest = requests.get(key);
        if (activeRequest) return activeRequest;

        const request = loaders[key]().then(() => undefined);
        requests.set(key, request);
        void request.catch(() => {
            if (requests.get(key) === request) requests.delete(key);
        });
        return request;
    };
}

export const prefetchRouteModule = createRouteModulePrefetcher(routeModuleLoaders);

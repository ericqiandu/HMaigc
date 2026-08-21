export function canvasUsesRevisionedMutations(projectLoaded: boolean, projectId: string | undefined) {
    return projectLoaded && Boolean(projectId?.trim());
}

export function remoteCanvasCreationRequired(
    remoteVersions: ReadonlyMap<string, string>,
    projectId: string,
) {
    return !remoteVersions.has(projectId);
}

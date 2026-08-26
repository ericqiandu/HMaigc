export function collectJavaScriptImportClosure(manifest, sources) {
    const visited = new Set();
    const files = new Set();
    const pending = [...sources];

    while (pending.length > 0) {
        const source = pending.pop();
        if (!source || visited.has(source)) continue;
        visited.add(source);

        const chunk = manifest[source];
        if (!chunk) continue;
        if (chunk.file?.endsWith(".js")) files.add(chunk.file);
        for (const dependency of chunk.imports || []) pending.push(dependency);
    }

    return [...files].sort();
}

export function evaluateJavaScriptRequestBudget(files, maxRequests) {
    const requestCount = files.length;
    return {
        requestCount,
        passed: requestCount <= maxRequests,
    };
}

export function collectStaticAssetClosure(manifest, sources) {
    const visited = new Set();
    const files = new Set();
    const pending = [...sources];

    while (pending.length > 0) {
        const source = pending.pop();
        if (!source || visited.has(source)) continue;
        visited.add(source);

        const chunk = manifest[source];
        if (!chunk) continue;
        for (const asset of chunk.assets || []) files.add(asset);
        for (const dependency of chunk.imports || []) pending.push(dependency);
    }

    return [...files].sort();
}

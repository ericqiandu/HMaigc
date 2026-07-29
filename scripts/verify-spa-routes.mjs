const baseUrl = new URL(process.argv[2] ?? "http://127.0.0.1:3000");
const spaRoutes = [
    "/",
    "/membership/",
    "/projects/",
    "/canvas",
    "/canvas/",
    "/tasks/",
    "/teams/",
    "/assets/",
    "/skills/",
    "/wallet/",
    "/settings/",
    "/admin/",
];

const failures = [];

for (const route of spaRoutes) {
    const response = await fetch(new URL(route, baseUrl), {
        redirect: "follow",
        headers: { accept: "text/html" },
    });
    const body = await response.text();
    const servesApplicationShell = response.ok && body.includes('<div id="root"></div>');

    if (!servesApplicationShell) {
        failures.push({
            route,
            status: response.status,
            finalUrl: response.url,
            contentType: response.headers.get("content-type"),
        });
    }
}

if (failures.length > 0) {
    console.error("SPA route verification failed:");
    console.error(JSON.stringify(failures, null, 2));
    process.exitCode = 1;
} else {
    console.log(`SPA route verification passed for ${spaRoutes.length} routes.`);
}

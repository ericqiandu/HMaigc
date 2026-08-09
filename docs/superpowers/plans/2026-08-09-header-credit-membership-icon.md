# Header Credit and Membership Icon Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the shared homepage and workspace account header render the reference 16px purple filled credit bolt and the existing 16px gold layered membership diamond.

**Architecture:** Keep `SiteAccountActions` as the only production owner for both homepage and workspace account actions. Add one semantic credit accent token, consume it from the shared balance icon style, preserve the existing layered membership SVG, then rebuild only the stateless local Web container so port 3000 serves the merged source.

**Tech Stack:** React 19, TypeScript, Lucide React, CSS semantic tokens, Bun test/build, Docker Compose, Nginx, Chromium.

## Global Constraints

- Workspaces in scope: projects, tasks, assets, skills and other routes already rendered by `WorkspaceTopBar`.
- Excluded surfaces: `/membership` and `/canvas/:id` remain independent and unchanged.
- Credit icon contract: Lucide `Zap`, 16px, `fill: currentcolor`, computed color `rgb(95, 111, 255)` from `--credit-accent: #5f6fff`.
- Membership icon contract: the existing `MembershipIcon`, 16px, `viewBox="0 0 1024 1024"`, eleven orange/gold path layers; never restore Lucide `Gem`.
- Do not add a second header implementation, duplicate the SVG, hard-code the color in business CSS, or change backend/database/business data.
- Rebuild only the stateless local Web service; preserve the running backend and `.local/data` volume.

---

### Task 1: Lock and implement the shared icon contract

**Files:**
- Modify: `web/test/site-header-unification.test.ts`
- Modify: `web/src/styles/design-tokens.css`
- Modify: `web/src/components/layout/site-account-actions.css`

**Interfaces:**
- Consumes: `SiteAccountActions`, `.site-account-balance-icon`, `MembershipIcon`, and the existing global semantic token scope.
- Produces: `--credit-accent: #5f6fff`, inherited by both themes and consumed only through `var(--credit-accent)`.

- [ ] **Step 1: Write the failing shared icon contract test**

Add this test inside `describe("site header unification", ...)`:

```ts
test("shared credit and membership icons match the homepage reference", async () => {
    const source = await sharedAccount.text();
    const styles = await sharedAccountStyles.text();
    const tokens = await designTokens.text();

    expect(source).toContain('<Zap className="site-account-balance-icon size-4" aria-hidden />');
    expect(source).toContain('viewBox="0 0 1024 1024"');
    expect(source).toContain("site-membership-icon-layer-11");
    expect(source).not.toContain("Gem");
    expect(tokens).toContain("--credit-accent: #5f6fff");
    expect(styles).toMatch(/\.site-account-balance-icon\s*\{[^}]*fill:\s*currentcolor;[^}]*color:\s*var\(--credit-accent\)/s);
    expect(styles).not.toMatch(/\.site-account-balance-icon\s*\{[^}]*color:\s*var\(--brand-primary\)/s);
});
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```powershell
cd web
bun test test/site-header-unification.test.ts
```

Expected: non-zero exit; the new test fails because `--credit-accent` is absent and `.site-account-balance-icon` still uses `var(--brand-primary)`.

- [ ] **Step 3: Add the semantic token**

In the light/root semantic color block of `web/src/styles/design-tokens.css`, immediately after `--focus-ring`, add:

```css
--credit-accent: #5f6fff;
```

Do not redeclare it in `.dark`; the exact reference purple must remain stable across themes.

- [ ] **Step 4: Switch only the shared credit icon to the token**

Change the existing rule in `web/src/components/layout/site-account-actions.css` to:

```css
.site-account-balance-icon {
    fill: currentcolor;
    color: var(--credit-accent);
}
```

Do not edit the `MembershipIcon` SVG paths or introduce another icon component.

- [ ] **Step 5: Run focused and full verification**

Run:

```powershell
cd web
bun test test/site-header-unification.test.ts
bun test
bun run build
cd ..
git diff --check
```

Expected: focused tests pass, all Web tests report zero failures, production build and Bundle Budget exit 0, and `git diff --check` is clean.

- [ ] **Step 6: Review and commit the source change**

Review only these three files, then commit:

```powershell
git diff -- web/test/site-header-unification.test.ts web/src/styles/design-tokens.css web/src/components/layout/site-account-actions.css
git add -- web/test/site-header-unification.test.ts web/src/styles/design-tokens.css web/src/components/layout/site-account-actions.css
git diff --cached --check
git commit -m "fix(web): 页头 - 统一算力与会员图标"
```

### Task 2: Rebuild and verify the local port-3000 demo

**Files:**
- No source files modified.
- Runtime service: Docker Compose project `hmaigc-local`, service `web`, container `hmaigc-local-web-1`.

**Interfaces:**
- Consumes: the current repository Dockerfile, existing healthy `hmaigc-local-backend-1`, and the unchanged `.local/data` bind mount.
- Produces: a freshly built stateless `hmaigc-web:local` container serving the current `main` source on `http://127.0.0.1:3000`.

- [ ] **Step 1: Verify the exact runtime target before replacement**

Run:

```powershell
docker inspect hmaigc-local-web-1 --format 'project={{index .Config.Labels "com.docker.compose.project"}} service={{index .Config.Labels "com.docker.compose.service"}} mounts={{len .Mounts}}'
docker inspect hmaigc-local-backend-1 --format 'state={{.State.Status}} health={{.State.Health.Status}}'
```

Expected: `project=hmaigc-local service=web mounts=0`; backend is `running` and `healthy`.

- [ ] **Step 2: Build and replace only the Web service**

Run from the repository root:

```powershell
docker compose build web
docker compose up -d --no-deps --force-recreate web
```

Expected: a new `hmaigc-local-web-1` is created from `hmaigc-web:local`; backend container identity and data mount are unchanged.

- [ ] **Step 3: Verify health and source cutover**

Run:

```powershell
docker inspect hmaigc-local-web-1 --format 'state={{.State.Status}} health={{.State.Health.Status}} image={{.Image}}'
curl.exe -fsS http://127.0.0.1:3000/api/health
```

Expected: Web is `running` and `healthy`; API health returns code 0.

- [ ] **Step 4: Verify homepage and workspace in real Chromium**

Reload `/` and `/projects`, then inspect both account headers. Each must satisfy:

```text
balance class prefix: site-account-
balance icon: lucide-zap site-account-balance-icon size-4
balance icon box: 16 × 16
balance icon computed color: rgb(95, 111, 255)
membership icon: site-membership-icon size-4
membership viewBox: 0 0 1024 1024
membership paths: 11
legacy Gem count: 0
console errors: 0
```

Also confirm `/canvas/:id` still has no `SiteAccountActions` owner.

- [ ] **Step 5: Final repository and runtime check**

Run:

```powershell
git status --short --branch
git diff --check
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}' | Select-String -Pattern 'NAMES|hmaigc-local'
```

Expected: `main` contains only committed changes, diff check is clean, and both local Web and backend services are healthy. Task 2 creates no source commit.

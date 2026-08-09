import { parse } from "@babel/parser";
import { readdir } from "node:fs/promises";
import postcss from "postcss";
import selectorParser from "postcss-selector-parser";

export interface NamedSource {
    fileName: string;
    source: string;
}

interface AstNode {
    type: string;
    [key: string]: unknown;
}

export interface MembershipSvgSignature {
    componentName: string | undefined;
    fileName: string;
    layerClassTokens: string[][];
    pathCount: number;
    viewBox: string | undefined;
}

export interface MembershipRouteContract {
    entryCount: number;
    entriesUsingSignatureOwner: number;
    gemUsages: number;
}

export interface RouteScopedGemUsage {
    fileName: string;
    gemUsages: number;
}

export interface CssContractDeclaration {
    important: boolean;
    property: string;
    selector: string;
    value: string;
}

export interface CssCustomPropertyDefinition {
    important: boolean;
    selectors: string[];
    value: string;
}

const membershipLayerClasses = Array.from({ length: 11 }, (_, index) => `site-membership-icon-layer-${index + 1}`);

function isAstNode(value: unknown): value is AstNode {
    return typeof value === "object" && value !== null && "type" in value && typeof value.type === "string";
}

function descendantsWithAncestors(root: unknown): Array<{ ancestors: AstNode[]; node: AstNode }> {
    const descendants: Array<{ ancestors: AstNode[]; node: AstNode }> = [];
    const visit = (value: unknown, ancestors: AstNode[]) => {
        if (Array.isArray(value)) {
            for (const item of value) visit(item, ancestors);
            return;
        }
        if (!isAstNode(value)) return;
        descendants.push({ ancestors, node: value });
        const nextAncestors = [...ancestors, value];
        for (const child of Object.values(value)) visit(child, nextAncestors);
    };
    visit(root, []);
    return descendants;
}

function descendantsMatching(root: unknown, predicate: (node: AstNode) => boolean): AstNode[] {
    return descendantsWithAncestors(root)
        .map(({ node }) => node)
        .filter(predicate);
}

function parseTsx(source: string): AstNode {
    return parse(source, { sourceType: "module", plugins: ["jsx", "typescript"] });
}

function identifierName(node: unknown): string | undefined {
    return isAstNode(node) && (node.type === "Identifier" || node.type === "JSXIdentifier") && typeof node.name === "string" ? node.name : undefined;
}

function jsxOpeningElement(node: AstNode): AstNode | undefined {
    return node.type === "JSXElement" && isAstNode(node.openingElement) && node.openingElement.type === "JSXOpeningElement" ? node.openingElement : undefined;
}

function jsxElementsNamed(root: AstNode, tagName: string): AstNode[] {
    return descendantsMatching(root, (node) => node.type === "JSXOpeningElement" && identifierName(node.name) === tagName);
}

function staticJsxAttribute(element: AstNode, attributeName: string): string | undefined {
    if (!Array.isArray(element.attributes)) return undefined;
    const attribute = element.attributes.find((candidate) => isAstNode(candidate) && candidate.type === "JSXAttribute" && identifierName(candidate.name) === attributeName);
    if (!isAstNode(attribute) || !isAstNode(attribute.value) || attribute.value.type !== "StringLiteral") return undefined;
    return typeof attribute.value.value === "string" ? attribute.value.value : undefined;
}

function classTokens(element: AstNode): string[] {
    return staticJsxAttribute(element, "className")?.split(/\s+/).filter(Boolean) ?? [];
}

function enclosingComponentName(ancestors: AstNode[]): string | undefined {
    for (const ancestor of [...ancestors].reverse()) {
        if (ancestor.type === "FunctionDeclaration") {
            const name = identifierName(ancestor.id);
            if (name) return name;
        }
        if (ancestor.type === "VariableDeclarator") {
            const name = identifierName(ancestor.id);
            if (name) return name;
        }
    }
    return undefined;
}

function isMembershipSvg(node: AstNode): boolean {
    const openingElement = jsxOpeningElement(node);
    if (!openingElement || identifierName(openingElement.name) !== "svg" || staticJsxAttribute(openingElement, "viewBox") !== "0 0 1024 1024") return false;
    const paths = jsxElementsNamed(node, "path");
    if (paths.length !== 11) return false;
    const pathClasses = paths.map(classTokens);
    return membershipLayerClasses.every((layerClass) => pathClasses.some((tokens) => tokens.includes("site-membership-icon-layer") && tokens.includes(layerClass)));
}

function lucideGemBindings(root: AstNode): { named: Set<string>; namespaces: Set<string> } {
    const named = new Set<string>();
    const namespaces = new Set<string>();
    for (const declaration of descendantsMatching(root, (node) => node.type === "ImportDeclaration")) {
        if (!isAstNode(declaration.source) || declaration.source.value !== "lucide-react" || !Array.isArray(declaration.specifiers)) continue;
        for (const specifier of declaration.specifiers) {
            if (!isAstNode(specifier)) continue;
            if (specifier.type === "ImportNamespaceSpecifier") {
                const localName = identifierName(specifier.local);
                if (localName) namespaces.add(localName);
            }
            if (specifier.type === "ImportSpecifier" && identifierName(specifier.imported) === "Gem") {
                const localName = identifierName(specifier.local);
                if (localName) named.add(localName);
            }
        }
    }
    return { named, namespaces };
}

function isGemTag(tag: unknown, bindings: ReturnType<typeof lucideGemBindings>): boolean {
    const directName = identifierName(tag);
    if (directName) return bindings.named.has(directName);
    if (!isAstNode(tag) || tag.type !== "JSXMemberExpression") return false;
    return bindings.namespaces.has(identifierName(tag.object) ?? "") && identifierName(tag.property) === "Gem";
}

function selectorPositivelyTargetsClass(selector: string, className: string): boolean {
    let matches = false;
    selectorParser((root) => {
        root.walkClasses((classNode) => {
            if (classNode.value !== className) return;
            let currentNode: typeof classNode | NonNullable<typeof classNode.parent> = classNode;
            let ancestor = classNode.parent;
            // :not/:has 参数只描述排除或关系条件，组合符左侧也不是最终命中主体；因此仅接受最终 selector subject 上的正向 class。
            while (ancestor && ancestor !== root) {
                if (ancestor.type === "pseudo" && (ancestor.value.toLowerCase() === ":not" || ancestor.value.toLowerCase() === ":has")) return;
                if (ancestor.type === "selector") {
                    const currentIndex = ancestor.index(currentNode);
                    if (ancestor.nodes.slice(currentIndex + 1).some((node) => node.type === "combinator")) return;
                }
                currentNode = ancestor;
                ancestor = ancestor.parent;
            }
            matches = true;
        });
    }).processSync(selector);
    return matches;
}

export async function readTsxSources(directory: URL, relativeDirectory = ""): Promise<NamedSource[]> {
    const sources: NamedSource[] = [];
    const entries = (await readdir(directory, { withFileTypes: true })).sort((left, right) => left.name.localeCompare(right.name));
    for (const entry of entries) {
        const relativePath = `${relativeDirectory}${entry.name}`;
        if (entry.isDirectory()) {
            sources.push(...(await readTsxSources(new URL(`${entry.name}/`, directory), `${relativePath}/`)));
        } else if (entry.isFile() && entry.name.endsWith(".tsx")) {
            sources.push({ fileName: relativePath, source: await Bun.file(new URL(entry.name, directory)).text() });
        }
    }
    return sources;
}

export function findMembershipSvgSignatures(sources: NamedSource[]): MembershipSvgSignature[] {
    const signatures: MembershipSvgSignature[] = [];
    for (const source of sources) {
        const root = parseTsx(source.source);
        for (const { ancestors, node } of descendantsWithAncestors(root)) {
            if (!isMembershipSvg(node)) continue;
            const openingElement = jsxOpeningElement(node);
            if (!openingElement) continue;
            const paths = jsxElementsNamed(node, "path");
            signatures.push({
                componentName: enclosingComponentName(ancestors),
                fileName: source.fileName,
                layerClassTokens: paths.map(classTokens),
                pathCount: paths.length,
                viewBox: staticJsxAttribute(openingElement, "viewBox"),
            });
        }
    }
    return signatures;
}

export function findComponentClassTokens(source: NamedSource, componentName: string): string[][] {
    return jsxElementsNamed(parseTsx(source.source), componentName).map(classTokens);
}

export function inspectMembershipRoutes(source: NamedSource, signatureComponentName?: string): MembershipRouteContract {
    const root = parseTsx(source.source);
    const gemBindings = lucideGemBindings(root);
    const membershipEntries = descendantsMatching(root, (node) => {
        const openingElement = jsxOpeningElement(node);
        return Boolean(openingElement && staticJsxAttribute(openingElement, "to") === "/membership");
    });
    let entriesUsingSignatureOwner = 0;
    let gemUsages = 0;
    for (const entry of membershipEntries) {
        const openingElements = descendantsMatching(entry, (node) => node.type === "JSXOpeningElement");
        if (signatureComponentName && openingElements.some((element) => identifierName(element.name) === signatureComponentName)) entriesUsingSignatureOwner += 1;
        gemUsages += openingElements.filter((element) => isGemTag(element.name, gemBindings)).length;
    }
    return { entryCount: membershipEntries.length, entriesUsingSignatureOwner, gemUsages };
}

export function collectRouteScopedGemUsages(sources: NamedSource[]): RouteScopedGemUsage[] {
    return sources.flatMap((source) => {
        const { gemUsages } = inspectMembershipRoutes(source);
        return gemUsages > 0 ? [{ fileName: source.fileName, gemUsages }] : [];
    });
}

export function collectPositiveClassDeclarations(css: string, className: string, properties: string[]): CssContractDeclaration[] {
    const propertySet = new Set(properties.map((property) => property.toLowerCase()));
    const declarations: CssContractDeclaration[] = [];
    postcss.parse(css).walkRules((rule) => {
        if (!rule.selectors.some((selector) => selectorPositivelyTargetsClass(selector, className))) return;
        for (const node of rule.nodes ?? []) {
            if (node.type !== "decl" || !propertySet.has(node.prop.toLowerCase())) continue;
            declarations.push({
                important: Boolean(node.important),
                property: node.prop.toLowerCase(),
                selector: rule.selector,
                value: node.value.trim().toLowerCase(),
            });
        }
    });
    return declarations;
}

export function collectCustomPropertyDefinitions(css: string, property: string): CssCustomPropertyDefinition[] {
    const definitions: CssCustomPropertyDefinition[] = [];
    postcss.parse(css).walkDecls(property, (declaration) => {
        const parent = declaration.parent;
        if (parent?.type === "rule") definitions.push({ important: Boolean(declaration.important), selectors: parent.selectors, value: declaration.value.trim().toLowerCase() });
    });
    return definitions;
}

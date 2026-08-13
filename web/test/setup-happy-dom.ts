import { Window } from "happy-dom";

const browserWindow = new Window({ url: "http://localhost/" });
const globals: Record<string, unknown> = {
    window: browserWindow,
    document: browserWindow.document,
    navigator: browserWindow.navigator,
    history: browserWindow.history,
    location: browserWindow.location,
    localStorage: browserWindow.localStorage,
    sessionStorage: browserWindow.sessionStorage,
    Event: browserWindow.Event,
    MouseEvent: browserWindow.MouseEvent,
    KeyboardEvent: browserWindow.KeyboardEvent,
    HTMLElement: browserWindow.HTMLElement,
    HTMLAnchorElement: browserWindow.HTMLAnchorElement,
    HTMLButtonElement: browserWindow.HTMLButtonElement,
    HTMLInputElement: browserWindow.HTMLInputElement,
    Element: browserWindow.Element,
    Node: browserWindow.Node,
    ShadowRoot: browserWindow.ShadowRoot,
    SVGElement: browserWindow.SVGElement,
    Blob: browserWindow.Blob,
    FileReader: browserWindow.FileReader,
    XMLHttpRequest: browserWindow.XMLHttpRequest,
    getComputedStyle: browserWindow.getComputedStyle.bind(browserWindow),
    requestAnimationFrame: browserWindow.requestAnimationFrame.bind(browserWindow),
    cancelAnimationFrame: browserWindow.cancelAnimationFrame.bind(browserWindow),
    ResizeObserver: browserWindow.ResizeObserver,
};

for (const [name, value] of Object.entries(globals)) {
    Object.defineProperty(globalThis, name, { configurable: true, writable: true, value });
}

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean; __APP_VERSION__: string }).IS_REACT_ACT_ENVIRONMENT = true;
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean; __APP_VERSION__: string }).__APP_VERSION__ = "test";

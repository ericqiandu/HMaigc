import Image from "@tiptap/extension-image";
import TextAlign from "@tiptap/extension-text-align";
import StarterKit from "@tiptap/starter-kit";

const supportedLinkProtocols = new Set(["http:", "https:", "mailto:", "tel:"]);

export function createLegalRichTextExtensions() {
    return [
        StarterKit.configure({
            heading: { levels: [1, 2, 3] },
            link: {
                openOnClick: false,
                HTMLAttributes: { rel: "noopener noreferrer", target: "_blank" },
            },
        }),
        TextAlign.configure({ types: ["heading", "paragraph"], alignments: ["left", "center", "right", "justify"] }),
        Image.configure({ allowBase64: false, inline: false, resize: false }),
    ];
}

export function isSafeLegalLink(rawURL: string) {
    try {
        return supportedLinkProtocols.has(new URL(rawURL).protocol);
    } catch {
        return false;
    }
}

export function isSafeLegalImageURL(rawURL: string) {
    try {
        const parsed = new URL(rawURL);
        return (parsed.protocol === "http:" || parsed.protocol === "https:") && !parsed.username && !parsed.password;
    } catch {
        return false;
    }
}

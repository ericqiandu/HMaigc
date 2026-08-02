import { EditorContent, useEditor } from "@tiptap/react";
import { useEffect } from "react";

import { createLegalRichTextExtensions } from "./legal-rich-text";

export function LegalRichTextViewer({ content }: { content: string }) {
    const editor = useEditor({
        immediatelyRender: false,
        editable: false,
        extensions: createLegalRichTextExtensions(),
        content,
        editorProps: { attributes: { class: "legal-document-rich-content" } },
    });

    useEffect(() => {
        if (!editor || editor.getHTML() === content) return;
        editor.commands.setContent(content, { emitUpdate: false });
    }, [content, editor]);

    return <EditorContent className="legal-document-rich-viewer" editor={editor} />;
}

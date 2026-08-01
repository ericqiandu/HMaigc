import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { useEffect } from "react";

export function LegalRichTextViewer({ content }: { content: string }) {
    const editor = useEditor({
        immediatelyRender: false,
        editable: false,
        extensions: [StarterKit.configure({ heading: { levels: [1, 2, 3] }, link: false })],
        content,
        editorProps: { attributes: { class: "legal-document-rich-content" } },
    });

    useEffect(() => {
        if (!editor || editor.getHTML() === content) return;
        editor.commands.setContent(content, { emitUpdate: false });
    }, [content, editor]);

    return <EditorContent className="legal-document-rich-viewer" editor={editor} />;
}

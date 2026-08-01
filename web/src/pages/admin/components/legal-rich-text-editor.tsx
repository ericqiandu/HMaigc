import CharacterCount from "@tiptap/extension-character-count";
import Placeholder from "@tiptap/extension-placeholder";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { Dropdown, Tooltip } from "antd";
import { Bold, ChevronDown, Italic, List, ListOrdered, Minus, Quote, Redo2, Strikethrough, Undo2 } from "lucide-react";
import { useEffect, type ReactNode } from "react";

const legalDocumentMaxCharacters = 50_000;

type LegalRichTextEditorProps = {
    value: string;
    placeholder: string;
    disabled?: boolean;
    onReady?: (characterCount: number) => void;
    onChange: (html: string, characterCount: number) => void;
};

export function LegalRichTextEditor({ value, placeholder, disabled = false, onReady, onChange }: LegalRichTextEditorProps) {
    const editor = useEditor({
        immediatelyRender: false,
        editable: !disabled,
        extensions: [
            StarterKit.configure({ heading: { levels: [1, 2, 3] }, link: false }),
            CharacterCount.configure({ limit: legalDocumentMaxCharacters }),
            Placeholder.configure({ placeholder }),
        ],
        content: value,
        editorProps: { attributes: { class: "admin-legal-editor-content focus:outline-none" } },
        onCreate: ({ editor: nextEditor }) => onReady?.(nextEditor.storage.characterCount.characters()),
        onUpdate: ({ editor: nextEditor }) => reportEditorValue(nextEditor, onChange),
    });

    useEffect(() => {
        if (!editor) return;
        editor.setEditable(!disabled);
    }, [disabled, editor]);

    useEffect(() => {
        if (!editor || editor.getHTML() === value) return;
        editor.commands.setContent(value, { emitUpdate: false });
    }, [editor, value]);

    return (
        <div className="admin-legal-editor" aria-disabled={disabled || undefined}>
            <LegalEditorToolbar editor={editor} />
            <EditorContent className="admin-legal-editor-surface" editor={editor} />
            <div className="admin-legal-editor-footer">
                <span className="admin-legal-editor-format">支持标题、段落、列表、引用和基础文字样式</span>
                <span className="admin-legal-editor-count">{editor?.storage.characterCount.characters() || 0} / {legalDocumentMaxCharacters.toLocaleString("zh-CN")}</span>
            </div>
        </div>
    );
}

function reportEditorValue(editor: Editor, onChange: LegalRichTextEditorProps["onChange"]) {
    const characterCount = editor.storage.characterCount.characters();
    onChange(characterCount > 0 ? editor.getHTML() : "", characterCount);
}

function LegalEditorToolbar({ editor }: { editor: Editor | null }) {
    const block = editor?.isActive("heading", { level: 1 }) ? "标题 1" : editor?.isActive("heading", { level: 2 }) ? "标题 2" : editor?.isActive("heading", { level: 3 }) ? "标题 3" : "正文";
    return (
        <div className="admin-legal-editor-toolbar" role="toolbar" aria-label="法律文档格式工具">
            <LegalEditorTool editor={editor} label="撤销" icon={<Undo2 className="admin-legal-editor-tool-icon size-3.5" />} onClick={() => editor?.chain().focus().undo().run()} />
            <LegalEditorTool editor={editor} label="重做" icon={<Redo2 className="admin-legal-editor-tool-icon size-3.5" />} onClick={() => editor?.chain().focus().redo().run()} />
            <span className="admin-legal-editor-divider" aria-hidden="true" />
            <Dropdown
                trigger={["click"]}
                menu={{
                    selectedKeys: [block],
                    items: ["正文", "标题 1", "标题 2", "标题 3"].map((key) => ({ key, label: key })),
                    onClick: ({ key }) => key === "正文" ? editor?.chain().focus().setParagraph().run() : editor?.chain().focus().toggleHeading({ level: Number(key.slice(-1)) as 1 | 2 | 3 }).run(),
                }}
            >
                <button className="admin-legal-editor-block-button" type="button" aria-label="段落格式" disabled={!editor}>
                    <span className="admin-legal-editor-block-label">{block}</span>
                    <ChevronDown className="admin-legal-editor-block-icon size-3" />
                </button>
            </Dropdown>
            <LegalEditorTool editor={editor} label="粗体" icon={<Bold className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("bold"))} onClick={() => editor?.chain().focus().toggleBold().run()} />
            <LegalEditorTool editor={editor} label="斜体" icon={<Italic className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("italic"))} onClick={() => editor?.chain().focus().toggleItalic().run()} />
            <LegalEditorTool editor={editor} label="删除线" icon={<Strikethrough className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("strike"))} onClick={() => editor?.chain().focus().toggleStrike().run()} />
            <span className="admin-legal-editor-divider" aria-hidden="true" />
            <LegalEditorTool editor={editor} label="项目符号" icon={<List className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("bulletList"))} onClick={() => editor?.chain().focus().toggleBulletList().run()} />
            <LegalEditorTool editor={editor} label="编号列表" icon={<ListOrdered className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("orderedList"))} onClick={() => editor?.chain().focus().toggleOrderedList().run()} />
            <LegalEditorTool editor={editor} label="引用" icon={<Quote className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("blockquote"))} onClick={() => editor?.chain().focus().toggleBlockquote().run()} />
            <LegalEditorTool editor={editor} label="分隔线" icon={<Minus className="admin-legal-editor-tool-icon size-3.5" />} onClick={() => editor?.chain().focus().setHorizontalRule().run()} />
        </div>
    );
}

function LegalEditorTool({ editor, label, icon, active = false, onClick }: { editor: Editor | null; label: string; icon: ReactNode; active?: boolean; onClick: () => void }) {
    return (
        <Tooltip title={label}>
            <button className={`admin-legal-editor-tool${active ? " is-active" : ""}`} type="button" aria-label={label} disabled={!editor} onClick={onClick}>{icon}</button>
        </Tooltip>
    );
}

import CharacterCount from "@tiptap/extension-character-count";
import Placeholder from "@tiptap/extension-placeholder";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import { App, Button, Dropdown, Input, Popover, Tooltip } from "antd";
import { AlignCenter, AlignJustify, AlignLeft, AlignRight, Bold, ChevronDown, ImagePlus, Italic, Link2, List, ListOrdered, Minus, Quote, Redo2, Strikethrough, Undo2 } from "lucide-react";
import { useEffect, useRef, useState, type ChangeEvent, type ReactNode } from "react";

import { createLegalRichTextExtensions, isSafeLegalImageURL, isSafeLegalLink } from "@/components/legal/legal-rich-text";
import { uploadResourceFile } from "@/services/api/resources";

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
            ...createLegalRichTextExtensions(),
            CharacterCount.configure({ limit: legalDocumentMaxCharacters }),
            Placeholder.configure({ placeholder }),
        ],
        content: value,
        editorProps: {
            attributes: {
                "aria-label": placeholder,
                "aria-multiline": "true",
                class: "admin-legal-editor-content focus:outline-none",
                role: "textbox",
            },
        },
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
                <span className="admin-legal-editor-format">支持标题、对齐、链接、图片、列表和引用</span>
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
    const { message } = App.useApp();
    const [linkOpen, setLinkOpen] = useState(false);
    const [linkValue, setLinkValue] = useState("");
    const [imageOpen, setImageOpen] = useState(false);
    const [imageURL, setImageURL] = useState("");
    const [uploadingImage, setUploadingImage] = useState(false);
    const imageInputRef = useRef<HTMLInputElement>(null);
    const block = editor?.isActive("heading", { level: 1 }) ? "标题 1" : editor?.isActive("heading", { level: 2 }) ? "标题 2" : editor?.isActive("heading", { level: 3 }) ? "标题 3" : "正文";
    const alignment = editor?.isActive({ textAlign: "center" }) ? "center" : editor?.isActive({ textAlign: "right" }) ? "right" : editor?.isActive({ textAlign: "justify" }) ? "justify" : "left";
    const alignmentIcon = alignment === "center" ? <AlignCenter className="size-3.5" /> : alignment === "right" ? <AlignRight className="size-3.5" /> : alignment === "justify" ? <AlignJustify className="size-3.5" /> : <AlignLeft className="size-3.5" />;
    const applyLink = () => {
        const href = linkValue.trim();
        if (!editor) return;
        if (href && !isSafeLegalLink(href)) return void message.error("链接仅支持 http、https、mailto 或 tel 协议");
        if (href) editor.chain().focus().extendMarkRange("link").setLink({ href }).run();
        else editor.chain().focus().unsetLink().run();
        setLinkOpen(false);
    };
    const insertImage = () => {
        const src = imageURL.trim();
        if (!editor || !isSafeLegalImageURL(src)) return void message.error("请输入有效的 HTTP 或 HTTPS 图片地址");
        editor.chain().focus().setImage({ src }).run();
        setImageURL("");
        setImageOpen(false);
    };
    const uploadImage = async (event: ChangeEvent<HTMLInputElement>) => {
        const file = event.target.files?.[0];
        event.target.value = "";
        if (!file || !editor) return;
        setUploadingImage(true);
        try {
            const resource = await uploadResourceFile(file, "image", { fileName: file.name });
            if (!resource.publicUrl || !isSafeLegalImageURL(resource.publicUrl)) throw new Error("图片已上传，但存储服务未返回可公开访问的地址，请先检查平台 OSS 公网域名配置");
            editor.chain().focus().setImage({ src: resource.publicUrl, alt: file.name }).run();
            setImageOpen(false);
            void message.success("图片已上传并插入");
        } catch (error) {
            void message.error(error instanceof Error ? error.message : "图片上传失败");
        } finally {
            setUploadingImage(false);
        }
    };
    return (
        <div className="admin-legal-editor-toolbar" role="toolbar" aria-label="法律文档格式工具">
            <LegalEditorTool editor={editor} label="撤销" icon={<Undo2 className="admin-legal-editor-tool-icon size-3.5" />} onClick={() => editor?.chain().focus().undo().run()} />
            <LegalEditorTool editor={editor} label="重做" icon={<Redo2 className="admin-legal-editor-tool-icon size-3.5" />} onClick={() => editor?.chain().focus().redo().run()} />
            <span className="admin-legal-editor-divider" aria-hidden="true" />
            <Dropdown
                rootClassName="admin-action-dropdown admin-form-dropdown workspace-ui-scope"
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
            <Dropdown
                rootClassName="admin-action-dropdown admin-form-dropdown workspace-ui-scope"
                trigger={["click"]}
                menu={{
                    selectedKeys: [alignment],
                    items: [
                        { key: "left", icon: <AlignLeft className="size-3.5" />, label: "左对齐" },
                        { key: "center", icon: <AlignCenter className="size-3.5" />, label: "居中" },
                        { key: "right", icon: <AlignRight className="size-3.5" />, label: "右对齐" },
                        { key: "justify", icon: <AlignJustify className="size-3.5" />, label: "两端对齐" },
                    ],
                    onClick: ({ key }) => editor?.chain().focus().setTextAlign(key).run(),
                }}
            >
                <button className="admin-legal-editor-tool admin-legal-editor-align" type="button" aria-label="文字对齐" disabled={!editor}>{alignmentIcon}<ChevronDown className="size-2.5" /></button>
            </Dropdown>
            <LegalEditorTool editor={editor} label="项目符号" icon={<List className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("bulletList"))} onClick={() => editor?.chain().focus().toggleBulletList().run()} />
            <LegalEditorTool editor={editor} label="编号列表" icon={<ListOrdered className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("orderedList"))} onClick={() => editor?.chain().focus().toggleOrderedList().run()} />
            <LegalEditorTool editor={editor} label="引用" icon={<Quote className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("blockquote"))} onClick={() => editor?.chain().focus().toggleBlockquote().run()} />
            <LegalEditorTool editor={editor} label="分隔线" icon={<Minus className="admin-legal-editor-tool-icon size-3.5" />} onClick={() => editor?.chain().focus().setHorizontalRule().run()} />
            <span className="admin-legal-editor-divider" aria-hidden="true" />
            <Popover rootClassName="admin-form-popover workspace-ui-scope" open={linkOpen} onOpenChange={(open) => { setLinkOpen(open); if (open) setLinkValue(String(editor?.getAttributes("link").href || "")); }} trigger="click" placement="bottom" content={<div className="admin-legal-editor-popover flex w-72 gap-2"><Input value={linkValue} placeholder="https://example.com" onChange={(event) => setLinkValue(event.target.value)} onPressEnter={applyLink} /><Button type="primary" onClick={applyLink}>应用</Button></div>}>
                <span className="admin-legal-editor-popover-trigger"><LegalEditorTool editor={editor} label="插入链接" icon={<Link2 className="admin-legal-editor-tool-icon size-3.5" />} active={Boolean(editor?.isActive("link"))} onClick={() => setLinkOpen(true)} /></span>
            </Popover>
            <input ref={imageInputRef} className="admin-legal-editor-image-input sr-only" type="file" accept="image/png,image/jpeg,image/webp,image/gif" aria-label="上传协议图片" onChange={(event) => void uploadImage(event)} />
            <Popover rootClassName="admin-form-popover workspace-ui-scope" open={imageOpen} onOpenChange={setImageOpen} trigger="click" placement="bottom" content={<div className="admin-legal-editor-popover flex w-96 max-w-[calc(100vw-32px)] flex-col gap-2"><div className="admin-legal-editor-image-actions flex gap-2"><Button loading={uploadingImage} onClick={() => imageInputRef.current?.click()}>上传图片</Button><span className="admin-legal-editor-image-separator self-center text-xs text-foreground/40">或填写公网地址</span></div><div className="admin-legal-editor-image-url flex gap-2"><Input value={imageURL} placeholder="https://example.com/image.png" onChange={(event) => setImageURL(event.target.value)} onPressEnter={insertImage} /><Button type="primary" onClick={insertImage}>插入</Button></div></div>}>
                <span className="admin-legal-editor-popover-trigger"><LegalEditorTool editor={editor} label="插入图片" icon={<ImagePlus className="admin-legal-editor-tool-icon size-3.5" />} onClick={() => setImageOpen(true)} /></span>
            </Popover>
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

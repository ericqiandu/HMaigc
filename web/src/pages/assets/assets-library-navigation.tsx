import { Archive, Download, FileUp, Folder, Image as ImageIcon, Package, Palette, Plus, Search, Shirt, Swords, Tags, Upload, UserRound, type LucideIcon } from "lucide-react";
import { Button, Dropdown, Input, Select } from "antd";

import type { AssetCategory, AssetKind } from "@/stores/use-asset-store";

export type AssetScope = "all" | "project" | "personal";

type CategoryOption = {
    label: string;
    value: AssetCategory | "all";
    icon: LucideIcon;
};

export const assetKindFilterOptions: Array<{ label: string; value: AssetKind | "all" }> = [
    { label: "全部类型", value: "all" },
    { label: "文本", value: "text" },
    { label: "图片", value: "image" },
    { label: "视频", value: "video" },
    { label: "音频", value: "audio" },
    { label: "3D 模型", value: "model" },
];

export const assetCategoryOptions: CategoryOption[] = [
    { label: "全部素材", value: "all", icon: Archive },
    { label: "角色", value: "character", icon: UserRound },
    { label: "场景", value: "environment", icon: ImageIcon },
    { label: "服饰", value: "wardrobe", icon: Shirt },
    { label: "道具", value: "prop", icon: Package },
    { label: "武器", value: "weapon", icon: Swords },
    { label: "画风", value: "style", icon: Palette },
    { label: "其他", value: "other", icon: Folder },
];

export const assetScopeOptions: Array<{ label: string; value: AssetScope }> = [
    { label: "全部素材", value: "all" },
    { label: "项目素材", value: "project" },
    { label: "个人素材", value: "personal" },
];

export function AssetScopeTabs({ value, counts, onChange }: { value: AssetScope; counts: Map<AssetScope, number>; onChange: (value: AssetScope) => void }) {
    return (
        <div className="assets-scope-row">
            <nav className="assets-scope-tabs thin-scrollbar" aria-label="素材来源">
                {assetScopeOptions.map((option) => (
                    <button key={option.value} type="button" className="assets-scope-tab" aria-current={value === option.value ? "page" : undefined} onClick={() => onChange(option.value)}>
                        <span className="assets-scope-tab-label">{option.label}</span>
                        <span className="assets-scope-tab-count">{counts.get(option.value) || 0}</span>
                    </button>
                ))}
            </nav>
        </div>
    );
}

export function AssetCategoryNavigation({ value, counts, onChange, onCreate }: { value: AssetCategory | "all"; counts: Map<AssetCategory | "all", number>; onChange: (value: AssetCategory | "all") => void; onCreate: () => void }) {
    return (
        <aside className="assets-category-panel" aria-label="素材业务分类">
            <button type="button" className="assets-category-create" onClick={onCreate}>
                <Plus className="assets-category-create-icon" />
                <span className="assets-category-create-label">新增素材</span>
            </button>
            <nav className="assets-category-navigation thin-scrollbar" aria-label="素材分类">
                {assetCategoryOptions.map((option) => {
                    const Icon = option.icon;
                    const isActive = value === option.value;
                    return (
                        <button key={option.value} type="button" className="assets-category-option" aria-current={isActive ? "page" : undefined} onClick={() => onChange(option.value)}>
                            <Icon className="assets-category-option-icon" />
                            <span className="assets-category-option-label">{option.label}</span>
                            <span className="assets-category-option-count">{counts.get(option.value) || 0}</span>
                        </button>
                    );
                })}
            </nav>
        </aside>
    );
}

export function AssetLibraryToolbar({
    keyword,
    kind,
    tag,
    availableTags,
    batchMode,
    onKeywordChange,
    onKindChange,
    onTagChange,
    onBatchModeChange,
    onExport,
    onImportPackage,
    onImportModel,
}: {
    keyword: string;
    kind: AssetKind | "all";
    tag: string;
    availableTags: string[];
    batchMode: boolean;
    onKeywordChange: (value: string) => void;
    onKindChange: (value: AssetKind | "all") => void;
    onTagChange: (value: string) => void;
    onBatchModeChange: (value: boolean) => void;
    onExport: () => void;
    onImportPackage: () => void;
    onImportModel: () => void;
}) {
    return (
        <div className="assets-library-toolbar">
            <div className="assets-library-toolbar-filters">
                <Input allowClear className="assets-library-search" prefix={<Search className="assets-library-search-icon" />} value={keyword} placeholder="搜索素材" onChange={(event) => onKeywordChange(event.target.value)} />
                <Select className="assets-library-type-select" value={kind} options={assetKindFilterOptions} onChange={onKindChange} />
                <Select
                    className="assets-library-tag-select"
                    value={tag}
                    suffixIcon={<Tags className="assets-library-tag-select-icon" />}
                    options={[{ label: "全部标签", value: "all" }, ...availableTags.map((currentTag) => ({ label: currentTag, value: currentTag }))]}
                    onChange={onTagChange}
                />
            </div>
            <div className="assets-library-toolbar-actions">
                <Button className="assets-library-batch-button" type={batchMode ? "primary" : "text"} onClick={() => onBatchModeChange(!batchMode)}>
                    {batchMode ? "退出批量" : "批量操作"}
                </Button>
                <Button className="assets-library-export-button" type="text" icon={<Download className="assets-library-export-icon" />} onClick={onExport}>
                    导出
                </Button>
                <Dropdown
                    trigger={["click"]}
                    menu={{
                        items: [
                            { key: "package", icon: <FileUp className="assets-library-menu-icon" />, label: "导入素材包", onClick: onImportPackage },
                            { key: "model", icon: <Upload className="assets-library-menu-icon" />, label: "上传 3D 模型", onClick: onImportModel },
                        ],
                    }}
                >
                    <Button className="assets-library-import-button" icon={<Upload className="assets-library-import-icon" />}>
                        导入
                    </Button>
                </Dropdown>
            </div>
        </div>
    );
}

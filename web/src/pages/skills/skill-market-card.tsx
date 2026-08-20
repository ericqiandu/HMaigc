import { Button, Skeleton } from "antd";
import { BadgeDollarSign, BookOpenText, Check, Clapperboard, FilePenLine, Heart, PanelsTopLeft, ScanEye, ShieldCheck, Sparkles, Zap, type LucideIcon } from "lucide-react";

import { skillImageUrl, type PlatformSkill } from "@/services/api/skills";

const skillIcons: Readonly<Record<string, LucideIcon>> = {
    "badge-dollar-sign": BadgeDollarSign,
    "book-open-text": BookOpenText,
    clapperboard: Clapperboard,
    "file-pen-line": FilePenLine,
    "panels-top-left": PanelsTopLeft,
    "scan-eye": ScanEye,
};

type SkillMarketCardProps = {
    skill: PlatformSkill;
    loading: boolean;
    onOpen: () => void;
    onActivate: () => void;
    onFavorite: () => void;
};

export function SkillMarketCard({ skill, loading, onOpen, onActivate, onFavorite }: SkillMarketCardProps) {
    return (
        <article className="skill-market-card">
            <button type="button" className="skill-market-card-main" onClick={onOpen} aria-label={`查看技能：${skill.name}`}>
                <span className="skill-market-card-cover">{skill.cover_url ? <img src={skillImageUrl(skill.cover_url)} alt="" className="skill-market-card-cover-image" /> : <SkillCoverFallback skill={skill} />}</span>
                <span className="skill-market-card-copy">
                    <span className="skill-market-card-title-row">
                        <strong className="skill-market-card-title">{skill.name}</strong>
                        {skill.activated ? <span className="skill-market-card-status">已激活</span> : null}
                    </span>
                    <span className="skill-market-card-uploader">
                        <ShieldCheck className="skill-market-card-uploader-icon" aria-hidden="true" />
                        <span className="skill-market-card-uploader-name">{skillUploaderLabel(skill.uploader_name)}</span>
                    </span>
                    <span className="skill-market-card-description">{skill.description || "暂无简介"}</span>
                    <span className="skill-market-card-meta">
                        <span className="skill-market-card-source">{skill.source_kind === "original" ? "平台原创" : "授权改编"}</span>
                        <span className="skill-market-card-version">V{skill.version || "-"}</span>
                    </span>
                </span>
            </button>
            <div className="skill-market-card-actions">
                <Button
                    className="skill-market-card-action"
                    type="text"
                    loading={loading}
                    icon={skill.activated ? <Check className="skill-market-card-action-icon" /> : <Zap className="skill-market-card-action-icon" />}
                    onClick={onActivate}
                    aria-label={skill.activated ? "取消激活" : "激活技能"}
                    title={skill.activated ? "取消激活" : "激活技能"}
                />
                <Button
                    className={`skill-market-card-action ${skill.liked ? "skill-market-card-action--liked" : ""}`}
                    type="text"
                    loading={loading}
                    icon={<Heart className="skill-market-card-action-icon" />}
                    onClick={onFavorite}
                    aria-label={skill.liked ? "取消收藏" : "收藏技能"}
                    title={skill.liked ? "取消收藏" : "收藏技能"}
                />
            </div>
        </article>
    );
}

export function SkillMarketSkeleton() {
    return (
        <div className="skills-market-grid" aria-label="正在加载技能">
            {Array.from({ length: 9 }).map((_, index) => (
                <div key={index} className="skill-market-card skill-market-card--skeleton">
                    <Skeleton.Image active className="skill-market-skeleton-cover" />
                    <Skeleton active title={{ width: "52%" }} paragraph={{ rows: 3, width: ["36%", "100%", "72%"] }} className="skill-market-skeleton-copy" />
                </div>
            ))}
        </div>
    );
}

export function SkillCoverFallback({ skill }: { skill: PlatformSkill }) {
    return (
        <span className="skill-cover-fallback">
            <SkillIcon icon={skill.icon} className="skill-cover-fallback-icon" />
            <span className="skill-cover-fallback-title">{skill.name}</span>
        </span>
    );
}

function SkillIcon({ icon, className }: { icon: string; className: string }) {
    const Icon = skillIcons[icon] ?? Sparkles;
    return <Icon className={className} aria-hidden="true" />;
}

function skillUploaderLabel(uploaderName: string | undefined) {
    const normalizedName = uploaderName?.trim();
    return normalizedName || "HMaigc";
}

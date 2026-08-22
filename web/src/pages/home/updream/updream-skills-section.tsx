import { ChevronRight, RefreshCw, Sparkles, UserRound, Zap } from "lucide-react";
import { Link } from "react-router";

import { useSiteSettings } from "@/components/site/site-settings-provider";
import skill1 from "@/pages/home/updream/assets/skill-1.png";
import skill2 from "@/pages/home/updream/assets/skill-2.png";
import skill3 from "@/pages/home/updream/assets/skill-3.png";
import skill4 from "@/pages/home/updream/assets/skill-4.png";
import skill5 from "@/pages/home/updream/assets/skill-5.png";
import skill6 from "@/pages/home/updream/assets/skill-6.png";
import { skillImageUrl, type PlatformSkill } from "@/services/api/skills";

import "@/pages/home/updream/updream-skills-section.css";

const HOME_SKILL_COVERS: Readonly<Record<string, string>> = {
    "screenplay-writer": skill1,
    "short-drama-director": skill2,
    "story-development": skill3,
    "storyboard-continuity-director": skill4,
    "commercial-film-director": skill5,
    "suspense-visual-director": skill6,
};

function FirstPartySkillCard({ skill, siteName, index }: { skill: PlatformSkill; siteName: string; index: number }) {
    const coverURL = HOME_SKILL_COVERS[skill.dir] ?? skillImageUrl(skill.cover_url);
    return (
        <Link to="/skills" className="updream-skill-link">
            <article className="updream-skill-card" data-tone={index % 6}>
                <div className="updream-skill-card-content">
                    <div className="updream-skill-heading">
                        <span className="updream-skill-mark">
                            <Zap className="updream-skill-mark-icon" aria-hidden="true" />
                        </span>
                        <div className="updream-skill-meta">
                            <h3 className="updream-skill-title">{skill.name}</h3>
                            <p className="updream-skill-author">
                                <UserRound className="updream-skill-author-icon" aria-hidden="true" />
                                {siteName}官方
                            </p>
                        </div>
                    </div>
                    <p className="updream-skill-description">{skill.description}</p>
                    <p className="updream-skill-version">
                        V{skill.version} · {skill.source_kind === "original" ? "平台原创" : "授权改编"}
                    </p>
                </div>
                {coverURL ? (
                    <img src={coverURL} alt={`${skill.name}封面`} loading="lazy" className="updream-skill-thumbnail" />
                ) : (
                    <span className="updream-skill-fallback" aria-hidden="true">
                        <Sparkles className="updream-skill-fallback-icon" />
                    </span>
                )}
            </article>
        </Link>
    );
}

type UpdreamSkillsSectionProps = {
    skills: PlatformSkill[];
    loading: boolean;
    error: string;
    onRetry: () => void;
};

export function UpdreamSkillsSection({ skills, loading, error, onRetry }: UpdreamSkillsSectionProps) {
    const { settings } = useSiteSettings();

    return (
        <section className="updream-skills">
            <div className="updream-skills-heading">
                <h2 className="updream-skills-title">官方精选技能</h2>
                <Link to="/skills" className="updream-skills-all">
                    查看全部
                    <ChevronRight className="updream-skills-all-icon" aria-hidden="true" />
                </Link>
            </div>
            {loading ? <p className="updream-skills-loading">正在加载平台技能…</p> : null}
            {!loading && error ? (
                <div className="updream-skills-error" role="alert">
                    <span className="updream-skills-error-text">{error}</span>
                    <button type="button" className="updream-skills-retry" onClick={onRetry}>
                        <RefreshCw className="updream-skills-retry-icon" aria-hidden="true" />
                        重试
                    </button>
                </div>
            ) : null}
            {!loading && !error ? (
                <div className="updream-skills-grid">
                    {skills.map((skill, index) => (
                        <FirstPartySkillCard key={skill.dir} skill={skill} siteName={settings.siteName} index={index} />
                    ))}
                </div>
            ) : null}
        </section>
    );
}

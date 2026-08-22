import { useEffect, useState } from "react";
import { BadgeCheck, ChevronRight, RefreshCw, Sparkles, Zap } from "lucide-react";
import { Link } from "react-router";

import { useSiteSettings } from "@/components/site/site-settings-provider";
import skill1 from "@/pages/home/updream/assets/skill-1.png";
import skill2 from "@/pages/home/updream/assets/skill-2.png";
import skill3 from "@/pages/home/updream/assets/skill-3.png";
import skill4 from "@/pages/home/updream/assets/skill-4.png";
import skill5 from "@/pages/home/updream/assets/skill-5.png";
import skill6 from "@/pages/home/updream/assets/skill-6.png";
import { listSkillsCatalog, skillImageUrl, type PlatformSkill } from "@/services/api/skills";

const HOME_SKILL_COVERS: Readonly<Record<string, string>> = {
    "screenplay-writer": skill1,
    "short-drama-director": skill2,
    "story-development": skill3,
    "storyboard-continuity-director": skill4,
    "commercial-film-director": skill5,
    "suspense-visual-director": skill6,
};

const CARD_GRADIENTS = [
    "linear-gradient(180deg, transparent 40%, rgba(86, 42, 128, 0.2) 100%), linear-gradient(115deg, #c067b5 0%, #b980b8 52%, #c67da7 100%)",
    "linear-gradient(180deg, transparent 36%, rgba(0, 84, 210, 0.78) 100%), linear-gradient(115deg, #5e9ae3 0%, #a1bfdb 56%, #afbfcc 100%)",
    "linear-gradient(180deg, transparent 38%, rgba(245, 102, 0, 0.74) 100%), linear-gradient(115deg, #e1ad84 0%, #f2b163 56%, #ddc690 100%)",
    "linear-gradient(180deg, transparent 38%, rgba(44, 77, 217, 0.52) 100%), linear-gradient(115deg, #788dec 0%, #95bbf9 56%, #9ec7fe 100%)",
    "linear-gradient(180deg, transparent 38%, rgba(213, 54, 91, 0.58) 100%), linear-gradient(115deg, #e0a7c3 0%, #ed96aa 56%, #f08da5 100%)",
    "linear-gradient(180deg, transparent 38%, rgba(39, 139, 255, 0.5) 100%), linear-gradient(115deg, #9dc7d6 0%, #84c5e8 56%, #79c2ee 100%)",
] as const;

function FirstPartySkillCard({ skill, siteName, index }: { skill: PlatformSkill; siteName: string; index: number }) {
    const coverURL = HOME_SKILL_COVERS[skill.dir] ?? skillImageUrl(skill.cover_url);
    return (
        <Link to="/skills" className="updream-skill-link block">
            <article
                className="updream-skill-card relative h-[180px] cursor-pointer overflow-hidden rounded-[16px] border border-white/10 p-6 pr-[176px] transition-transform duration-300 hover:-translate-y-1"
                style={{ background: CARD_GRADIENTS[index % CARD_GRADIENTS.length] }}
            >
                <div className="updream-skill-heading flex items-center gap-2.5">
                    <span className="updream-skill-mark flex h-11 w-11 shrink-0 items-center justify-center rounded-[10px] border border-white/30 bg-white/20 text-white backdrop-blur-sm">
                        <Zap className="updream-skill-mark-icon size-5" />
                    </span>
                    <div className="updream-skill-meta min-w-0">
                        <h3 className="updream-skill-title truncate text-base font-semibold leading-6 text-white">{skill.name}</h3>
                        <p className="updream-skill-author flex items-center gap-1 text-xs leading-4 text-white/70">
                            <BadgeCheck className="updream-skill-verified size-3.5" />
                            {siteName} 官方
                        </p>
                    </div>
                </div>
                <p className="updream-skill-description mt-4 line-clamp-2 text-xs leading-4 text-white/70">{skill.description}</p>
                <p className="updream-skill-version absolute bottom-6 left-6 text-xs leading-4 text-white/70">
                    V{skill.version} · {skill.source_kind === "original" ? "平台原创" : "授权改编"}
                </p>
                {coverURL ? (
                    <img src={coverURL} alt="" loading="lazy" className="updream-skill-thumbnail absolute right-6 top-6 h-[171px] w-32 rounded-xl object-cover shadow-[0_8px_24px_rgba(0,0,0,0.26)]" />
                ) : (
                    <span className="updream-skill-fallback absolute right-6 top-6 flex h-[171px] w-32 items-center justify-center rounded-xl bg-white/12 text-white/80 shadow-[0_8px_24px_rgba(0,0,0,0.18)] backdrop-blur-sm" aria-hidden="true">
                        <Sparkles className="updream-skill-fallback-icon size-9" />
                    </span>
                )}
            </article>
        </Link>
    );
}

export function UpdreamSkillsSection() {
    const { settings } = useSiteSettings();
    const [skills, setSkills] = useState<PlatformSkill[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [reloadKey, setReloadKey] = useState(0);

    useEffect(() => {
        let cancelled = false;
        setLoading(true);
        setError("");
        listSkillsCatalog({ page: 1, page_size: 6 })
            .then((catalog) => {
                if (!cancelled) setSkills(catalog.skills);
            })
            .catch((reason: unknown) => {
                if (!cancelled) setError(reason instanceof Error ? reason.message : "技能目录加载失败");
            })
            .finally(() => {
                if (!cancelled) setLoading(false);
            });
        return () => {
            cancelled = true;
        };
    }, [reloadKey]);

    return (
        <section className="updream-skills mx-auto w-full max-w-[1408px] px-4 pb-24 sm:px-8">
            <div className="updream-skills-heading mb-5 flex items-center justify-between">
                <h2 className="updream-skills-title text-[20px] font-semibold text-white">官方精选技能</h2>
                <Link to="/skills" className="updream-skills-all flex items-center gap-0.5 text-[13px] text-white/45 transition-colors hover:text-white/80">
                    查看全部
                    <ChevronRight className="updream-skills-all-icon size-3.5" />
                </Link>
            </div>
            {loading ? <p className="updream-skills-loading py-8 text-sm text-white/55">正在加载平台技能…</p> : null}
            {!loading && error ? (
                <div className="updream-skills-error flex items-center gap-3 py-8 text-sm text-white/65" role="alert">
                    <span className="updream-skills-error-text">{error}</span>
                    <button type="button" className="updream-skills-retry inline-flex items-center gap-1 text-white hover:text-white/75" onClick={() => setReloadKey((value) => value + 1)}>
                        <RefreshCw className="updream-skills-retry-icon size-3.5" />
                        重试
                    </button>
                </div>
            ) : null}
            {!loading && !error ? (
                <div className="updream-skills-grid grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
                    {skills.map((skill, index) => (
                        <FirstPartySkillCard key={skill.dir} skill={skill} siteName={settings.siteName} index={index} />
                    ))}
                </div>
            ) : null}
        </section>
    );
}

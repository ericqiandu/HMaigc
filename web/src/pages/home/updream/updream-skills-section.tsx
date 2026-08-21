import { useEffect, useState } from "react";
import { BadgeCheck, ChevronRight, RefreshCw, Sparkles } from "lucide-react";
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
    "linear-gradient(115deg, #3447c8 0%, #6675e0 55%, #8994eb 100%)",
    "linear-gradient(115deg, #16697a 0%, #2a8998 55%, #63aeba 100%)",
    "linear-gradient(115deg, #70409b 0%, #9365b8 55%, #b28dca 100%)",
    "linear-gradient(115deg, #9b542f 0%, #ba7650 55%, #d29a77 100%)",
] as const;

function FirstPartySkillCard({ skill, siteName, index }: { skill: PlatformSkill; siteName: string; index: number }) {
    const coverURL = HOME_SKILL_COVERS[skill.dir] ?? skillImageUrl(skill.cover_url);
    return (
        <Link to="/skills" className="updream-skill-link block">
            <article
                className="updream-skill-card relative h-[176px] cursor-pointer overflow-hidden rounded-[18px] p-5 pr-[152px] transition-transform duration-300 hover:-translate-y-1"
                style={{ background: CARD_GRADIENTS[index % CARD_GRADIENTS.length] }}
            >
                <div className="updream-skill-heading flex items-center gap-2.5">
                    <span className="updream-skill-mark flex h-9 w-9 shrink-0 items-center justify-center rounded-[10px] border border-white/30 bg-white/20 text-white backdrop-blur-sm">
                        <Sparkles className="updream-skill-mark-icon size-4" />
                    </span>
                    <div className="updream-skill-meta min-w-0">
                        <h3 className="updream-skill-title truncate text-[15px] font-semibold leading-5 text-white">{skill.name}</h3>
                        <p className="updream-skill-author mt-0.5 flex items-center gap-1 text-[11px] text-white/75">
                            <BadgeCheck className="updream-skill-verified size-3" />
                            {siteName} 官方
                        </p>
                    </div>
                </div>
                <p className="updream-skill-description mt-3 line-clamp-2 text-[12px] leading-[1.6] text-white/85">{skill.description}</p>
                <p className="updream-skill-version absolute bottom-4 left-5 text-[11px] text-white/75">
                    V{skill.version} · {skill.source_kind === "original" ? "平台原创" : "授权改编"}
                </p>
                {coverURL ? (
                    <img src={coverURL} alt="" loading="lazy" className="updream-skill-thumbnail absolute right-5 top-5 h-[136px] w-[108px] rounded-xl object-cover shadow-[0_8px_24px_rgba(0,0,0,0.35)]" />
                ) : (
                    <span className="updream-skill-fallback absolute right-5 top-5 flex h-[136px] w-[108px] items-center justify-center rounded-xl bg-white/12 text-white/80 shadow-[0_8px_24px_rgba(0,0,0,0.18)] backdrop-blur-sm" aria-hidden="true">
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
                <h2 className="updream-skills-title text-[20px] font-semibold text-white">官方导演技能</h2>
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
                <div className="updream-skills-grid grid grid-cols-1 gap-x-6 gap-y-5 md:grid-cols-2 xl:grid-cols-3">
                    {skills.map((skill, index) => (
                        <FirstPartySkillCard key={skill.dir} skill={skill} siteName={settings.siteName} index={index} />
                    ))}
                </div>
            ) : null}
        </section>
    );
}

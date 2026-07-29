import { useEffect, useMemo, useState } from "react";
import { Check, ExternalLink, LoaderCircle, Search, Wrench } from "lucide-react";
import { Link } from "react-router";

import { cn } from "@/lib/utils";
import {
    listActivatedSkills,
    listCommunitySkills,
    listFavoriteSkills,
    type UpdreamSkill,
} from "@/services/api/skills";
import { toCanvasAgentSkillSelection } from "@/lib/canvas/canvas-agent-composer-context";
import type { CanvasAgentSkillSelection } from "@/types/canvas";

type SkillScope = "general" | "favorites" | "mine";

const scopeOptions: Array<{ value: SkillScope; label: string }> = [
    { value: "general", label: "通用" },
    { value: "favorites", label: "收藏" },
    { value: "mine", label: "我的" },
];

export function CanvasAgentSkillMenu({
    selectedSkills,
    onChange,
}: {
    selectedSkills: CanvasAgentSkillSelection[];
    onChange: (skills: CanvasAgentSkillSelection[]) => void;
}) {
    const [scope, setScope] = useState<SkillScope>("general");
    const [query, setQuery] = useState("");
    const [skills, setSkills] = useState<UpdreamSkill[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");

    useEffect(() => {
        let active = true;
        setLoading(true);
        setError("");
        loadSkills(scope)
            .then((items) => {
                if (active) setSkills(items);
            })
            .catch((reason: unknown) => {
                if (!active) return;
                setSkills([]);
                setError(reason instanceof Error ? reason.message : "Skills 加载失败");
            })
            .finally(() => {
                if (active) setLoading(false);
            });
        return () => {
            active = false;
        };
    }, [scope]);

    const selectedDirs = useMemo(() => new Set(selectedSkills.map((skill) => skill.dir)), [selectedSkills]);
    const visibleSkills = useMemo(() => {
        const normalized = query.trim().toLocaleLowerCase();
        if (!normalized) return skills;
        return skills.filter((skill) => `${skill.name} ${skill.description}`.toLocaleLowerCase().includes(normalized));
    }, [query, skills]);

    const toggleSkill = (skill: UpdreamSkill) => {
        onChange(
            selectedDirs.has(skill.dir)
                ? selectedSkills.filter((item) => item.dir !== skill.dir)
                : [...selectedSkills, toCanvasAgentSkillSelection(skill)],
        );
    };

    return (
        <section className="canvas-agent-picker canvas-agent-skill-menu" aria-label="Skills">
            <header className="canvas-agent-skill-header">
                <h3 className="canvas-agent-picker-title">Skill</h3>
                <Link className="canvas-agent-skill-manage" to="/skills">
                    全部
                    <ExternalLink className="canvas-agent-skill-manage-icon" aria-hidden="true" />
                </Link>
            </header>
            <div className="canvas-agent-skill-toolbar">
                <div className="canvas-agent-skill-tabs" role="tablist" aria-label="Skill 范围">
                    {scopeOptions.map((option) => (
                        <button
                            key={option.value}
                            type="button"
                            role="tab"
                            aria-selected={scope === option.value}
                            className={cn("canvas-agent-skill-tab", scope === option.value && "canvas-agent-skill-tab--active")}
                            onClick={() => setScope(option.value)}
                        >
                            {option.label}
                        </button>
                    ))}
                </div>
                <label className="canvas-agent-skill-search">
                    <Search className="canvas-agent-skill-search-icon" aria-hidden="true" />
                    <input
                        className="canvas-agent-skill-search-input"
                        value={query}
                        onChange={(event) => setQuery(event.target.value)}
                        placeholder="搜索 Skill"
                        aria-label="搜索 Skill"
                    />
                </label>
            </div>
            <div className="canvas-agent-skill-list thin-scrollbar">
                {loading ? (
                    <div className="canvas-agent-picker-state" role="status">
                        <LoaderCircle className="canvas-agent-picker-state-icon canvas-agent-picker-state-icon--loading" aria-hidden="true" />
                        正在加载 Skills
                    </div>
                ) : error ? (
                    <div className="canvas-agent-picker-state" role="alert">
                        <span className="canvas-agent-picker-state-text">{error}</span>
                    </div>
                ) : visibleSkills.length ? (
                    visibleSkills.map((skill) => {
                        const selected = selectedDirs.has(skill.dir);
                        return (
                            <button
                                key={skill.dir}
                                type="button"
                                className={cn("canvas-agent-skill-row", selected && "canvas-agent-skill-row--selected")}
                                aria-pressed={selected}
                                onClick={() => toggleSkill(skill)}
                            >
                                <span className="canvas-agent-skill-icon">
                                    <Wrench className="canvas-agent-skill-icon-glyph" aria-hidden="true" />
                                </span>
                                <span className="canvas-agent-skill-copy">
                                    <span className="canvas-agent-skill-name-line">
                                        <span className="canvas-agent-skill-name">{skill.name}</span>
                                        <span className="canvas-agent-skill-command">/{skill.dir}</span>
                                    </span>
                                    <span className="canvas-agent-skill-description">{skill.description}</span>
                                </span>
                                {selected ? <Check className="canvas-agent-skill-check" aria-hidden="true" /> : null}
                            </button>
                        );
                    })
                ) : (
                    <div className="canvas-agent-picker-empty" role="status">
                        暂无匹配的 Skill。
                    </div>
                )}
            </div>
        </section>
    );
}

async function loadSkills(scope: SkillScope) {
    if (scope === "favorites") return (await listFavoriteSkills()).skills;
    if (scope === "mine") return (await listActivatedSkills()).skills;
    return (await listCommunitySkills({ page: 1, page_size: 60, sort: "hot" })).skills;
}

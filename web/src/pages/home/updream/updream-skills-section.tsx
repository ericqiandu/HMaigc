import { BadgeCheck, ChevronRight, Zap } from "lucide-react";
import { Link } from "react-router";

import { useSiteSettings } from "@/components/site/site-settings-provider";
import skill1 from "@/pages/home/updream/assets/skill-1.png";
import skill2 from "@/pages/home/updream/assets/skill-2.png";
import skill3 from "@/pages/home/updream/assets/skill-3.png";
import skill4 from "@/pages/home/updream/assets/skill-4.png";
import skill5 from "@/pages/home/updream/assets/skill-5.png";
import skill6 from "@/pages/home/updream/assets/skill-6.png";

interface Skill {
    title: string;
    author: string;
    description: string;
    uses: string;
    thumbnail: string;
}

const SKILLS: readonly Skill[] = [
    {
        title: "剧本策划助手",
        author: "官方",
        description: "对剧本、故事梗概、角色设定或原始想法做诊断、策划、结构优化、大纲扩写和正文改写。",
        uses: "3.4 k",
        thumbnail: skill1,
    },
    {
        title: "剧本转视频提示词",
        author: "官方",
        description: "将完整剧本的文本一键转换为严格时间码视频提示词。",
        uses: "1.3 k",
        thumbnail: skill2,
    },
    {
        title: "剧本直出美术资产",
        author: "官方",
        description: "将剧本、小说或故事文本拆解为资产清单、分场美术演变表（Markdown 表格）和固定美学提示词。",
        uses: "662",
        thumbnail: skill3,
    },
    {
        title: "分镜光影设计",
        author: "官方",
        description: "为分镜图、分镜视频、分镜脚本、剧本或故事梗概建立统一光影逻辑。",
        uses: "263",
        thumbnail: skill4,
    },
    {
        title: "人物多视角生成",
        author: "官方",
        description: "为人物角色规划特殊视角、环视机位、视角迁移 prompt，并在需要时生成一致性视角图。",
        uses: "2.3 k",
        thumbnail: skill5,
    },
    {
        title: "外部模型直连器",
        author: "杨浦吴彦祖",
        description: "由平台管理员统一接入并维护图片、视频与文本模型，用户无需管理 API 密钥，可直接在创作流程中使用已启用的系统模型。",
        uses: "249",
        thumbnail: skill6,
    },
] as const;

function UpdreamSkillCard({ skill, siteName }: { skill: Skill; siteName: string }) {
    return (
        <Link to="/skills" className="updream-skill-link block">
            <article className="updream-skill-card relative cursor-pointer">
                <div className="updream-skill-heading flex items-center gap-2.5">
                    <span className="updream-skill-mark flex shrink-0 items-center justify-center">
                        <Zap className="updream-skill-mark-icon size-4 fill-current" />
                    </span>
                    <div className="updream-skill-meta min-w-0">
                        <h3 className="updream-skill-title truncate">
                            {skill.title}
                        </h3>
                        <p className="updream-skill-author flex items-center gap-1">
                            <BadgeCheck className="updream-skill-verified size-3" />
                            {skill.author === "官方" ? `${siteName} 官方` : skill.author}
                        </p>
                    </div>
                </div>
                <p className="updream-skill-description line-clamp-2">
                    {skill.description}
                </p>
                <p className="updream-skill-uses absolute flex items-center gap-1.5">
                    使用次数
                    <Zap className="updream-skill-uses-icon size-[11px] fill-current" />
                    {skill.uses}
                </p>
                <img
                    src={skill.thumbnail}
                    alt={skill.title}
                    className="updream-skill-thumbnail absolute object-cover"
                />
            </article>
        </Link>
    );
}

export function UpdreamSkillsSection() {
    const { settings } = useSiteSettings();

    return (
        <section className="updream-skills mx-auto w-full">
            <div className="updream-skills-heading flex items-center justify-between">
                <h2 className="updream-skills-title">官方精选技能</h2>
                <Link to="/skills" className="updream-skills-all flex items-center gap-0.5 transition-colors">
                    查看全部
                    <ChevronRight className="updream-skills-all-icon size-3.5" />
                </Link>
            </div>
            <div className="updream-skills-grid grid">
                {SKILLS.map((skill) => (
                    <UpdreamSkillCard key={skill.title} skill={skill} siteName={settings.siteName} />
                ))}
            </div>
        </section>
    );
}

import type { MembershipStorefrontSetting } from "@/services/api/membership";

type MembershipStorefrontGenerationProps = {
    presentation: MembershipStorefrontSetting;
};

export function MembershipStorefrontGeneration({ presentation }: MembershipStorefrontGenerationProps) {
    return (
        <section aria-labelledby="membership-generation-heading" className="membership-storefront-generation mx-auto mt-16 max-w-[1300px] px-6">
            <h2 className="membership-storefront-generation-title text-center text-[24px] font-bold text-white" id="membership-generation-heading">{presentation.copy.generationHeading}</h2>
            <div className="membership-storefront-generation-scroll mt-8 overflow-x-auto rounded-lg">
                <table className="membership-storefront-generation-table w-full min-w-[1000px] border-collapse">
                    <thead className="membership-storefront-generation-head">
                        <tr className="membership-storefront-generation-header-row bg-[#171e28]">
                            <th aria-label="模型" className="membership-storefront-generation-model-header rounded-tl-lg py-3.5 pl-4 text-left text-[13px] font-normal text-[#8b95a5]" />
                            {presentation.generationColumns.map((column, index) => (
                                <th className={`membership-storefront-generation-column py-3.5 text-center text-[13px] font-normal text-[#aeb8c5] ${index === presentation.generationColumns.length - 1 ? "rounded-tr-lg" : ""}`} key={column.key} scope="col">{column.label}</th>
                            ))}
                        </tr>
                    </thead>
                    <tbody className="membership-storefront-generation-body">
                        {presentation.generationSections.map((section) => (
                            <GenerationSection columnCount={presentation.generationColumns.length} key={section.title} section={section} />
                        ))}
                    </tbody>
                </table>
            </div>
            <p className="membership-storefront-generation-footnote mt-4 text-[12px] text-[#6b7684]">*{presentation.generationFootnote}</p>
        </section>
    );
}

type GenerationSectionProps = {
    columnCount: number;
    section: MembershipStorefrontSetting["generationSections"][number];
};

function GenerationSection({ columnCount, section }: GenerationSectionProps) {
    return (
        <>
            <tr className="membership-storefront-generation-section-row">
                <th className="membership-storefront-generation-section-title pb-1 pt-5 text-left text-[13px] font-normal text-[#8b95a5]" colSpan={columnCount + 1} scope="colgroup">{section.title}</th>
            </tr>
            {section.rows.map((row) => (
                <tr className="membership-storefront-generation-data-row border-b border-[#1c242f] last:border-0" key={`${section.title}-${row.model}`}>
                    <th className="membership-storefront-generation-model whitespace-nowrap py-4 pr-4 text-left font-normal" scope="row">
                        <span aria-hidden="true" className="membership-storefront-generation-model-icon mr-2 inline-flex h-6 w-6 items-center justify-center rounded-full bg-[#1d2530] text-[12px] text-[#c9d2dd]">{row.icon}</span>
                        <span className="membership-storefront-generation-model-name text-[14px] text-[#dde4ec]">{row.model}</span>
                    </th>
                    {row.values.map((value, index) => (
                        <td className="membership-storefront-generation-value py-4 text-center" key={`${row.model}-${index}`}>
                            <strong className="membership-storefront-generation-number text-[15px] font-bold text-white">{value}</strong>
                            <span className="membership-storefront-generation-unit ml-1 text-[12px] font-normal text-[#8b95a5]">{row.unit}</span>
                        </td>
                    ))}
                </tr>
            ))}
        </>
    );
}

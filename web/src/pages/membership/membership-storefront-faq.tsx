import { useState } from "react";

import type { MembershipStorefrontFAQ } from "@/services/api/membership";

type MembershipStorefrontFAQProps = {
    heading: string;
    items: MembershipStorefrontFAQ[];
};

export function MembershipStorefrontFAQs({ heading, items }: MembershipStorefrontFAQProps) {
    const [openIndex, setOpenIndex] = useState<number | null>(null);
    return (
        <section aria-labelledby="membership-faq-heading" className="membership-storefront-faq mx-auto mt-20 max-w-[900px] px-6 pb-16">
            <h2 className="membership-storefront-faq-title text-center text-[24px] font-bold text-white" id="membership-faq-heading">{heading}</h2>
            <div className="membership-storefront-faq-list mt-10">
                {items.map((item, index) => {
                    const open = openIndex === index;
                    return (
                        <article className="membership-storefront-faq-item border-b border-[#1c242f]" key={item.question}>
                            <h3 className="membership-storefront-faq-question-heading">
                                <button aria-expanded={open} className="membership-storefront-faq-question flex min-h-14 w-full items-center justify-between py-5 text-left" onClick={() => setOpenIndex(open ? null : index)} type="button">
                                    <span className="membership-storefront-faq-question-text text-[15px] font-medium text-[#dde4ec]">{item.question}</span>
                                    <svg aria-hidden="true" className={`membership-storefront-faq-chevron h-3.5 w-3.5 shrink-0 text-[#7d8794] transition-transform duration-200 ${open ? "rotate-180" : ""}`} fill="none" viewBox="0 0 14 14">
                                        <path className="membership-storefront-faq-chevron-path" d="M3 5.2L7 9.2l4-4" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.6" />
                                    </svg>
                                </button>
                            </h3>
                            <div className={`membership-storefront-faq-answer-grid grid transition-all duration-200 ${open ? "grid-rows-[1fr] pb-5 opacity-100" : "grid-rows-[0fr] opacity-0"}`}>
                                <div className="membership-storefront-faq-answer-overflow overflow-hidden">
                                    <p className="membership-storefront-faq-answer text-[13px] leading-7 text-[#8b95a5]">{item.answer}</p>
                                </div>
                            </div>
                        </article>
                    );
                })}
            </div>
        </section>
    );
}

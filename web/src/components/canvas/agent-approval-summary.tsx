import { formatCredits } from "@/constant/credits";
import type { AgentPendingApproval } from "@/services/api/agent-runtime";

export function AgentApprovalSummary({ approval }: { approval: AgentPendingApproval }) {
    if (!approval.quote) return null;
    return (
        <section className="canvas-agent-runtime-approval-summary" aria-label="冻结生成费用">
            <div className="canvas-agent-runtime-approval-cost">
                <span className="canvas-agent-runtime-approval-cost-label">预计扣除</span>
                <strong className="canvas-agent-runtime-approval-cost-value">{formatCredits(approval.quote.amountMicrocredits)} 积分</strong>
            </div>
            <dl className="canvas-agent-runtime-approval-facts">
                <div className="canvas-agent-runtime-approval-fact">
                    <dt className="canvas-agent-runtime-approval-fact-label">模型</dt>
                    <dd className="canvas-agent-runtime-approval-fact-value">{approval.quote.modelKey}</dd>
                </div>
                <div className="canvas-agent-runtime-approval-fact">
                    <dt className="canvas-agent-runtime-approval-fact-label">价格版本</dt>
                    <dd className="canvas-agent-runtime-approval-fact-value">{approval.quote.priceVersion}</dd>
                </div>
            </dl>
        </section>
    );
}

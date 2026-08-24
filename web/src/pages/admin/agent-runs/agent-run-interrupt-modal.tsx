import { Alert, Button, Input, Modal } from "antd";
import { CircleAlert } from "lucide-react";

import { describeAgentRunControl, describeAgentRunFacts } from "./agent-run-presenters";
import type { AgentRunInterruptDraft } from "./agent-run-page-state";

type AgentRunInterruptModalProps = {
    draft: AgentRunInterruptDraft | null;
    onChange: (draft: AgentRunInterruptDraft) => void;
    onCancel: () => void;
    onSubmit: () => void;
};

export function AgentRunInterruptModal({ draft, onChange, onCancel, onSubmit }: AgentRunInterruptModalProps) {
    const presentation = draft ? describeAgentRunControl(draft.run) : null;
    const reasonLength = draft ? Array.from(draft.reason).length : 0;
    const confirmationPhrase = draft?.run.confirmationPhrase ?? "";
    const canSubmit = Boolean(
        draft &&
            presentation?.canInterrupt &&
            reasonLength >= 4 &&
            reasonLength <= 500 &&
            confirmationPhrase &&
            draft.confirmation === confirmationPhrase,
    );

    return (
        <Modal
            className="admin-operation-modal agent-run-interrupt-modal workspace-ui-scope"
            title={presentation?.title ?? "终止 Agent 运行"}
            open={Boolean(draft)}
            footer={null}
            closable={!draft?.submitting}
            keyboard={!draft?.submitting}
            maskClosable={!draft?.submitting}
            onCancel={onCancel}
            destroyOnHidden
        >
            {draft && presentation ? (
                <div className="agent-run-interrupt-content">
                    <Alert
                        className="agent-run-interrupt-alert"
                        type={presentation.canInterrupt ? "warning" : "error"}
                        showIcon
                        message={presentation.description}
                    />
                    <dl className="agent-run-interrupt-facts" aria-label="本次终止的真实影响">
                        {describeAgentRunFacts(draft.run).map((fact) => (
                            <div className="agent-run-interrupt-fact" key={fact.label}>
                                <dt className="agent-run-interrupt-fact-label">{fact.label}</dt>
                                <dd className="agent-run-interrupt-fact-value">{fact.value}</dd>
                            </div>
                        ))}
                    </dl>
                    {draft.error ? (
                        <div className="agent-run-interrupt-error" role="alert">
                            <CircleAlert className="agent-run-interrupt-error-icon size-4" aria-hidden="true" />
                            <span className="agent-run-interrupt-error-text">{draft.error}</span>
                        </div>
                    ) : null}
                    <div className="agent-run-interrupt-field">
                        <label className="agent-run-interrupt-label" htmlFor="agent-run-interrupt-reason">
                            终止原因
                        </label>
                        <Input.TextArea
                            className="agent-run-interrupt-reason"
                            id="agent-run-interrupt-reason"
                            value={draft.reason}
                            maxLength={500}
                            showCount
                            autoSize={{ minRows: 3, maxRows: 6 }}
                            disabled={draft.submitting || !presentation.canInterrupt}
                            placeholder="说明可审计的终止原因（4–500 个字符）"
                            onChange={(event) => onChange({ ...draft, reason: event.target.value, error: "" })}
                        />
                    </div>
                    <div className="agent-run-interrupt-field">
                        <label className="agent-run-interrupt-label" htmlFor="agent-run-interrupt-confirmation">
                            输入确认短语
                        </label>
                        <Input
                            className="agent-run-interrupt-confirmation"
                            id="agent-run-interrupt-confirmation"
                            autoComplete="off"
                            value={draft.confirmation}
                            disabled={draft.submitting || !presentation.canInterrupt}
                            placeholder={confirmationPhrase || "等待运行详情"}
                            onChange={(event) => onChange({ ...draft, confirmation: event.target.value, error: "" })}
                        />
                        <p className="agent-run-interrupt-hint">
                            必须完整输入：<code className="agent-run-interrupt-code">{confirmationPhrase || "未提供"}</code>
                        </p>
                    </div>
                    <div className="agent-run-interrupt-actions">
                        <Button className="agent-run-interrupt-cancel" disabled={draft.submitting} onClick={onCancel}>
                            取消
                        </Button>
                        <Button
                            className="agent-run-interrupt-submit"
                            type="primary"
                            danger
                            loading={draft.submitting}
                            disabled={!canSubmit}
                            onClick={onSubmit}
                        >
                            确认终止
                        </Button>
                    </div>
                </div>
            ) : null}
        </Modal>
    );
}

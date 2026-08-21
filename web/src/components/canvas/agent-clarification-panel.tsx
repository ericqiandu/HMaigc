import { ChevronLeft, ChevronRight, MessageCircleQuestion } from "lucide-react";
import { useEffect, useMemo, useState, type KeyboardEvent } from "react";

import type {
    AgentClarificationAnswer,
    AgentClarificationAnswerInput,
    AgentClarificationQuestion,
    AgentCompletedClarification,
    AgentPendingClarification,
} from "@/services/api/agent-runtime";

type AgentClarificationPanelProps = Readonly<{
    pending: AgentPendingClarification;
    history: readonly AgentCompletedClarification[];
    busy: boolean;
    error: string;
    onRespond: (input: { requestId: string; questionId: string; answer: AgentClarificationAnswerInput; complete: boolean }) => Promise<unknown>;
}>;

type DraftMap = Record<string, AgentClarificationAnswerInput>;

export function AgentClarificationPanel({ pending, history, busy, error, onRespond }: AgentClarificationPanelProps) {
    const questions = pending.request.questions;
    const [drafts, setDrafts] = useState<DraftMap>(() => savedDrafts(pending.answers));
    const [activeIndex, setActiveIndex] = useState(() => firstUnansweredIndex(questions, pending.answers));
    const [historyOpen, setHistoryOpen] = useState(false);

    useEffect(() => {
        setDrafts(savedDrafts(pending.answers));
        setActiveIndex(firstUnansweredIndex(pending.request.questions, pending.answers));
    }, [pending]);

    const question = questions[activeIndex];
    const draft = question ? drafts[question.id] ?? emptyAnswer() : emptyAnswer();
    const persistedDrafts = useMemo(() => savedDrafts(pending.answers), [pending.answers]);
    const lastQuestion = activeIndex === questions.length - 1;
    const currentAnswerSaved = Boolean(question && sameAnswer(draft, persistedDrafts[question.id]));
    const complete = lastQuestion && questions.every((item) => answerValid(item, item.id === question?.id ? draft : persistedDrafts[item.id]));
    const canSubmit = Boolean(question && answerValid(question, draft) && (!lastQuestion || complete));

    const selected = useMemo(() => new Set(draft.selectedOptionIds), [draft.selectedOptionIds]);

    const updateDraft = (next: AgentClarificationAnswerInput) => {
        if (!question) return;
        setDrafts((current) => ({ ...current, [question.id]: next }));
    };

    const submit = async (answer: AgentClarificationAnswerInput) => {
        if (!question || busy) return;
        try {
            await onRespond({ requestId: pending.request.requestId, questionId: question.id, answer, complete: lastQuestion && questions.every((item) => answerValid(item, item.id === question.id ? answer : persistedDrafts[item.id])) });
        } catch {
            // The parent owns the visible error. Keeping the draft is the required failure behavior.
        }
    };

    const toggleOption = (optionId: string) => {
        if (!question) return;
        const nextSelection = question.type === "single_choice"
            ? [optionId]
            : selected.has(optionId)
              ? draft.selectedOptionIds.filter((id) => id !== optionId)
              : [...draft.selectedOptionIds, optionId];
        updateDraft({
            ...draft,
            selectedOptionIds: nextSelection,
            customText: question.type === "single_choice" ? "" : draft.customText,
            skipped: false,
        });
    };

    const updateCustomText = (customText: string) => {
        if (!question) return;
        updateDraft({
            ...draft,
            selectedOptionIds: question.type === "single_choice" ? [] : draft.selectedOptionIds,
            customText,
            skipped: false,
        });
    };

    const handleInputKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key !== "Enter" || event.shiftKey) return;
        event.preventDefault();
        if (canSubmit) void submit(draft);
    };

    if (!question) return null;

    return (
        <section className="agent-clarification-section" aria-label="Agent 询问">
            {history.length > 0 ? <AgentClarificationHistory history={history} open={historyOpen} onOpenChange={setHistoryOpen} /> : null}
            <div className={`agent-clarification-card${busy ? " is-busy" : ""}`} aria-busy={busy}>
                <header className="agent-clarification-card-header">
                    <h3 className="agent-clarification-question">{question.prompt}</h3>
                    <div className="agent-clarification-nav" aria-label="问题进度">
                        <button className="agent-clarification-nav-button" type="button" aria-label="上一题" disabled={busy || activeIndex === 0} onClick={() => setActiveIndex((current) => Math.max(0, current - 1))}>
                            <ChevronLeft className="agent-clarification-nav-icon" aria-hidden="true" />
                        </button>
                        <span className="agent-clarification-position">{activeIndex + 1}/{questions.length}</span>
                        <button className="agent-clarification-nav-button" type="button" aria-label="下一题" disabled={busy || activeIndex === questions.length - 1 || !currentAnswerSaved} onClick={() => setActiveIndex((current) => Math.min(questions.length - 1, current + 1))}>
                            <ChevronRight className="agent-clarification-nav-icon" aria-hidden="true" />
                        </button>
                    </div>
                </header>

                <fieldset className="agent-clarification-fieldset">
                    <legend className="agent-clarification-legend">{question.prompt}</legend>
                    {question.type !== "free_text" ? (
                    <div className="agent-clarification-options" role={question.type === "single_choice" && !question.allowCustomAnswer ? "radiogroup" : "group"}>
                        {question.options.map((option, index) => {
                            const checked = selected.has(option.id);
                            return (
                                <button className={`agent-clarification-option${checked ? " is-selected" : ""}`} disabled={busy} type="button" key={option.id} role={question.type === "single_choice" ? "radio" : "checkbox"} aria-checked={checked} onClick={() => toggleOption(option.id)}>
                                    <span className="agent-clarification-option-index">{index + 1}</span>
                                    <span className="agent-clarification-option-label">{option.label}</span>
                                </button>
                            );
                        })}
                        {question.allowCustomAnswer ? (
                            <label className={`agent-clarification-option agent-clarification-custom-option${draft.customText.trim() ? " is-selected" : ""}`}>
                                <span className="agent-clarification-option-index">{question.options.length + 1}</span>
                                <textarea
                                    className="agent-clarification-custom-input"
                                    aria-label="填写其他答案"
                                    placeholder="其他答案，请描述..."
                                    value={draft.customText}
                                    rows={1}
                                    disabled={busy}
                                    onChange={(event) => updateCustomText(event.target.value)}
                                    onKeyDown={handleInputKeyDown}
                                />
                            </label>
                        ) : null}
                    </div>
                    ) : null}

                    {question.type === "free_text" ? (
                        <label className={`agent-clarification-option agent-clarification-custom-option agent-clarification-free-text-option${draft.customText.trim() ? " is-selected" : ""}`}>
                        <span className="agent-clarification-option-index">1</span>
                        <textarea
                            className="agent-clarification-custom-input"
                            aria-label="填写回答"
                            placeholder="输入你的回答"
                            value={draft.customText}
                            rows={1}
                            disabled={busy}
                            onChange={(event) => updateCustomText(event.target.value)}
                            onKeyDown={handleInputKeyDown}
                        />
                        </label>
                    ) : null}
                </fieldset>

                {error ? <p className="agent-clarification-error" role="alert">{error}</p> : null}
                <footer className="agent-clarification-card-footer">
                    <button className="agent-clarification-skip" type="button" disabled={busy} onClick={() => void submit({ selectedOptionIds: [], customText: "", skipped: true })}>忽略</button>
                    <button className="agent-clarification-submit" type="button" disabled={busy || !canSubmit} onClick={() => void submit(draft)}>{lastQuestion ? "提交" : "继续"}</button>
                </footer>
            </div>
        </section>
    );
}

export function AgentClarificationStatus() {
    return (
        <div className="agent-clarification-live" aria-live="polite">
            <MessageCircleQuestion className="agent-clarification-live-icon" aria-hidden="true" />
            询问中
        </div>
    );
}

export function AgentClarificationHistory({ history, open, onOpenChange }: Readonly<{ history: readonly AgentCompletedClarification[]; open: boolean; onOpenChange: (open: boolean) => void }>) {
    return (
        <div className="agent-clarification-history-wrap">
            <button className="agent-clarification-history-toggle" type="button" aria-expanded={open} onClick={() => onOpenChange(!open)}>
                已询问
                <span className="agent-clarification-history-count">{history.length}</span>
            </button>
            {open ? (
                <div className="agent-clarification-history">
                    {history.map((batch) => (
                        <section className="agent-clarification-history-batch" key={batch.request.requestId} aria-label="已完成询问">
                            {batch.request.questions.map((question) => {
                                const answer = batch.answers.find((item) => item.questionId === question.id);
                                return (
                                    <div className="agent-clarification-history-row" key={question.id}>
                                        <p className="agent-clarification-history-question">{question.prompt}</p>
                                        <p className="agent-clarification-history-answer">{answerLabel(question, answer)}</p>
                                    </div>
                                );
                            })}
                        </section>
                    ))}
                </div>
            ) : null}
        </div>
    );
}

function answerLabel(question: AgentClarificationQuestion, answer: AgentClarificationAnswer | undefined) {
    if (!answer) return "未回答";
    if (answer.skipped) return "已忽略";
    const labels = answer.selectedOptionIds.map((id) => question.options.find((option) => option.id === id)?.label).filter((label): label is string => Boolean(label));
    if (answer.customText) labels.push(answer.customText);
    return labels.join("、") || "未回答";
}

function savedDrafts(answers: readonly AgentClarificationAnswer[]): DraftMap {
    return Object.fromEntries(answers.map((answer) => [answer.questionId, { selectedOptionIds: [...answer.selectedOptionIds], customText: answer.customText, skipped: answer.skipped }]));
}

function firstUnansweredIndex(questions: readonly AgentClarificationQuestion[], answers: readonly AgentClarificationAnswer[]) {
    const answered = new Set(answers.map((answer) => answer.questionId));
    const index = questions.findIndex((question) => !answered.has(question.id));
    return index < 0 ? Math.max(0, questions.length - 1) : index;
}

function answerValid(question: AgentClarificationQuestion, answer: AgentClarificationAnswerInput | undefined) {
    if (!answer) return false;
    if (answer.skipped) return true;
    if (question.type === "free_text") return Boolean(answer.customText.trim());
    return answer.selectedOptionIds.length > 0 || (question.allowCustomAnswer && Boolean(answer.customText.trim()));
}

function emptyAnswer(): AgentClarificationAnswerInput {
    return { selectedOptionIds: [], customText: "", skipped: false };
}

function sameAnswer(left: AgentClarificationAnswerInput, right: AgentClarificationAnswerInput | undefined) {
    if (!right || left.customText !== right.customText || left.skipped !== right.skipped || left.selectedOptionIds.length !== right.selectedOptionIds.length) return false;
    return left.selectedOptionIds.every((optionId, index) => optionId === right.selectedOptionIds[index]);
}

import type { AgentExpectedDelivery } from "./agent-runtime";
import { array, boundedText, exactObject, flag, integer } from "./strict-contract";

export type AgentClarificationQuestionType = "single_choice" | "multi_choice" | "free_text";
export type AgentClarificationOption = Readonly<{ id: string; label: string }>;
export type AgentClarificationQuestion = Readonly<{
    id: string;
    prompt: string;
    type: AgentClarificationQuestionType;
    options: readonly AgentClarificationOption[];
    allowCustomAnswer: boolean;
}>;
export type AgentClarificationRequest = Readonly<{
    requestId: string;
    questions: readonly AgentClarificationQuestion[];
    expectedDelivery: AgentExpectedDelivery;
}>;
export type AgentClarificationAnswer = Readonly<{
    questionId: string;
    selectedOptionIds: readonly string[];
    customText: string;
    skipped: boolean;
}>;
export type AgentClarificationAnswerInput = Omit<AgentClarificationAnswer, "questionId">;
export type AgentPendingClarification = Readonly<{ request: AgentClarificationRequest; answers: readonly AgentClarificationAnswer[] }>;
export type AgentCompletedClarification = Readonly<{
    request: AgentClarificationRequest;
    answers: readonly AgentClarificationAnswer[];
    completionQuestionId: string;
    completionExpectedStateVersion: number;
}>;

type DeliveryParser = (value: unknown) => AgentExpectedDelivery;

export function parsePendingClarification(value: unknown, parseDelivery: DeliveryParser): AgentPendingClarification {
    const source = exactObject(value, "pendingClarification", ["request", "answers"]);
    const request = parseClarificationRequest(source.request, "pendingClarification.request", parseDelivery);
    const answers = parseClarificationAnswers(source.answers, request, "pendingClarification.answers");
    return { request, answers };
}

export function parseClarificationHistory(value: unknown, parseDelivery: DeliveryParser): AgentCompletedClarification[] {
    const requests = new Set<string>();
    return array(value, "clarificationHistory").map((item, index) => {
        const label = `clarificationHistory[${index}]`;
        const source = exactObject(item, label, ["request", "answers", "completionQuestionId", "completionExpectedStateVersion"]);
        const request = parseClarificationRequest(source.request, `${label}.request`, parseDelivery);
        if (requests.has(request.requestId)) throw new Error("Agent 追问历史身份重复");
        requests.add(request.requestId);
        const answers = parseClarificationAnswers(source.answers, request, `${label}.answers`);
        if (answers.length !== request.questions.length) throw new Error(`${label}.answers 不完整`);
        const completionQuestionId = boundedText(source.completionQuestionId, `${label}.completionQuestionId`, 120);
        if (!answers.some((answer) => answer.questionId === completionQuestionId)) throw new Error(`${label}.completionQuestionId 不属于回答事实`);
        return {
            request,
            answers,
            completionQuestionId,
            completionExpectedStateVersion: integer(source.completionExpectedStateVersion, `${label}.completionExpectedStateVersion`),
        };
    });
}

function parseClarificationRequest(value: unknown, label: string, parseDelivery: DeliveryParser): AgentClarificationRequest {
    const source = exactObject(value, label, ["requestId", "questions", "expectedDelivery"]);
    const questionsSource = array(source.questions, `${label}.questions`);
    if (questionsSource.length < 1 || questionsSource.length > 3) throw new Error(`${label}.questions 数量必须在 1 到 3 之间`);
    const ids = new Set<string>();
    const questions = questionsSource.map((item, index) => {
        const questionLabel = `${label}.questions[${index}]`;
        const question = exactObject(item, questionLabel, ["id", "prompt", "type", "options", "allowCustomAnswer"]);
        const id = boundedText(question.id, `${questionLabel}.id`, 120);
        if (ids.has(id)) throw new Error(`${label}.questions 存在重复问题身份`);
        ids.add(id);
        const type = clarificationQuestionType(question.type, `${questionLabel}.type`);
        const options = question.options === undefined ? [] : parseClarificationOptions(question.options, questionLabel);
        const allowCustomAnswer = question.allowCustomAnswer === undefined ? false : flag(question.allowCustomAnswer, `${questionLabel}.allowCustomAnswer`);
        if (type === "free_text" && (options.length !== 0 || allowCustomAnswer)) throw new Error(`${questionLabel} 自由文本问题不能携带选项`);
        if (type !== "free_text" && (options.length < 2 || options.length > 6)) throw new Error(`${questionLabel} 选择题选项数量必须在 2 到 6 之间`);
        return { id, prompt: boundedText(question.prompt, `${questionLabel}.prompt`, 240), type, options, allowCustomAnswer };
    });
    return { requestId: boundedText(source.requestId, `${label}.requestId`, 120), questions, expectedDelivery: parseDelivery(source.expectedDelivery) };
}

function parseClarificationOptions(value: unknown, questionLabel: string): AgentClarificationOption[] {
    const ids = new Set<string>();
    return array(value, `${questionLabel}.options`).map((item, index) => {
        const label = `${questionLabel}.options[${index}]`;
        const source = exactObject(item, label, ["id", "label"]);
        const id = boundedText(source.id, `${label}.id`, 120);
        if (ids.has(id)) throw new Error(`${questionLabel}.options 存在重复选项身份`);
        ids.add(id);
        return { id, label: boundedText(source.label, `${label}.label`, 80) };
    });
}

function parseClarificationAnswers(value: unknown, request: AgentClarificationRequest, label: string): AgentClarificationAnswer[] {
    const questions = new Map(request.questions.map((question) => [question.id, question]));
    const ids = new Set<string>();
    return array(value, label).map((item, index) => {
        const answerLabel = `${label}[${index}]`;
        const source = exactObject(item, answerLabel, ["questionId", "selectedOptionIds", "customText", "skipped"]);
        const questionId = boundedText(source.questionId, `${answerLabel}.questionId`, 120);
        const question = questions.get(questionId);
        if (!question) throw new Error(`${answerLabel} 引用了未知问题`);
        if (ids.has(questionId)) throw new Error(`${label} 存在重复问题回答`);
        ids.add(questionId);
        const selectedOptionIds = source.selectedOptionIds === undefined ? [] : array(source.selectedOptionIds, `${answerLabel}.selectedOptionIds`).map((id) => boundedText(id, `${answerLabel}.selectedOptionIds`, 120));
        if (new Set(selectedOptionIds).size !== selectedOptionIds.length) throw new Error(`${answerLabel} 包含重复选项`);
        const customText = source.customText === undefined ? "" : boundedText(source.customText, `${answerLabel}.customText`, 1000, true);
        const skipped = flag(source.skipped, `${answerLabel}.skipped`);
        validateClarificationAnswer(question, { questionId, selectedOptionIds, customText, skipped }, answerLabel);
        return { questionId, selectedOptionIds, customText, skipped };
    });
}

function validateClarificationAnswer(question: AgentClarificationQuestion, answer: AgentClarificationAnswer, label: string) {
    if (answer.skipped) {
        if (answer.selectedOptionIds.length || answer.customText) throw new Error(`${label} 忽略回答不能携带内容`);
        return;
    }
    if (question.type === "free_text") {
        if (answer.selectedOptionIds.length || !answer.customText.trim()) throw new Error(`${label} 自由文本回答无效`);
        return;
    }
    const optionIds = new Set(question.options.map((option) => option.id));
    if (answer.selectedOptionIds.some((id) => !optionIds.has(id))) throw new Error(`${label} 引用了未知选项`);
    if (question.type === "single_choice" && answer.selectedOptionIds.length > 1) throw new Error(`${label} 单选回答数量无效`);
    if (answer.customText && !question.allowCustomAnswer) throw new Error(`${label} 不允许自定义回答`);
    if (!answer.selectedOptionIds.length && !answer.customText.trim()) throw new Error(`${label} 选择题回答为空`);
}

function clarificationQuestionType(value: unknown, label: string): AgentClarificationQuestionType {
    if (value !== "single_choice" && value !== "multi_choice" && value !== "free_text") throw new Error(`${label} 问题类型无效`);
    return value;
}

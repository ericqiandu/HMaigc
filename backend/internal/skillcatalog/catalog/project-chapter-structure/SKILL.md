---
name: 项目章节结构化
description: 把章节正文提取为待确认角色候选或可编辑分镜，并原子写回项目。
---

# 项目章节结构化

只在用户已经从项目章节工作台明确发起“提取角色候选”或“生成章节分镜”时使用。输入必须包含真实的 `domainProjectId`、`baseRevision`、`unitId`、章节正文和用户选择的动作；任何必要事实缺失时先追问或显式失败，不得猜测项目身份、章节或版本。

## 执行契约

1. 读取输入中的项目、章节、现有角色和现有分镜事实，保持章节原意，不补写输入没有支持的剧情事实。
2. 最终交付必须是 `project_change`，`targetProjectId` 必须等于输入的 `domainProjectId`，完成条件必须包含 `project_revision`。
3. 写入前调用 `project.apply_ops`，逐字复用输入的 `domainProjectId` 与 `baseRevision`，使用本轮唯一且稳定的 `clientMutationId` 和 `operationId`。
4. `project.apply_ops` 是需要用户确认的不可变提案。未获得确认不得声称项目已经更新。
5. 工具成功后，以返回的 `committedRevision`、角色候选 ID 或镜头 ID 作为交付证据；工具失败或 revision 冲突时显式报告并停止，不得改走其他写入路径。

## 提取角色候选

- 使用 `create_character_candidates`，`unitId` 必须等于当前章节。
- 只提取在正文中实际作为角色出现的实体；现有已确认角色或同章待确认候选不得重复创建。
- `aliases` 只记录正文明确出现的称呼。
- `role`、`appearance`、`clothing`、`physique`、`personality`、`voiceLanguage`、`voiceAge` 与 `voiceTimbre` 必须区分“正文事实”和“本章未明确”。未知信息用明确的“本章未明确”表述，禁止伪造具体设定。
- `consistencyPrompt` 只包含正文支持的稳定身份特征和明确未知项；`multiViewPrompt` 描述后续三视图需要保持的同一身份，不得擅自补充品牌、演员或受版权保护的具体形象。
- `props` 可以为空；仅记录角色稳定持有或与身份直接相关的道具。

## 生成章节分镜

- 使用 `replace_unit_shots`，`unitId` 必须等于当前章节。
- 每个镜头必须有简洁标题、可执行的画面与动作描述，以及 1000–30000 毫秒的明确时长。
- 描述应保留出场角色身份、空间关系、动作、对白归属、镜头景别/机位/运动以及与前后镜头的连续性；不要把解释报告写进镜头内容。
- 覆盖已有分镜是用户明确动作，但仍必须等待 `project.apply_ops` 提案审批后才执行。
- 此阶段只写入可编辑分镜结构，不生成图片、视频，不伪造媒体 URL。

## 完成前自检

- 实际动作与用户明确选择一致。
- 项目、章节和 revision 与输入事实一致。
- 已成功执行 `project.apply_ops`，而不是只输出文本。
- 返回的项目 revision 是当前 revision，并满足 `project_change` 交付契约。

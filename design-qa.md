# Design QA — 模型运营展示

- Source visual truth: `C:\Users\nz999\AppData\Local\Temp\codex-clipboard-25e3da44-af24-4e9d-a003-b33371e44eac.png`
- Implementation screenshot: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-marketing-implementation-final-20260729.png`
- Admin screenshot: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-marketing-admin-20260729.png`
- Full comparison: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-marketing-full-comparison-final-20260729.png`
- Focused comparison: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-marketing-focused-comparison-final-20260729.png`
- Source pixels: `591 × 598`
- Implementation pixels / CSS viewport: `991 × 912`
- Device scale factor: `1`
- State: 深色画布、视频节点已选中、系统只配置了一个可用视频模型、当前用户无有效会员、模型选择弹层已展开。
- Density normalization: 完整对比图把实现按等高比例缩放；聚焦对比图使用参考模型行 `360 × 52px` 与实现模型行 `370 × 62px` 的原始像素裁切，不做密度插值。

## Full-view comparison evidence

参考图与实现都把模型入口放在提示词面板底部，并采用“渠道图标 → 模型名称 → 会员钻石 → 促销胶囊 → 右侧计费信息”的单行结构。实现不再在模型名称下显示渠道名或模型标识，保持与本次需求一致。参考图包含多个示例模型，而本地目录当前只有一个已配置视频模型，这是数据范围差异，不是布局缺失。

## Focused region comparison evidence

- 字体与排版：模型名称使用 14px 半粗体，名称、钻石和促销角标在同一基线；没有第二行说明挤占列表密度。
- 间距与布局：图标为 36px，行高 62px，钻石与名称相邻，促销胶囊保持紧凑；价格独立右对齐。
- 颜色与视觉 token：钻石和促销胶囊使用参考图的暖金色层级；会员模型虽不可点击，名称仍保持清晰对比度。
- 图像质量：会员钻石使用用户提供的真实 SVG 资产；模型图标继续使用后台上传的真实模型图标，不使用 CSS 或文本近似绘制。
- 文案与内容：列表只显示名称、会员身份、运营角标和计费信息；完整推广文案由后台字段提供并绑定到悬浮提示，不在名称下常驻显示。
- 权限状态：当前非会员用户的会员模型仍为 `aria-disabled="true"`，展示优化没有绕过服务端会员校验。

## Findings

没有剩余可执行的 P0、P1 或 P2 视觉问题。

可接受差异：

- 本地只配置一个视频模型，因此弹层没有参考图中的多行目录。
- 本地模型图标为后台上传内容，外观由运营配置决定，不在前端强制染色。
- 实现保留项目现有的单层弹层描边，符合仓库 `Design.md` 的容器层级要求。

## Comparison history

1. 初始对比发现 P2：移除第二行信息后，模型名称失去了既有 `canvas-model-picker-option-title` 视觉类；在会员禁用状态下继承了过暗颜色。
2. 修复：把标题视觉类恢复到新的单行标题容器，不改变权限、点击或计费逻辑。
3. 修复后证据：`model-marketing-focused-comparison-final-20260729.png` 中模型名称恢复到参考图的清晰白色层级，钻石与促销胶囊保持紧邻且没有产生第二行。

## Interaction evidence

- 后台编辑抽屉真实显示并保存“悬浮介绍文案”和“促销角标”，长度分别限制为 120 和 12 个字符。
- 保存纯展示配置后，模型计费版本保持 `v4`，未发生错误递增。
- 画布真实目录读取到后台配置的 `会员专享` 角标，会员钻石渲染为 `14 × 14px`，名称下方旧信息节点数量为 `0`。
- 推广文案通过 Ant Design `Tooltip` 绑定在完整模型选项上；当前内置浏览器控制接口不提供鼠标移动指令，因此验收以真实 DOM 绑定、后台持久化和浏览器渲染证据完成，没有伪造悬浮截图。

## Implementation checklist

- [x] 放大会员钻石
- [x] 删除模型名称下方常驻信息
- [x] 后台配置悬浮介绍文案
- [x] 后台配置促销胶囊角标
- [x] 画布模型目录消费后台动态字段
- [x] 会员权限保持服务端强校验
- [x] 展示配置不再错误递增计费版本
- [x] 后端测试、前端类型检查和生产构建通过

final result: passed

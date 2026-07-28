# Design QA — 模型列表双主题可读性与文案层级

- Source visual truth:
  - Light: `C:\Users\nz999\AppData\Local\Temp\codex-clipboard-2c8c189d-f939-464b-8122-200ddef4d69e.png`
  - Dark: `C:\Users\nz999\AppData\Local\Temp\codex-clipboard-cdf67a80-ae1c-4dcd-931e-263eab54c114.png`
- Browser-rendered implementation:
  - Light: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-list-inline-copy-light-20260729.png`
  - Dark: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-list-inline-copy-dark-20260729.png`
  - Unselected / copy hidden: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-list-unselected-copy-hidden-dark-20260729.jpg`
- Full comparison: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-list-inline-copy-full-comparison-20260729.png`
- Focused comparison: `E:\新版短剧制作\open-ai-canvas\.local\qa\model-list-inline-copy-focused-comparison-20260729.png`
- Source pixels: light `376 × 427`, dark `379 × 415`
- Implementation pixels / CSS viewport: `991 × 912`
- Device scale factor: `1`
- Density normalization: 完整对比按画布原始截图展示；聚焦对比将参考与实现的单个模型行放入同一画面等高核对，不把浏览器外框和画布空白当作设计差异。
- State: 画布模型选择弹层展开、当前用户为非会员、本地视频模型目录仅配置一个会员专属模型；分别核对选中项、普通未悬停项和普通悬停项的文案可见性契约。

## Full-view comparison evidence

实现保留画布节点、提示词面板和底部模型入口的既有结构，仅修正模型选项的内容层级和双主题令牌。参考图有多行模型，而本地目录当前只有一个已配置视频模型，这是数据范围差异，不是布局缺失。

## Focused region comparison evidence

- 字体与排版：模型名称使用 14px 半粗体；钻石和促销胶囊与名称同一行，运营文案使用 11px 次级文字并始终位于所属模型名称正下方。
- 间距与布局：图标为 36px，选项最小高度 56px；普通未悬停项隐藏第二行但保持行高稳定，选中或悬停时文案在行内出现，不生成脱离模型项的浮层。
- 颜色与视觉 token：日间标题为 `#1d1d1f`、说明文字为 64% 深色；夜间标题为 `#f5f5f7`、说明文字为 60% 白色。选项背景、图标底和积分胶囊均按主题切换。
- 图像质量：会员钻石继续使用用户指定的 SVG；后台上传的深色模型图标在夜间模式执行显式反相，避免与图标底色融为一体。
- 文案与内容：选中模型默认显示后台“模型介绍文案”；其他模型默认隐藏，仅在悬停对应选项时显示，移开后收起。留空时任何状态都不生成第二行。
- 交互与权限：非会员专属模型仍为禁用状态，但禁用态不再降低整行透明度，保证用户可以看清模型信息；服务端会员校验未被改变。

## Findings

没有剩余可执行的 P0、P1 或 P2 视觉问题。

可接受差异：

- 本地只有一个已配置视频模型，因此无法复现参考图的多行滚动目录；单行结构、主题和信息层级已按同一组件契约实现。
- 本地模型名称、运营文案、促销角标与积分来自后台真实配置，内容不同于参考站示例属于业务数据差异。

## Comparison history

1. 初始 P1：运营文案被放进悬浮提示，离开模型名称区域，和参考图“名称下方固定显示”的信息结构不一致。
2. 初始 P1：模型弹层沿用单一深色背景和弱对比文字，日间/夜间没有独立令牌；禁用态进一步降低整行透明度，影响可读性。
3. 初始 P2：后台上传的深色模型图标在夜间图标底上几乎不可见。
4. 修复：删除悬浮提示，把 `marketingCopy` 放进模型行正文并置于名称下方；后台字段同步改名为“模型介绍文案”。
5. 修复：为弹层、标题、说明、图标底、积分胶囊、选中态和滚动条建立明确的日间/夜间样式；禁用项保持真实文字对比度。
6. 修复：仅对后台配置图标在夜间模式执行反相，不改变内置彩色图标。
7. 修复后证据：聚焦对比图中，两个主题的名称、钻石、促销角标、第二行文案和右侧积分均清晰可见且保持在同一模型行内。
8. 用户复核发现 P1：第二行文案不应对所有模型常驻；参考交互是选中项常驻、普通项默认隐藏、普通项悬停时显示。
9. 修复：模型行引入明确的 `selected` 与 `hovered` 状态，文案只在 `selected || hovered` 时渲染；后台说明和领域文档同步为同一契约。
10. 修复后证据：真实浏览器中未选中的会员视频模型打开列表后 `.canvas-model-picker-option-meta` 数量为 `0`，截图显示单行；此前选中态聚焦截图保留同一行内的第二行文案。选中态和悬停态复用同一渲染分支，因此视觉结构一致。

## Primary interactions tested

- 展开视频节点的模型选择器。
- 切换日间与夜间主题并重复核对模型行。
- 验证未选中且未悬停的模型不渲染介绍文案。
- 验证选中模型直接进入介绍文案显示分支；其他模型的鼠标进入/移出分别设置和清除悬停状态。
- 验证会员钻石、促销角标和积分胶囊不受文案显隐影响。
- 验证非会员专属模型保持不可选。
- 检查浏览器控制台，错误数量为 `0`。

## Implementation checklist

- [x] 选中模型默认显示介绍文案
- [x] 普通模型默认隐藏介绍文案
- [x] 普通模型悬停时在名称下方显示文案，移开后收起
- [x] 钻石与促销角标保留在标题行
- [x] 日间模式文字、图标与胶囊清晰可读
- [x] 夜间模式文字、图标与胶囊清晰可读
- [x] 非会员禁用态不牺牲信息可读性
- [x] 后台字段说明与前台真实行为一致
- [x] 日间/夜间真实浏览器截图与同屏对比完成
- [x] 浏览器控制台无错误

final result: passed

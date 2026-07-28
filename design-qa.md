# Design QA — 会员模型钻石标识

- Source visual truth: `C:\Users\nz999\AppData\Local\Temp\codex-clipboard-655b77fc-3382-423b-b670-7ac951870eae.png`
- Implementation screenshot: `C:\Users\nz999\AppData\Local\Temp\member-diamond-picker.png`
- Source pixels: `360 × 412`
- Implementation pixels / viewport: `991 × 912`, device scale factor `1`
- State: 深色画布、视频节点已选中、模型选择下拉已展开、当前用户无有效会员
- Density normalization: 对比范围限定为模型列表项；以实现中的 CSS 实测尺寸作为图标尺寸依据，不对整页布局做像素缩放。

## Full-view comparison evidence

参考图把会员身份表现为紧跟模型名称的橙色钻石，右侧价格/时长保持独立对齐。实现中 `Seedance 2.0 Fast` 后紧跟相同橙色钻石，积分价格仍保持右侧对齐；原先独立占位的“会员”胶囊已完全移除。

## Focused region comparison evidence

- 钻石资源使用用户提供的原始 SVG 路径与颜色，不使用近似图标或 CSS 绘制。
- 实际渲染尺寸为 `12 × 12px`，位于 `20px` 高的模型名称行中，比例和参考图一致。
- 名称与钻石间距为 `4px`，长名称仍可截断，钻石不参与压缩。
- 非会员选项仍为 `aria-disabled="true"`，视觉替换没有破坏权限状态。
- 已选模型标签和下拉列表项复用同一钻石组件，避免状态表现不一致。

## Findings

没有发现可执行的 P0、P1 或 P2 差异。

## Comparison history

- Initial implementation: 使用锁图标和“会员”文字胶囊，与参考图的紧凑钻石状态标识不一致。
- Fix: 移除文字胶囊，将用户提供的钻石 SVG 保存为独立资产并紧贴模型名称渲染。
- Post-fix evidence: 真实画布下拉中钻石为 `12 × 12px`，旧胶囊数量为 `0`，会员模型仍保持禁用状态。

## Implementation checklist

- [x] 下拉列表会员模型名称后显示钻石
- [x] 已选会员模型名称后显示钻石
- [x] 移除旧“会员”胶囊
- [x] 保留非会员禁用状态
- [x] TypeScript 类型检查与生产构建通过

final result: passed

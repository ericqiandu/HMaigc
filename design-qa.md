# Design QA — 会员中心窗口式高保真复刻

- Source visual truth:
  - Desktop personal: `E:\新版短剧制作\open-ai-canvas\.local\qa\liblib-membership-source-desktop-20260729.jpg`
  - Desktop team: `E:\新版短剧制作\open-ai-canvas\.local\qa\liblib-membership-source-team-20260729.jpg`
- Browser-rendered implementation:
  - Desktop personal: `E:\新版短剧制作\open-ai-canvas\.local\qa\membership-window-personal-final-20260729.png`
  - Desktop team: `E:\新版短剧制作\open-ai-canvas\.local\qa\membership-window-team-20260729.png`
  - Mobile: `E:\新版短剧制作\open-ai-canvas\.local\qa\membership-window-mobile-390x844-20260729.png`
- Full comparison: `E:\新版短剧制作\open-ai-canvas\.local\qa\membership-window-full-comparison-20260729.png`
- Focused comparison: `E:\新版短剧制作\open-ai-canvas\.local\qa\membership-window-focused-comparison-20260729.png`
- Desktop source pixels / implementation pixels: `990 × 912` / `990 × 912`
- Desktop CSS viewport: `990 × 912`
- Mobile implementation pixels / CSS viewport: `390 × 844`
- Device scale factor: `1`
- Density normalization: 桌面参考与实现均为相同像素尺寸，未缩放；移动端单独按真实 `390 × 844` CSS 视口验证。
- State: 创作会员 / 年付 / 套餐列表首屏；另外验证团队会员、席位增减、个人月付和购买确认窗口。

## Full-view comparison evidence

同屏对比显示，两侧都采用接近全视口的深色会员窗口：右上角关闭、顶部创作/团队双标签、独立计费胶囊、纵向高套餐卡和左右悬浮箭头。HMaigc 窗口保留 10px 外边距与 8px 圆角，以明确满足“窗口形式”，参考站则几乎贴满浏览器视口。

参考图处于横向列表中段，HMaigc 截图处于列表起点；二者卡片轨道、卡宽、间距和截断方式仍按相同结构核对，不把业务数据或滚动位置差异判为布局缺陷。

## Focused comparison evidence

- 字体与排版：顶部标签均为 17px 半粗体；套餐标题为 22px，金额使用 42px 低字重显示；次级说明、原价和月均价格保持弱对比，不抢主价格层级。
- 间距与布局：顶部标签分隔线、计费切换和卡片起始位置与参考图在同一纵向节奏；套餐卡固定 310px 宽、12px 间距，横向溢出由左右箭头与触控滚动共同承载。
- 颜色与视觉 token：窗口使用纯黑背景，普通卡头部为 `#202022`、权益区为 `#0e0e10`；推荐卡使用 HMaigc 蓝色而非复制参考站青色促销资产。
- 图像质量：该界面没有依赖插画、品牌图形或产品图；关闭、并发、积分、权益等均使用现有 Lucide 图标库，不存在占位图或手绘近似资产。
- 文案与内容：会员类型、套餐名称、金额、积分、并发、折扣和权益全部来自 HMaigc 后台真实配置。未复制参考站价格、品牌活动或自动续费承诺。

## Findings

没有剩余可执行的 P0、P1 或 P2 视觉问题。

可接受差异：

- 参考站显示年/季/月三个计费周期，当前 HMaigc 后台契约仅提供年/月。界面只渲染真实可售周期，不创建不可下单的“按季购买”假入口。
- 参考站包含花呗、专属模型低价和活动赠品；HMaigc 当前后台无对应商业配置，因此不复制这些业务内容。
- 参考图套餐价格、权益数量和卡片名称不同，属于真实业务数据差异。
- HMaigc 保留轻微窗口外边距与圆角，使独立窗口边界可识别；窄屏下自动切为全屏无圆角窗口。

## Comparison history

1. 初始 P1：旧实现是普通整页定价页，顶部带返回与品牌栏，缺少参考站的遮罩、右上关闭、固定双标签和窗口内滚动结构。
2. 修复：页面硬切换为 `role="dialog"` 的固定会员窗口；顶栏、计费切换、套餐舞台、箭头和内部滚动重新建立，不保留旧布局兼容分支。
3. 初始 P2：第一轮窗口卡片约 286px 宽，整体为单一蓝黑表面，参考站卡宽约 310px 且购买区与权益区有明显明度分层。
4. 修复：卡片统一为 310px 固定轨道；普通卡改为深灰购买区与近黑权益区，推荐卡使用品牌蓝强调；卡高增加到 760px，首屏延续参考站的纵向滚动节奏。
5. 修复后证据：最终同屏图中，标签分隔线、计费胶囊、卡片顶部、三卡横向节奏和悬浮箭头均在相同视觉区域；桌面与移动端均无裁切核心操作。

## Primary interactions tested

- 创作会员与团队版会员切换。
- 个人年付与月付切换，月付套餐单位随真实数据变为 `/月`。
- 团队套餐席位从 2 增加到 3，价格与总积分随席位重新计算。
- 点击“开通团队会员”打开真实购买确认窗口，再取消返回。
- 桌面横向套餐舞台与左右箭头状态。
- `390 × 844` 窄屏下全屏窗口、顶部标签、计费切换、卡片和箭头可见性。
- 生产构建通过；浏览器内未出现错误页或运行时异常提示。

## Implementation checklist

- [x] 会员中心改为固定窗口，不再是普通全页布局
- [x] 右上角关闭与 `Escape` 关闭路径
- [x] 创作/团队双标签固定在窗口顶部
- [x] 计费周期由后台真实套餐动态生成
- [x] 套餐卡横向舞台、左右箭头和触控滚动
- [x] 团队席位、总价、总积分保持真实联动
- [x] 当前会员权益和订单记录保留在窗口后续内容区
- [x] 桌面与 `390 × 844` 窄屏浏览器截图
- [x] 参考与实现同屏完整及聚焦对比
- [x] TypeScript 与 Vite 生产构建通过

final result: passed

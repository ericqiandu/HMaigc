# 画布浮层组件统一验收

## 目标

统一画布页按钮触发的菜单、选择面板与模态框视觉语言，同时保持原有业务行为、权限判断和数据请求不变。

## 审查结论

- P0：无阻断问题。
- P1：已消除画布菜单、工作空间模式、创建节点、画布外观、缩放、素材托盘、搜索、分享与团队协作之间的圆角、阴影、标题层级和控件高度差异。
- P2：Agent 模型、Skill 与生成模式选择器保留其既有信息密度，但共享同一浮层边框、表面、阴影和字体令牌。

## 统一令牌

- 浮层圆角：`12px`
- 控件圆角：`8px`
- 菜单项圆角：`7px`
- 桌面菜单项最小高度：`36px`
- 移动菜单项最小高度：`44px`
- 标题栏高度：`48px`
- 正文间距：`16px`
- 阴影：单层 `0 16px 48px`
- 强调色仅用于选中、聚焦和主要操作

## 已验收入口

- 顶部：画布菜单、工作空间模式、媒体性能、搜索、团队协作、分享
- 底部：添加节点、画布外观、精确缩放、素材空间
- 画布：右键菜单及新增节点子菜单（源码契约与统一类检查）
- Agent：模型、Skill、生成模式选择浮层

## 验收环境与证据

- 桌面：1294 × 912，深色与浅色主题
- 移动：390 × 844，创建节点与分享弹窗
- 构建：`pnpm --dir web build`
- 测试：`pnpm --dir web test`，11 项通过
- 容器：`docker compose up -d --build --no-deps web`

代表性截图：

- `top-navigation-menu.png`
- `workspace-mode-menu.png`
- `create-node-panel.png`
- `appearance-panel.png`
- `search-modal.png`
- `share-modal.png`
- `collaboration-modal.png`
- `light-navigation-menu.png`
- `mobile-create-panel-390.png`
- `mobile-share-modal-390.png`
- `agent-model-menu.png`
- `agent-skill-menu.png`
- `agent-mode-menu.png`

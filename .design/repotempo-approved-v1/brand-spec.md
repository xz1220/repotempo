# RepoTempo 品牌规范

采用 shadcn/ui 中性工作台的组件规范作为参考，保留单文件原型实现。浅灰导航、白色内容区、石墨色正文和少量靛蓝强调色，优先呈现仓库信息。

```css
:root {
  --bg: oklch(0.978 0.004 265);
  --surface: oklch(1 0 0);
  --fg: oklch(0.215 0.012 265);
  --muted: oklch(0.485 0.016 265);
  --border: oklch(0.91 0.006 265);
  --accent: oklch(0.54 0.19 267);
}
```

- Display 与 Body：`ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif`
- Mono：`ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace`

## 视觉规则

1. 使用浅色侧栏，导航当前项采用白色底与细边框；靛蓝仅用于主操作和键盘焦点。
2. 项目以轻分隔列表呈现，仓库信息、累计 Star、期间增长和操作采用共享列宽。
3. 页面标题 24px，仓库名 14px，正文 13px，辅助信息 11–12px；用字重与颜色建立层级。
4. 桌面常规按钮 34–36px，行内按钮 26–28px；筛选默认收起，标签默认展示四个并支持展开全部。
5. 原始快照、关注、已读和筛选功能保留；没有历史数据时显示破折号并说明原因，禁止虚构增长。

## 参考

- https://ui.shadcn.com/docs/theming
- https://ui.shadcn.com/examples/tasks

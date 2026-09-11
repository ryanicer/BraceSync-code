# BraceSync Agent 协作纪律

> 本文件自动注入所有 agent 上下文。每次接任务前必读。

## 你的工作目录（绝对不要用别人的）

### docs 仓（写文档时）
| 你是谁 | 你的目录 |
|---|---|
| Winner | D:/proj/BraceSync-docs-win |
| Iris   | D:/proj/BraceSync-docs-iris |
| Andy   | D:/proj/BraceSync-docs-andy |
| Peter  | D:/proj/BraceSync-docs-peter |
| Ella   | D:/proj/BraceSync-docs-ella |
| Joe    | D:/proj/BraceSync-docs-joe |

### code 仓（写代码时）
- 宿主：D:/proj/BraceSync（只读，不在此写代码）
- 你的 worktree：D:/proj/BraceSync/.worktrees/{你的名字}

## 三条铁律（违反 = 直接打回）

1. **分支名不许带 /** — 本机 git 建不出带斜杠的引用。用 t151-seg2-iris 格式。
   建完必验证：git rev-parse --abbrev-ref HEAD
2. **禁止 git checkout --orphan 后提交** — 会生成垃圾 root commit PR。
   新分支一律从 origin/main 起。
3. **禁共用目录** — docs 仓每人独立 clone，code 仓每人独立 worktree。
   绝不碰别人的目录。

## 红线（碰了就打回）
- 分支名含 /
- git checkout --orphan 后提交
- 共用别人的 clone / worktree
- 本地 merge / commit 到 main
- git reset --hard / git rebase
- 对象库损坏后继续在该目录写文件

## 标准流程
1. 在自己的目录干活（docs 用 clone，code 用 worktree）
2. 起分支：git checkout -b tXXX-{你}（无斜杠）→ 验证 git rev-parse --abbrev-ref HEAD
3. 提交 + 开 PR：git push origin HEAD → gh pr create（base origin/main）
4. 同步 main：git fetch origin + git merge origin/main（不用 reset/rebase）
5. PM 合并后：git fetch 拉新 main

## 推送 & gh
- gh CLI：export GH_CONFIG_DIR="C:/Users/never/AppData/Roaming/GitHub CLI"
- 推送用：git push origin HEAD:refs/heads/main（避免假成功）
- 出问题？对象库损坏 → 不原地抢救，丢弃重 clone

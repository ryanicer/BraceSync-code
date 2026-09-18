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

### ⚠️ Andy 的工作环境在「远程服务器」
- 你在服务器上改文件，**改完必须 `git commit` + `git push` 开 PR** ——
  只留工作区会被清理动作丢弃（2026-09-18 T238 实测：脚本修复当晚丢失）。

## 四条铁律（违反 = 直接打回）

1. **分支名不许带 /** — 本机 git 建不出带斜杠的引用。用 t151-seg2-iris 格式。
   建完必验证：git rev-parse --abbrev-ref HEAD
2. **禁止 git checkout --orphan 后提交** — 会生成垃圾 root commit PR。
   新分支一律从 origin/main 起。
3. **禁共用目录** — docs 仓每人独立 clone，code 仓每人独立 worktree。
   绝不碰别人的目录。
4. 🔴 **新任务必须从最新 `origin/main` 开分支** —— **绝不基于「上一个任务的分支」继续**。
   上个分支一旦被 **squash 合并**，它的 commit 就作废了；你再基于它，GitHub 必然报
   **`CONFLICTING`/`DIRTY` 且 CI 不跑**（假冲突，2026-09-18 已因此返工三次）。
   正确：`git fetch origin main && git checkout -b tXXX-{你} origin/main`
   若已冲突：`git merge origin/main` 解决后 push（**禁 rebase**）。

## 红线（碰了就打回）
- 分支名含 /
- git checkout --orphan 后提交
- 共用别人的 clone / worktree
- 本地 merge / commit 到 main
- git reset --hard / git rebase
- 对象库损坏后继续在该目录写文件
- **基于「上一个任务的分支」开新分支**（squash 后必冲突）

## 标准流程
1. 在自己的目录干活（docs 用 clone，code 用 worktree）
2. **从最新 main 起分支**：`git fetch origin main && git checkout -b tXXX-{你} origin/main`
   → 验证 `git rev-parse --abbrev-ref HEAD`
3. 提交 + 开 PR：`git push origin HEAD:refs/heads/<你的分支>` → `gh pr create --base main`
4. 同步 main：`git fetch origin` + `git merge origin/main`（不用 reset/rebase）
5. PM 合并后：`git fetch` 拉新 main

## 推送 & gh
- gh CLI：`export GH_CONFIG_DIR="C:/Users/never/AppData/Roaming/GitHub CLI"`
- 推送用：`git push origin HEAD:refs/heads/<你的分支>`（**不要写 `refs/heads/main`**，那是直推主干）
- 🔴 **推送后必做**：`git ls-remote origin <你的分支>` 确认远端真有你的 commit
  （防「报成功但没推」—— 2026-09-18 已踩 2 次）
- 🔴 **开 PR 后必做**：`gh pr checks <PR号>`；若显示 **`no checks reported`，多半是分支冲突了**
  （GitHub 对冲突 PR 不跑 CI），**不是延迟** —— 先 `git merge origin/main` 解决
- 出问题？对象库损坏 → 不原地抢救，丢弃重 clone

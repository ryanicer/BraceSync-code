#!/usr/bin/env bash
# BraceSync 服务器端自举上下文脚本
# 用法:
#   bash scripts/deploy/server-context.sh               # 全量上下文(仓库/目录/任务索引/命令速查/staging 状态)
#   bash scripts/deploy/server-context.sh T035-staging部署   # 查看指定任务文档全文
# 目的:让服务器端(Andy 等 DevOps/部署 agent)不依赖本地转述,一条命令自取全部上下文。
# 权威仓库: /home/ubuntu/bracesync(最新 main,含任务文档与部署资产)
set -euo pipefail

PROJECT_ROOT="/home/ubuntu/bracesync"
STAGING_DIR="/opt/bracesync-staging"
PROD_DIR="/opt/bracesync"
export PATH=/usr/local/go/bin:/usr/bin:/usr/sbin:$PATH

cd "$PROJECT_ROOT"

# 指定了任务编号 → 直接打印任务文档
if [ -n "${1:-}" ]; then
  DOC="docs/tasks/andy/$1.md"
  if [ -f "$DOC" ]; then
    echo "=== 任务文档: $DOC ==="
    cat "$DOC"
  else
    echo "未找到 $DOC。任务文档已移至文档仓（BraceSync-docs），服务器不维护。" >&2
    exit 1
  fi
  exit 0
fi

echo "================ BraceSync 服务器自举上下文 ================"
echo ""
echo "[1/5] 仓库状态(权威工作目录)"
echo "  目录:   $PROJECT_ROOT"
echo "  分支:   $(git branch --show-current)"
echo "  版本:   $(git rev-parse --short HEAD)  $(git log --oneline -1 --format='%s')"
echo "  远程:   $(git remote | tr '\n' ' ')"
echo "  未提交: $(git status --porcelain | wc -l) 处"
echo "  更新仓库: git pull <远程名> main(远程通时);否则联系本地用 bundle/rsync 同步"
echo ""

echo "[2/5] 服务器目录地图"
echo "  $PROJECT_ROOT  权威仓库(最新代码+任务文档+scripts/deploy 部署资产)"
echo "  $STAGING_DIR   staging 部署目录(docker-compose.yml/.env/nginx.conf)"
echo "  $PROD_DIR      生产部署目录(红线:仅部署流程触碰,日常禁止)"
echo "  /tmp/bracesync-worktree-residual  旧分支残留备份(可清理)"
echo "  /tmp/bracesync-build  构建临时目录"
echo ""

echo "[3/5] 任务文档"
if [ -d docs/tasks ]; then
  ls docs/tasks/andy/ | sed 's/^/    - /'
  echo "  查看全文: bash scripts/deploy/server-context.sh <任务编号>"
  echo "  全部任务: ls docs/tasks/ 按负责人分目录"
else
  echo "  任务文档已移至文档仓（BraceSync-docs），服务器不维护 docs/"
fi
echo ""

echo "[4/5] 命令速查"
echo "  ⓪ 一键部署 staging: bash scripts/deploy/deploy-staging.sh          # git pull + 构建 + 迁移 + 冒烟"
echo "  ① 编译全部镜像:   bash scripts/deploy/build-all.sh staging-<sha>   # go 已注入 PATH"
echo "  ② 部署到 staging:  cd $STAGING_DIR && sudo docker compose up -d"
echo "  ③ 查看容器状态:   sudo docker compose ps"
echo "  ④ 单服务日志:     sudo docker logs -f bracesync-staging-<service>-1"
echo "  ⑤ 迁移(方式B):    cd $STAGING_DIR && for f in $PROJECT_ROOT/scripts/db/migrations/*.up.sql; do sudo docker compose exec -T postgres psql -U bracesync -d bracesync -v ON_ERROR_STOP=1 -f - < \"\$f\"; done"
echo "  ⑥ 冒烟:           curl -s localhost:81/healthz;curl -s localhost:81/api/v1/auth/login ...(详见任务文档验收标准)"
echo "  ⑦ 磁盘/资源:      df -h /; free -h"
echo ""

echo "[5/5] staging 运行状态"
if cd "$STAGING_DIR" && sudo docker compose ps --format 'table {{.Name}}\t{{.Status}}\t{{.Ports}}' 2>/dev/null | head -25; then
  echo ""
  echo "  (说明: Restarting 表示容器崩溃循环,查日志: sudo docker logs --tail 30 <容器名>)"
else
  echo "  docker compose 不可用或 staging 未定义,检查 $STAGING_DIR"
fi
echo ""
echo "============================================================"

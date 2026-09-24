#!/usr/bin/env bash
# T383 第一处：把 cron 引用的备份脚本「外置签发」到被部署工作树之外
# 用法：bash scripts/deploy/publish-cron-scripts.sh [本轮 head 短 sha]
#   环境变量（测试用）：CRON_PUBLISH_DIR 外置落点 / CRON_PUBLISH_LOG 台账 / CRON_SOURCE_DIR 源目录
#
# 要解决的问题：ubuntu crontab 两条定时任务原先直接 /bin/bash $PROJECT_ROOT/scripts/backup/*.sh，
#   而 $PROJECT_ROOT 正是 deploy-staging.sh ① 步 `git checkout -- .` + `git pull` 要换掉的工作树 ——
#   和被部署脚本同域，跑批中途被覆盖 / 随回滚漂移，两条隐患与 T364 那一格同源。
# 做法：每轮部署把源脚本原子装到工作树之外的落点（mode 500），cron 只引用那份外置副本。
#   install 到同目录临时文件再 mv ⇒ 换文件是原子的，正在跑的那轮继续读旧 inode，读不到半截脚本。
# 退出码：0 全部签发成功 / 1 源缺失或字节不一致或装不下（宁可红，也不让 cron 指向半成品）
set -euo pipefail

HEAD_LABEL="${1:-unknown}"
SOURCE_DIR="${CRON_SOURCE_DIR:-/home/ubuntu/bracesync/scripts/backup}"
PUBLISH_DIR="${CRON_PUBLISH_DIR:-/home/ubuntu/bracesync-ops/bin}"
PUBLISH_LOG="${CRON_PUBLISH_LOG:-${PUBLISH_DIR%/*}/cron-scripts.published.log}"
# cron 现在真正引用的两条 —— 缺任何一条就判红，别让「外置副本比工作树少一个脚本」蒙混过去
REQUIRED="pg-backup-cos.sh staging-restore-drill.sh"

sha_of() { sha256sum -- "$1" | awk '{print $1}'; }
say() { echo "[publish-cron] $*"; }
die() { echo "[publish-cron] ERROR $*" >&2; exit 1; }

[ -d "$SOURCE_DIR" ] || die "源目录不存在：$SOURCE_DIR（本轮不签发，避免外置落点被清空后 cron 无脚本可用）"
# 先验齐再签发：只签发「一半」会让 cron 下一轮拿到旧副本却没人报警（源缺一半比全缺更难查）
for req in $REQUIRED; do
  [ -r "$SOURCE_DIR/$req" ] || die "源目录缺 cron 引用的 $req：$SOURCE_DIR ⇒ 本轮不签发（宁可让 cron 继续跑上一轮副本，也不签一半）"
done
mkdir -p "$PUBLISH_DIR" || die "外置落点建不出来：$PUBLISH_DIR"
[ -w "$PUBLISH_DIR" ] || die "外置落点不可写：$PUBLISH_DIR"

published=0
for name in $(ls -1 "$SOURCE_DIR"/*.sh 2>/dev/null | xargs -r -n1 basename); do
  src="$SOURCE_DIR/$name"
  [ -r "$src" ] || die "源脚本不可读：$src"
  tmp=$(mktemp "$PUBLISH_DIR/.${name}.XXXXXX") || die "mktemp 失败（$PUBLISH_DIR）"
  if ! install -m 500 "$src" "$tmp"; then
    rm -f "$tmp" 2>/dev/null || true
    die "临时副本安装失败：$src"
  fi
  if ! mv -f "$tmp" "$PUBLISH_DIR/$name"; then
    rm -f "$tmp" 2>/dev/null || true
    die "原子替换失败：$PUBLISH_DIR/$name"
  fi
  src_sha=$(sha_of "$src")
  dst_sha=$(sha_of "$PUBLISH_DIR/$name")
  [ "$src_sha" = "$dst_sha" ] || die "签发后字节不一致：$name（源 $src_sha 副本 $dst_sha）"
  say "✅ $name → $PUBLISH_DIR/$name sha256=$src_sha bytes=$(wc -c < "$src" | tr -d '[:space:]') mode=500"
  printf '%s head=%s script=%s sha256=%s\n' "$(date '+%Y-%m-%d %H:%M:%S %Z')" "$HEAD_LABEL" "$name" "$dst_sha" >> "$PUBLISH_LOG"
  published=$((published + 1))
done

[ "$published" -gt 0 ] || die "源目录里一个 .sh 都没签发：$SOURCE_DIR"

for req in $REQUIRED; do
  [ -r "$PUBLISH_DIR/$req" ] || die "cron 引用的 $req 未出现在外置落点（$PUBLISH_DIR）⇒ 改 cron 表前先补齐"
done
say "完成：$published 个脚本已外置，台账 $PUBLISH_LOG（cron 应引用 $PUBLISH_DIR 下的副本，不再引用工作树）"

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
#   T394 起 1 还包含两种「签了一半」的形态：临时副本字节与源不等（坏字节不落落点）、
#   本轮签发集没覆盖 REQUIRED（落点里那份是上一轮的陈旧副本 —— 少签不等于签过）。
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
# 中止前先清临时文件：坏字节留在落点里，一来 `ls -1A` 多出一个隐藏半成品，
# 二来让人以为「签了一半、还差一步」，而实际上那份半成品永远不会被 cron 引用（名字带随机后缀）。
abort_tmp() { local msg="$1" f="$2"; rm -f "$f" 2>/dev/null || true; die "$msg"; }

[ -d "$SOURCE_DIR" ] || die "源目录不存在：$SOURCE_DIR（本轮不签发，避免外置落点被清空后 cron 无脚本可用）"
# 先验齐再签发：只签发「一半」会让 cron 下一轮拿到旧副本却没人报警（源缺一半比全缺更难查）
for req in $REQUIRED; do
  [ -r "$SOURCE_DIR/$req" ] || die "源目录缺 cron 引用的 $req：$SOURCE_DIR ⇒ 本轮不签发（宁可让 cron 继续跑上一轮副本，也不签一半）"
done
mkdir -p "$PUBLISH_DIR" || die "外置落点建不出来：$PUBLISH_DIR"
[ -w "$PUBLISH_DIR" ] || die "外置落点不可写：$PUBLISH_DIR"

published=0
signed_names=""
for name in $(ls -1 "$SOURCE_DIR"/*.sh 2>/dev/null | xargs -r -n1 basename); do
  src="$SOURCE_DIR/$name"
  [ -r "$src" ] || die "源脚本不可读：$src"
  tmp=$(mktemp "$PUBLISH_DIR/.${name}.XXXXXX") || die "mktemp 失败（$PUBLISH_DIR）"
  if ! install -m 500 "$src" "$tmp"; then
    rm -f "$tmp" 2>/dev/null || true
    die "临时副本安装失败：$src"
  fi
  # T394 N2：字节核对先做一次，且做在 mv 【之前】。
  #   原先只有 mv 之后那一次比对 ⇒ 临时副本一旦与源不同字节（拷贝被截断、磁盘写坏、被并发进程动过），
  #   坏字节已经落到 cron 引用的路径上：die 只「喊了出事」，没「挡住出事」——
  #   下一轮 02:00 定时任务照跑那份坏副本，而台账里还留着上一轮的正确 sha 当证据。
  #   现在先在临时文件上比，不等就中止并清掉临时文件 ⇒ 坏字节不落落点，cron 继续用上一轮副本
  #   （宁可旧，不可半截）。mv 之后那次比对保留：它管「换完名字之后又被改掉」那另一种时序。
  src_sha=$(sha_of "$src")
  [ "$src_sha" = "$(sha_of "$tmp")" ] || abort_tmp "临时副本与源字节不一致：$name（源 $src_sha）⇒ 坏字节未落外置落点，cron 仍用上一轮副本" "$tmp"
  if ! mv -f "$tmp" "$PUBLISH_DIR/$name"; then
    rm -f "$tmp" 2>/dev/null || true
    die "原子替换失败：$PUBLISH_DIR/$name"
  fi
  dst_sha=$(sha_of "$PUBLISH_DIR/$name")
  [ "$src_sha" = "$dst_sha" ] || die "签发后字节不一致：$name（源 $src_sha 副本 $dst_sha）"
  say "✅ $name → $PUBLISH_DIR/$name sha256=$src_sha bytes=$(wc -c < "$src" | tr -d '[:space:]') mode=500"
  printf '%s head=%s script=%s sha256=%s\n' "$(date '+%Y-%m-%d %H:%M:%S %Z')" "$HEAD_LABEL" "$name" "$dst_sha" >> "$PUBLISH_LOG"
  signed_names="$signed_names $name"
  published=$((published + 1))
done

[ "$published" -gt 0 ] || die "源目录里一个 .sh 都没签发：$SOURCE_DIR"

# T394 N3：落点后验从「文件在不在」改成「本轮签发集 ⊇ REQUIRED」。
#   旧写法逐条 [ -r "$PUBLISH_DIR/$req" ] 只问存在 ⇒ 上一轮留下的陈旧副本正好把这一格填掉：
#   Joe 验收 M4 现场（前置齐验被改成 continue）只签出 1 条、返回 0，收尾后验照样放行，
#   于是「本轮少签」和「本轮全签」在回执里长得一模一样。
#   改成按本轮签发集判，并把两种失败文案分开：没签 ≠ 签了但落点那份是旧的 —— 前一句要人去补签，
#   后一句要人去查为什么签了一半还留了个能跑的文件。
for req in $REQUIRED; do
  case " $signed_names " in
    *" $req "*) ;;
    *)
      if [ -r "$PUBLISH_DIR/$req" ]; then
        die "cron 引用的 $req 本轮没签出去，外置落点里那份是上一轮留下的陈旧副本（$PUBLISH_DIR/$req）⇒ 少签不等于签过"
      fi
      die "cron 引用的 $req 本轮没签出去，且外置落点里没有副本（$PUBLISH_DIR/$req）⇒ cron 下一轮直接无可执行文件"
      ;;
  esac
done
say "完成：$published 个脚本已外置（本轮签发集：${signed_names# }），台账 $PUBLISH_LOG（cron 应引用 $PUBLISH_DIR 下的副本，不再引用工作树）"

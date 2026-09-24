#!/usr/bin/env bash
# T364 部署脚本自改隐患自检
# 用法：
#   bash scripts/deploy/selfcheck-deploy-script.sh record <被检脚本> <状态文件>
#   bash scripts/deploy/selfcheck-deploy-script.sh verify <被检脚本> <状态文件>
# 退出码：
#   record  0 已落基线 / 3 参数或被检脚本不可读
#   verify  0 字节未变 / 2 字节已变（自改隐患在本次跑批中发生过）/ 3 参数或状态文件缺失
set -euo pipefail

die() { echo "[selfcheck] $*" >&2; exit 3; }

sha_of() { sha256sum -- "$1" | awk '{print $1}'; }
bytes_of() { wc -c < "$1" | tr -d '[:space:]'; }
head_of() { git -C "$(dirname "$(readlink -f "$1")")" rev-parse --short HEAD 2>/dev/null || echo unknown; }

cmd="${1:-}"
target="${2:-}"
state="${3:-}"

[ -n "$cmd" ] && [ -n "$target" ] && [ -n "$state" ] || die "用法：$0 {record|verify} <被检脚本> <状态文件>"

case "$cmd" in
  record)
    [ -r "$target" ] || die "被检脚本不可读：$target"
    mkdir -p "$(dirname "$state")"
    {
      echo "mode=record"
      echo "path=$target"
      echo "sha256=$(sha_of "$target")"
      echo "bytes=$(bytes_of "$target")"
      echo "head=$(head_of "$target")"
      echo "at=$(date '+%Y-%m-%d %H:%M:%S %Z')"
    } > "$state"
    echo "[selfcheck] 基线已落盘 $state"
    echo "[selfcheck]   path=$target sha256=$(sha_of "$target") bytes=$(bytes_of "$target") head=$(head_of "$target")"
    ;;
  verify)
    [ -r "$target" ] || die "被检脚本不可读：$target"
    [ -r "$state" ] || die "状态文件不存在：$state（部署开头未跑 record）"
    base_sha=$(grep '^sha256=' "$state" | cut -d= -f2-)
    base_bytes=$(grep '^bytes=' "$state" | cut -d= -f2-)
    base_head=$(grep '^head=' "$state" | cut -d= -f2-)
    now_sha=$(sha_of "$target")
    now_bytes=$(bytes_of "$target")
    now_head=$(head_of "$target")
    echo "[selfcheck] 被检脚本 $target"
    echo "[selfcheck]   基线 sha256=$base_sha bytes=$base_bytes head=$base_head"
    echo "[selfcheck]   结束时 sha256=$now_sha bytes=$now_bytes head=$now_head"
    if [ "$base_sha" = "$now_sha" ]; then
      echo "[selfcheck] PASS 跑批期间工作树内该脚本字节未变，未触发自改"
      exit 0
    fi
    echo "[selfcheck] ALARM 跑批期间该脚本被换掉（sha256 不等，字节 $base_bytes 变 $now_bytes）" >&2
    exit 2
    ;;
  *)
    die "未知子命令：$cmd（可用 record / verify）"
    ;;
esac

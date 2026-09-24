#!/usr/bin/env bash
# T364 部署脚本自改隐患自检
# 用法：
#   bash scripts/deploy/selfcheck-deploy-script.sh record <被检脚本> <状态文件>
#   bash scripts/deploy/selfcheck-deploy-script.sh verify <被检脚本> <状态文件>
#   bash scripts/deploy/selfcheck-deploy-script.sh receipt <被检脚本> <状态文件> <凭据目录> <中止退出码>
# 退出码：
#   record   0 已落基线 / 3 参数或被检脚本不可读
#   verify   0 字节未变 / 2 字节已变（自改隐患在本次跑批中发生过）/ 3 参数或状态文件缺失
#   receipt  0 凭据已落盘（T383：中止轮没有 verify 可调，基线缺失也照实落一份）/ 3 参数不足或目录建不出来
set -euo pipefail

die() { echo "[selfcheck] $*" >&2; exit 3; }

sha_of() { sha256sum -- "$1" | awk '{print $1}'; }
bytes_of() { wc -c < "$1" | tr -d '[:space:]'; }
head_of() { git -C "$(dirname "$(readlink -f "$1")")" rev-parse --short HEAD 2>/dev/null || echo unknown; }

cmd="${1:-}"
target="${2:-}"
state="${3:-}"
receipt_dir="${4:-}"
abort_rc="${5:-}"

[ -n "$cmd" ] && [ -n "$target" ] && [ -n "$state" ] || die "用法：$0 {record|verify|receipt} <被检脚本> <状态文件> [凭据目录] [中止退出码]"

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
  receipt)
    # T383 第二处：① 至 ⑦ 任一步中止时 ⑧ 段根本没跑到，而 EXIT trap 随后就把基线删了
    #   ⇒ 中止轮一条凭据都不留。这里在删之前把「开头基线 + 此刻读数」落一份只读凭据文件，
    #   让每一轮无论成败都可对账。基线/被检脚本任一不可读都照实落盘（判读为 unknown），
    #   不能因为「读不到」就干脆不留东西 —— 那正是本卡要修的那一格。
    [ -n "$receipt_dir" ] || die "receipt 需要第 4 个参数：凭据目录"
    mkdir -p "$receipt_dir" || die "凭据目录建不出来：$receipt_dir"
    chmod 700 "$receipt_dir" 2>/dev/null || true

    base_sha="missing"
    base_bytes="missing"
    base_head="missing"
    base_at="missing"
    if [ -r "$state" ]; then
      base_sha=$(grep '^sha256=' "$state" | cut -d= -f2- || echo missing)
      base_bytes=$(grep '^bytes=' "$state" | cut -d= -f2- || echo missing)
      base_head=$(grep '^head=' "$state" | cut -d= -f2- || echo missing)
      base_at=$(grep '^at=' "$state" | cut -d= -f2- || echo missing)
    fi

    now_sha="unreadable"
    now_bytes="unreadable"
    now_head="unreadable"
    if [ -r "$target" ]; then
      now_sha=$(sha_of "$target")
      now_bytes=$(bytes_of "$target")
      now_head=$(head_of "$target")
    fi

    verdict="unknown"
    if [ "$base_sha" = "missing" ] || [ "$now_sha" = "unreadable" ]; then
      verdict="unknown（基线或当前读数取不到，无法判读自改）"
    elif [ "$base_sha" = "$now_sha" ]; then
      verdict="unchanged（中止期间工作树内该脚本字节未变）"
    else
      verdict="changed（中止期间该脚本字节已变，字节 $base_bytes 变 $now_bytes）"
    fi

    out="$receipt_dir/abort-$(date +%Y%m%d-%H%M%S)-$$.env"
    {
      echo "mode=abort-selfcheck"
      echo "written_at=$(date '+%Y-%m-%d %H:%M:%S %Z')"
      echo "abort_rc=${abort_rc:-unknown}"
      echo "path=$target"
      echo "baseline_at=$base_at"
      echo "baseline_sha256=$base_sha"
      echo "baseline_bytes=$base_bytes"
      echo "baseline_head=$base_head"
      echo "abort_sha256=$now_sha"
      echo "abort_bytes=$now_bytes"
      echo "abort_head=$now_head"
      echo "verdict=$verdict"
    } > "$out"
    chmod 400 "$out"
    echo "[selfcheck] 中止轮自比对凭据：$out"
    echo "[selfcheck]   $verdict"
    # 只留最新 20 份，防止反复失败轮把盘写满（对账用的都是最近一轮）
    ls -1t "$receipt_dir"/abort-*.env 2>/dev/null | tail -n +21 | while IFS= read -r stale; do
      rm -f "$stale" 2>/dev/null || true
    done
    ;;
  *)
    die "未知子命令：$cmd（可用 record / verify / receipt）"
    ;;
esac

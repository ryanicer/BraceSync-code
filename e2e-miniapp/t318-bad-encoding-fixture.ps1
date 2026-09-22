# T318 门禁自证夹具（临时）—— 故意写成「UTF-8 无 BOM + 含中文」，用于验证 PowerShell 编码门禁会判红。
# 结构复刻 T313 真实炸点（run-real-e2e.ps1 第 114 行）：中文串尾字节吞掉闭引号后 else 会变成独立命令。
# 采完「判红」证据后本文件即在下一个 commit 删除，不要在别处引用。
$ok = $true
if ($ok) { Write-Host "  已上报" -ForegroundColor Green } else { Write-Host "  上报失败（不影响本地结果）" -ForegroundColor Yellow }

$ErrorActionPreference = "Continue"
$base = "http://127.0.0.1:2080/api/v1"
$results = New-Object System.Collections.ArrayList

function Log-Result {
    param(
        [string]$task,
        [string]$id,
        [string]$endpoint,
        [int]$http,
        [string]$code,
        [string]$keyData,
        [string]$status="PASS"
    )
    $obj = New-Object PSObject -Property @{
        Task     = $task
        ID       = $id
        Endpoint = $endpoint
        HTTP     = $http
        Code     = $code
        KeyData  = $keyData
        Status   = $status
    }
    [void]$script:results.Add($obj)
    Write-Host "[$task-$id] HTTP=$http Code=$code | $keyData | $status"
}

function Safe-Get {
    param($uri, $headers)
    try {
        $resp = Invoke-WebRequest -Uri $uri -Headers $headers -UseBasicParsing
        $data = $resp.Content | ConvertFrom-Json
        return @{ http=$resp.StatusCode; code=$data.code; msg=$data.message; data=$data.data; raw=$data }
    } catch {
        if ($_.Exception.Response) {
            $sc = [int]$_.Exception.Response.StatusCode
            try {
                $sr = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
                $body = $sr.ReadToEnd()
                $data = $body | ConvertFrom-Json
                return @{ http=$sc; code=$data.code; msg=$data.message; data=$data.data; raw=$data }
            } catch {
                return @{ http=$sc; code=-1; msg=$_.Exception.Message; data=$null; raw=$null }
            }
        }
        return @{ http=0; code=-1; msg=$_.Exception.Message; data=$null; raw=$null }
    }
}

function Safe-Post {
    param($uri, $headers, $body)
    try {
        $resp = Invoke-WebRequest -Uri $uri -Method POST -Headers $headers -Body $body -ContentType "application/json" -UseBasicParsing
        $data = $resp.Content | ConvertFrom-Json
        return @{ http=$resp.StatusCode; code=$data.code; msg=$data.message; data=$data.data; raw=$data }
    } catch {
        if ($_.Exception.Response) {
            $sc = [int]$_.Exception.Response.StatusCode
            try {
                $sr = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
                $rb = $sr.ReadToEnd()
                $data = $rb | ConvertFrom-Json
                return @{ http=$sc; code=$data.code; msg=$data.message; data=$data.data; raw=$data }
            } catch {
                return @{ http=$sc; code=-1; msg=$_.Exception.Message; data=$null; raw=$null }
            }
        }
        return @{ http=0; code=-1; msg=$_.Exception.Message; data=$null; raw=$null }
    }
}

function Safe-Put {
    param($uri, $headers, $body)
    try {
        $resp = Invoke-WebRequest -Uri $uri -Method PUT -Headers $headers -Body $body -ContentType "application/json" -UseBasicParsing
        $data = $resp.Content | ConvertFrom-Json
        return @{ http=$resp.StatusCode; code=$data.code; msg=$data.message; data=$data.data; raw=$data }
    } catch {
        if ($_.Exception.Response) {
            $sc = [int]$_.Exception.Response.StatusCode
            try {
                $sr = New-Object System.IO.StreamReader($_.Exception.Response.GetResponseStream())
                $rb = $sr.ReadToEnd()
                $data = $rb | ConvertFrom-Json
                return @{ http=$sc; code=$data.code; msg=$data.message; data=$data.data; raw=$data }
            } catch {
                return @{ http=$sc; code=-1; msg=$_.Exception.Message; data=$null; raw=$null }
            }
        }
        return @{ http=0; code=-1; msg=$_.Exception.Message; data=$null; raw=$null }
    }
}

Write-Host "====== PRE: Login ======"
$loginBody = @{username="ops_admin";password="admin123"} | ConvertTo-Json
$login = Safe-Post -uri "$base/auth/login" -headers @{} -body $loginBody
$token = $login.data.token
$h = @{Authorization="Bearer $token"}
Write-Host "TOKEN OK: $($token.Length) chars, code=$($login.code), http=$($login.http)"

Write-Host ""
Write-Host "====== Task 5: Devices/Teams/Technicians/InstallRecords ======"

# 5-1 GET /devices
Write-Host ""
Write-Host "--- 5-1 GET /devices ---"
$r = Safe-Get -uri "$base/devices" -headers $h
$list = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$total = if ($r.data -and $r.data.total) { $r.data.total } else { @($list).Count }
$hasPatName = $false
$fields = "N/A"
if ($list -and @($list).Count -gt 0) {
    $f = @($list[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name)
    $hasPatName = $f -contains "patientName"
    $fields = $f -join ","
}
$st = if ($r.http -eq 200 -and $r.code -eq 0) {"PASS"} else {"FAIL"}
Log-Result -task "T5" -id "1" -endpoint "GET /devices" -http $r.http -code $r.code -keyData "total=$total, hasPatientName=$hasPatName, fields=$fields" -status $st

$r2 = Safe-Get -uri "$base/devices?keyword=DEV" -headers $h
$list2 = if ($r2.data -and $r2.data.list) { $r2.data.list } else { $r2.data }
$total2 = if ($r2.data -and $r2.data.total) { $r2.data.total } else { @($list2).Count }
$st2 = if ($r2.http -eq 200 -and $r2.code -eq 0) {"PASS"} else {"FAIL"}
Log-Result -task "T5" -id "1b" -endpoint "GET /devices?keyword=DEV" -http $r2.http -code $r2.code -keyData "count=$total2" -status $st2

# 5-2 GET /teams
Write-Host ""
Write-Host "--- 5-2 GET /teams ---"
$r = Safe-Get -uri "$base/teams" -headers $h
$teams = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$tlen = @($teams).Count
$tfields = "N/A"
$hasRank = $false
$teamIds = "N/A"
if ($tlen -gt 0) {
    $tfields = @($teams[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name) -join ","
    $hasRank = (@($teams[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name) -contains "rank")
    $teamIds = @($teams | ForEach-Object { if ($_.teamId) { $_.teamId } else { $_.id } }) -join ","
}
if ($r.http -eq 200 -and $r.code -eq 0 -and $tlen -ge 3) { $st = "PASS" }
elseif ($r.http -eq 200 -and $r.code -eq 0) { $st = "WARN" }
else { $st = "FAIL" }
Log-Result -task "T5" -id "2" -endpoint "GET /teams" -http $r.http -code $r.code -keyData "len=$tlen, hasRank=$hasRank, teamIds=$teamIds, fields=$tfields" -status $st

# 5-3 GET /technicians
Write-Host ""
Write-Host "--- 5-3 GET /technicians ---"
$r = Safe-Get -uri "$base/technicians?page=1&pageSize=20" -headers $h
$techs = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$tlen = @($techs).Count
$tfields = "N/A"
if ($tlen -gt 0) {
    $tfields = @($techs[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name) -join ","
}
if ($r.http -eq 200 -and $r.code -eq 0 -and $tlen -ge 4) { $st = "PASS" }
elseif ($r.http -eq 200 -and $r.code -eq 0) { $st = "WARN" }
else { $st = "FAIL" }
Log-Result -task "T5" -id "3" -endpoint "GET /technicians?page=1&pageSize=20" -http $r.http -code $r.code -keyData "len=$tlen, fields=$tfields" -status $st

# toggle tech
$toggleResult = "skipped"
if ($tlen -gt 0) {
    $disabled = @($techs | Where-Object { $_.status -eq "disabled" -or $_.enabledStatus -eq "disabled" -or $_.enabledStatus -eq 0 })
    $targetTech = if ($disabled.Count -gt 0) { $disabled[0] } else { $techs[0] }
    $tid = if ($targetTech.techId) { $targetTech.techId } else { $targetTech.id }
    $curStatus = if ($targetTech.status) { $targetTech.status } else { $targetTech.enabledStatus }
    $action = if ($curStatus -eq "disabled" -or $curStatus -eq 0 -or $curStatus -eq $false) { "enable" } else { "disable" }
    $tb = @{action=$action} | ConvertTo-Json
    $tr = Safe-Post -uri "$base/technicians/$tid/toggle" -headers $h -body $tb
    $toggleResult = "techId=$tid, cur=$curStatus, action=$action, http=$($tr.http), code=$($tr.code), msg=$($tr.msg)"
    if ($tr.http -eq 200 -and $tr.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
    Log-Result -task "T5" -id "3b" -endpoint "POST /technicians/{id}/toggle" -http $tr.http -code $tr.code -keyData $toggleResult -status $st
} else {
    Log-Result -task "T5" -id "3b" -endpoint "POST /technicians/{id}/toggle" -http 0 -code -1 -keyData "no technicians to toggle" -status "WARN"
}

# 5-4 GET /install-records
Write-Host ""
Write-Host "--- 5-4 GET /install-records ---"
$r = Safe-Get -uri "$base/install-records?page=1&pageSize=20" -headers $h
$irs = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$ilen = @($irs).Count
$ifields = "N/A"
$hasTechName = $false
if ($ilen -gt 0) {
    $propNames = @($irs[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name)
    $ifields = $propNames -join ","
    $hasTechName = $propNames -contains "techName"
}
if ($r.http -eq 200 -and $r.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T5" -id "4" -endpoint "GET /install-records?page=1&pageSize=20" -http $r.http -code $r.code -keyData "len=$ilen, hasTechName=$hasTechName, fields=$ifields" -status $st

if ($ilen -gt 0) {
    $firstId = if ($irs[0].installId) { $irs[0].installId.ToString() } else { "INS-001" }
    $kw = $firstId.Substring(0, [Math]::Min(5, $firstId.Length))
    $rs = Safe-Get -uri "$base/install-records?page=1&pageSize=20&keyword=$kw" -headers $h
    $irl = if ($rs.data -and $rs.data.list) { $rs.data.list } else { $rs.data }
    if ($rs.http -eq 200 -and $rs.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
    Log-Result -task "T5" -id "4b" -endpoint "GET /install-records?keyword=$kw" -http $rs.http -code $rs.code -keyData "count=$(@($irl).Count)" -status $st
}

Write-Host ""
Write-Host "====== Task 6: Alerts + Feedbacks ======"

# 6-1 GET /alerts
Write-Host ""
Write-Host "--- 6-1 GET /alerts ---"
$r = Safe-Get -uri "$base/alerts?page=1&pageSize=20" -headers $h
$alerts = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$alen = @($alerts).Count
$afields = "N/A"
if ($alen -gt 0) {
    $afields = @($alerts[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name) -join ","
}
if ($r.http -eq 200 -and $r.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T6" -id "1" -endpoint "GET /alerts?page=1&pageSize=20" -http $r.http -code $r.code -keyData "len=$alen, fields=$afields" -status $st

$r2 = Safe-Get -uri "$base/alerts?page=1&pageSize=20&type=pressure_high" -headers $h
$alerts2 = if ($r2.data -and $r2.data.list) { $r2.data.list } else { $r2.data }
if ($r2.http -eq 200 -and $r2.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T6" -id "1b" -endpoint "GET /alerts?type=pressure_high" -http $r2.http -code $r2.code -keyData "count=$(@($alerts2).Count)" -status $st

$r3 = Safe-Get -uri "$base/alerts?page=1&pageSize=20&status=pending" -headers $h
$pendingAlerts = if ($r3.data -and $r3.data.list) { $r3.data.list } else { $r3.data }
if ($r3.http -eq 200 -and $r3.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T6" -id "1c" -endpoint "GET /alerts?status=pending" -http $r3.http -code $r3.code -keyData "pendingCount=$(@($pendingAlerts).Count)" -status $st

# 6-2 POST process alert
Write-Host ""
Write-Host "--- 6-2 POST process alert ---"
$pendingAlertsCount = @($pendingAlerts).Count
if ($pendingAlertsCount -gt 0) {
    $aid = if ($pendingAlerts[0].alertId) { $pendingAlerts[0].alertId } else { $pendingAlerts[0].id }
    $pr = Safe-Post -uri "$base/alerts/$aid/process" -headers $h -body "{}"
    if ($pr.http -eq 200 -and $pr.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
    Log-Result -task "T6" -id "2" -endpoint "POST /alerts/{id}/process" -http $pr.http -code $pr.code -keyData "alertId=$aid, msg=$($pr.msg)" -status $st
    $vr = Safe-Get -uri "$base/alerts?page=1&pageSize=20&status=processed" -headers $h
    $va = if ($vr.data -and $vr.data.list) { $vr.data.list } else { $vr.data }
    $found = @($va | Where-Object { ($_.alertId -eq $aid) -or ($_.id -eq $aid) }).Count -gt 0
    if ($found) { $st = "PASS" } else { $st = "WARN" }
    Log-Result -task "T6" -id "2b" -endpoint "GET /alerts verify processed" -http $vr.http -code $vr.code -keyData "alertId=$aid foundInProcessed=$found" -status $st
} else {
    Log-Result -task "T6" -id "2" -endpoint "POST /alerts/{id}/process" -http 0 -code -1 -keyData "no pending alerts" -status "WARN"
}

# 6-3 GET /feedbacks
Write-Host ""
Write-Host "--- 6-3 GET /feedbacks ---"
$r = Safe-Get -uri "$base/feedbacks" -headers $h
$fbs = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$flen = @($fbs).Count
if ($r.http -eq 200 -and $r.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
if ($flen -ge 10) { $extra = "(>=10 known seed legacy)" } else { $extra = "" }
Log-Result -task "T6" -id "3" -endpoint "GET /feedbacks" -http $r.http -code $r.code -keyData "len=$flen $extra" -status $st

$pendingFb = @($fbs | Where-Object { $_.status -eq "pending" })
$fbFields = "N/A"
if ($flen -gt 0) {
    $fbFields = @($fbs[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name) -join ","
}
if ($pendingFb.Count -gt 0) { $st = "PASS" } else { $st = "WARN" }
Log-Result -task "T6" -id "3b" -endpoint "GET /feedbacks fields" -http $r.http -code $r.code -keyData "pendingCount=$($pendingFb.Count), fields=$fbFields" -status $st

# 6-4 POST process feedback
Write-Host ""
Write-Host "--- 6-4 POST process feedback ---"
$fid = $null
if ($pendingFb.Count -gt 0) {
    $fid = if ($pendingFb[0].feedbackId) { $pendingFb[0].feedbackId } else { $pendingFb[0].id }
    $fbody = @{replyContent="验收测试回复：已收到反馈，将跟进处理。-Ella T052"} | ConvertTo-Json
    $fpr = Safe-Post -uri "$base/feedbacks/$fid/process" -headers $h -body $fbody
    if ($fpr.http -eq 200 -and $fpr.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
    Log-Result -task "T6" -id "4" -endpoint "POST /feedbacks/{id}/process" -http $fpr.http -code $fpr.code -keyData "fbId=$fid, msg=$($fpr.msg)" -status $st
    $vr = Safe-Get -uri "$base/feedbacks" -headers $h
    $vf = if ($vr.data -and $vr.data.list) { $vr.data.list } else { $vr.data }
    $found = @($vf | Where-Object { (($_.feedbackId -eq $fid) -or ($_.id -eq $fid)) -and ($_.status -ne "pending") })
    if ($found.Count -gt 0) { $st = "PASS" } else { $st = "WARN" }
    Log-Result -task "T6" -id "4b" -endpoint "GET /feedbacks verify status" -http $vr.http -code $vr.code -keyData "fbId=$fid statusChanged=$($found.Count -gt 0)" -status $st
} else {
    Log-Result -task "T6" -id "4" -endpoint "POST /feedbacks/{id}/process" -http 0 -code -1 -keyData "no pending feedbacks" -status "WARN"
}

# 6-5 feedback detail content length
Write-Host ""
Write-Host "--- 6-5 feedback detail ---"
if ($flen -gt 0) {
    $detailFb = $null
    if ($pendingFb.Count -gt 0 -and $fid -and $vf) {
        $detailFb = @($vf | Where-Object { $_.feedbackId -eq $fid -or $_.id -eq $fid })
        if ($detailFb.Count -gt 0) { $detailFb = $detailFb[0] } else { $detailFb = $fbs[0] }
    } else {
        $detailFb = $fbs[0]
    }
    $contentLen = if ($detailFb.content) { $detailFb.content.Length } else { 0 }
    $replyLen = if ($detailFb.replyContent) { $detailFb.replyContent.Length } else { 0 }
    $contentPreview = if ($detailFb.content) { $detailFb.content.Substring(0, [Math]::Min(60, $detailFb.content.Length)) } else { "(empty)" }
    $replyPreview = if ($detailFb.replyContent) { $detailFb.replyContent.Substring(0, [Math]::Min(60, $detailFb.replyContent.Length)) } else { "(empty)" }
    Log-Result -task "T6" -id "5" -endpoint "Feedback content detail" -http 200 -code 0 -keyData "contentLen=$contentLen, replyLen=$replyLen | content[:60]=$contentPreview | reply[:60]=$replyPreview" -status "PASS"
}

Write-Host ""
Write-Host "====== Task 7: Doctors + Orthosis ======"

# 7-1 GET /doctors
Write-Host ""
Write-Host "--- 7-1 GET /doctors ---"
$r = Safe-Get -uri "$base/doctors" -headers $h
$docs = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$dlen = @($docs).Count
$dfields = "N/A"
if ($dlen -gt 0) {
    $dfields = @($docs[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name) -join ","
}
if ($r.http -eq 200 -and $r.code -eq 0 -and $dlen -ge 3) { $st = "PASS" }
elseif ($r.http -eq 200 -and $r.code -eq 0) { $st = "WARN" }
else { $st = "FAIL" }
Log-Result -task "T7" -id "1" -endpoint "GET /doctors" -http $r.http -code $r.code -keyData "len=$dlen, fields=$dfields" -status $st

# 7-2 route diff note
Log-Result -task "T7" -id "2" -endpoint "Route diff(non-API)" -http 200 -code 0 -keyData "ops admin has ~12 biz routes, no standalone doctor mgmt page (missing route diff), only via API+Dashboard ranking+patient detail mapping" -status "WARN"

# find valid patientId
$patientId = "P20260001"
if ($list -and @($list).Count -gt 0 -and $list[0].patientId) {
    $patientId = $list[0].patientId.ToString()
    Write-Host "Using patientId from device: $patientId"
}

# 7-3 GET orthosis-plans
Write-Host ""
Write-Host "--- 7-3 GET orthosis-plans (patient=$patientId) ---"
$r = Safe-Get -uri "$base/patients/$patientId/orthosis-plans" -headers $h
$ops = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$olen = @($ops).Count
$ofields = "N/A"
if ($olen -gt 0) {
    $ofields = @($ops[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name) -join ","
}
if ($r.http -eq 200 -and $r.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
if ($olen -ge 10) { $extra = "(>=10 known seed legacy)" } else { $extra = "" }
Log-Result -task "T7" -id "3" -endpoint "GET /patients/{id}/orthosis-plans" -http $r.http -code $r.code -keyData "patient=$patientId, len=$olen $extra, fields=$ofields" -status $st

# 7-4 POST orthosis-plan
Write-Host ""
Write-Host "--- 7-4 POST orthosis-plan ---"
$opbody = @{content="Acceptance test orthosis plan v1 - wear 22h daily, pressure threshold 180N. T052 Ella"} | ConvertTo-Json
$opr = Safe-Post -uri "$base/patients/$patientId/orthosis-plans" -headers $h -body $opbody
$newPlanId = $null
$newVersion = "N/A"
if ($opr.data) {
    $newPlanId = if ($opr.data.planId) { $opr.data.planId } else { $opr.data.id }
    if ($opr.data.version) { $newVersion = $opr.data.version }
}
if ($opr.http -eq 200 -and $opr.code -eq 0 -and $newPlanId) { $st = "PASS" }
elseif ($opr.http -eq 200 -and $opr.code -eq 0) { $st = "WARN" }
else { $st = "FAIL" }
Log-Result -task "T7" -id "4" -endpoint "POST /patients/{id}/orthosis-plans" -http $opr.http -code $opr.code -keyData "patient=$patientId, planId=$newPlanId, version=$newVersion, msg=$($opr.msg)" -status $st

# 7-5 GET feeling-logs
Write-Host ""
Write-Host "--- 7-5 GET feeling-logs ---"
$r = Safe-Get -uri "$base/patients/$patientId/feeling-logs" -headers $h
$fls = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$flen2 = @($fls).Count
$firstFeeling = "N/A"
$firstTs = "N/A"
if ($flen2 -gt 0) {
    if ($fls[0].feeling) { $firstFeeling = $fls[0].feeling } else { $firstFeeling = ($fls[0] | ConvertTo-Json -Depth 3 -Compress) }
    if ($fls[0].timestamp) { $firstTs = $fls[0].timestamp } elseif ($fls[0].createdAt) { $firstTs = $fls[0].createdAt }
}
if ($r.http -eq 200 -and $r.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T7" -id "5" -endpoint "GET /patients/{id}/feeling-logs" -http $r.http -code $r.code -keyData "patient=$patientId, len=$flen2, firstFeeling=$firstFeeling, firstTs=$firstTs" -status $st

# 7-6 GET health-reports
Write-Host ""
Write-Host "--- 7-6 GET health-reports ---"
$r = Safe-Get -uri "$base/patients/$patientId/health-reports" -headers $h
$hrs = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$hlen = @($hrs).Count
$firstMetrics = "N/A"
$firstAdvice = "N/A"
if ($hlen -gt 0) {
    if ($hrs[0].metrics) {
        $m = $hrs[0].metrics | ConvertTo-Json -Depth 3 -Compress
    } else {
        $m = $hrs[0] | ConvertTo-Json -Depth 3 -Compress
    }
    $firstMetrics = $m.Substring(0, [Math]::Min(120, $m.Length))
    if ($hrs[0].advice) {
        $firstAdvice = $hrs[0].advice.Substring(0, [Math]::Min(60, $hrs[0].advice.Length))
    }
}
if ($r.http -eq 200 -and $r.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T7" -id "6" -endpoint "GET /patients/{id}/health-reports" -http $r.http -code $r.code -keyData "patient=$patientId, len=$hlen, metrics[:120]=$firstMetrics, advice[:60]=$firstAdvice" -status $st

Write-Host ""
Write-Host "====== Task 8: Permissions + System Config ======"

# 8-1 GET /admin/roles
Write-Host ""
Write-Host "--- 8-1 GET /admin/roles ---"
$r = Safe-Get -uri "$base/admin/roles" -headers $h
$roles = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$rlen = @($roles).Count
$roleInfo = @()
foreach ($role in $roles) {
    $rn = if ($role.name) { $role.name } else { $role.roleName }
    $permCount = 0
    if ($role.permissions -is [array]) { $permCount = $role.permissions.Count }
    elseif ($role.permissions) {
        $p = @($role.permissions | Get-Member -MemberType NoteProperty)
        $permCount = $p.Count
    }
    $scopeTxt = "N/A"
    if ($role.scope) { $scopeTxt = $role.scope }
    elseif ($role.dataScope) { $scopeTxt = $role.dataScope }
    $roleInfo += "$rn(perm=$permCount,scope=$scopeTxt)"
}
if ($r.http -eq 200 -and $r.code -eq 0 -and $rlen -ge 3) { $st = "PASS" }
elseif ($r.http -eq 200 -and $r.code -eq 0) { $st = "WARN" }
else { $st = "FAIL" }
Log-Result -task "T8" -id "1" -endpoint "GET /admin/roles" -http $r.http -code $r.code -keyData "len=$rlen, roles=$($roleInfo -join ' | ')" -status $st

# 8-2 GET /admin/settings backup
Write-Host ""
Write-Host "--- 8-2 GET /admin/settings (backup) ---"
$r = Safe-Get -uri "$base/admin/settings" -headers $h
$settings = $r.data
$origDailyWear = if ($settings -and $settings.PSObject.Properties["dailyWearTargetHours"]) { $settings.dailyWearTargetHours } elseif ($settings -and $settings.PSObject.Properties["daily_wear_target_hours"]) { $settings.daily_wear_target_hours } else { "N/A" }
$pressureHigh = if ($settings -and $settings.PSObject.Properties["pressureHighThresholdN"]) { $settings.pressureHighThresholdN } elseif ($settings -and $settings.PSObject.Properties["pressure_high_threshold_n"]) { $settings.pressure_high_threshold_n } else { "N/A" }
$pressureFluc = if ($settings -and $settings.PSObject.Properties["pressureFluctuationPct"]) { $settings.pressureFluctuationPct } elseif ($settings -and $settings.PSObject.Properties["pressure_fluctuation_pct"]) { $settings.pressure_fluctuation_pct } else { "N/A" }
$wearInt = if ($settings -and $settings.PSObject.Properties["wearInterruptMinutes"]) { $settings.wearInterruptMinutes } elseif ($settings -and $settings.PSObject.Properties["wear_interrupt_minutes"]) { $settings.wear_interrupt_minutes } else { "N/A" }
$sensorDrift = if ($settings -and $settings.PSObject.Properties["sensorDriftN"]) { $settings.sensorDriftN } elseif ($settings -and $settings.PSObject.Properties["sensor_drift_n"]) { $settings.sensor_drift_n } else { "N/A" }
$wifiListLen = 0
if ($settings -and $settings.PSObject.Properties["wifiPresets"] -and $settings.wifiPresets -is [array]) { $wifiListLen = $settings.wifiPresets.Count }
elseif ($settings -and $settings.PSObject.Properties["wifi_presets"] -and $settings.wifi_presets -is [array]) { $wifiListLen = $settings.wifi_presets.Count }
if ($r.http -eq 200 -and $r.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T8" -id "2" -endpoint "GET /admin/settings (backup)" -http $r.http -code $r.code -keyData "dailyWear=$origDailyWear, pressureHigh=$pressureHigh, pressureFluc=$pressureFluc, wearInt=$wearInt, sensorDrift=$sensorDrift, wifiPresets=$wifiListLen" -status $st

$origSettingsJson = $null
if ($r.raw -and $r.raw.data) {
    $origSettingsJson = $r.raw.data | ConvertTo-Json -Depth 10 -Compress
    Write-Host "ORIG SETTINGS BACKUP LEN: $($origSettingsJson.Length) chars"
}

# 8-3 PUT modify dailyWear=21
Write-Host ""
Write-Host "--- 8-3 PUT settings (dailyWear=21) ---"
$pr_http = 0
$pr_code = -1
$pr_msg = "N/A"
if ($r.raw -and $r.raw.data) {
    $modSettings = $r.raw.data
    if ($modSettings.PSObject.Properties["dailyWearTargetHours"]) {
        $modSettings.dailyWearTargetHours = 21
    } elseif ($modSettings.PSObject.Properties["daily_wear_target_hours"]) {
        $modSettings.daily_wear_target_hours = 21
    }
    $modBody = $modSettings | ConvertTo-Json -Depth 10
    $pr = Safe-Put -uri "$base/admin/settings" -headers $h -body $modBody
    $pr_http = $pr.http
    $pr_code = $pr.code
    $pr_msg = $pr.msg
}
if ($pr_http -eq 200 -and $pr_code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T8" -id "3" -endpoint "PUT /admin/settings (dailyWear=21)" -http $pr_http -code $pr_code -keyData "msg=$pr_msg" -status $st

# 8-4 GET verify 21
Write-Host ""
Write-Host "--- 8-4 GET verify dailyWear=21 ---"
$r2 = Safe-Get -uri "$base/admin/settings" -headers $h
$s2 = $r2.data
$curDaily = if ($s2 -and $s2.PSObject.Properties["dailyWearTargetHours"]) { $s2.dailyWearTargetHours } elseif ($s2 -and $s2.PSObject.Properties["daily_wear_target_hours"]) { $s2.daily_wear_target_hours } else { "N/A" }
if ($r2.http -eq 200 -and $r2.code -eq 0 -and [string]$curDaily -eq "21") { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T8" -id "4" -endpoint "GET /admin/settings (verify 21)" -http $r2.http -code $r2.code -keyData "dailyWear=$curDaily (expect 21)" -status $st

# 8-5 PUT restore 22
Write-Host ""
Write-Host "--- 8-5 PUT restore dailyWear=22 ---"
$rr_http = 0
$rr_code = -1
$rr_msg = "N/A"
if ($origSettingsJson) {
    $rr = Safe-Put -uri "$base/admin/settings" -headers $h -body $origSettingsJson
    $rr_http = $rr.http
    $rr_code = $rr.code
    $rr_msg = $rr.msg
}
if ($rr_http -eq 200 -and $rr_code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T8" -id "5" -endpoint "PUT /admin/settings (restore 22)" -http $rr_http -code $rr_code -keyData "msg=$rr_msg" -status $st

$r3 = Safe-Get -uri "$base/admin/settings" -headers $h
$s3 = $r3.data
$finalDaily = if ($s3 -and $s3.PSObject.Properties["dailyWearTargetHours"]) { $s3.dailyWearTargetHours } elseif ($s3 -and $s3.PSObject.Properties["daily_wear_target_hours"]) { $s3.daily_wear_target_hours } else { "N/A" }
if ($r3.http -eq 200 -and $r3.code -eq 0 -and [string]$finalDaily -eq "22") { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T8" -id "5b" -endpoint "GET /admin/settings (verify 22 restored)" -http $r3.http -code $r3.code -keyData "dailyWear=$finalDaily (expect 22)" -status $st

# 8-6 GET /admin/notify-rules
Write-Host ""
Write-Host "--- 8-6 GET /admin/notify-rules ---"
$r = Safe-Get -uri "$base/admin/notify-rules" -headers $h
$nrs = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$nrlen = @($nrs).Count
$nrInfo = @()
foreach ($nr in $nrs) {
    $ntype = if ($nr.type) { $nr.type } else { $nr.alertType }
    $ch = if ($nr.channels -is [array]) { $nr.channels.Count } else { 0 }
    $nt = if ($nr.notifyTargets -is [array]) { $nr.notifyTargets.Count } else { 0 }
    $nrInfo += "$ntype(ch=$ch,targets=$nt)"
}
if ($r.http -eq 200 -and $r.code -eq 0 -and $nrlen -ge 4) { $st = "PASS" }
elseif ($r.http -eq 200 -and $r.code -eq 0) { $st = "WARN" }
else { $st = "FAIL" }
Log-Result -task "T8" -id "6" -endpoint "GET /admin/notify-rules" -http $r.http -code $r.code -keyData "len=$nrlen, types=$($nrInfo -join ' | ')" -status $st

# 8-7 GET /admin/notification-logs
Write-Host ""
Write-Host "--- 8-7 GET /admin/notification-logs ---"
$r = Safe-Get -uri "$base/admin/notification-logs?page=1&pageSize=10" -headers $h
$nls = if ($r.data -and $r.data.list) { $r.data.list } else { $r.data }
$nllen = @($nls).Count
$nlfields = "N/A"
if ($nllen -gt 0) {
    $nlfields = @($nls[0] | Get-Member -MemberType NoteProperty | Select-Object -ExpandProperty Name) -join ","
}
if ($r.http -eq 200 -and $r.code -eq 0) { $st = "PASS" } else { $st = "FAIL" }
Log-Result -task "T8" -id "7" -endpoint "GET /admin/notification-logs?page=1&pageSize=10" -http $r.http -code $r.code -keyData "len=$nllen, fields=$nlfields" -status $st

# Output
Write-Host ""
Write-Host ""
Write-Host "================ ACCEPTANCE RESULT SUMMARY ================"
$results | Format-Table -AutoSize Task, ID, Endpoint, HTTP, Code, Status, KeyData -Wrap | Out-String -Width 500

Write-Host ""
Write-Host ""
Write-Host "================ JSON OUTPUT ================"
$results | ConvertTo-Json -Depth 5

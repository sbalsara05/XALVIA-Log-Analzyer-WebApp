param(
    [Parameter(Mandatory)][string]$Token,
    [string]$BaseUrl = "http://localhost:8080",
    [string]$InviteEmail = "testuser@example.com"
)

$headers = @{
    Authorization  = "Bearer $Token"
    "Content-Type" = "application/json"
}

function Step($label) { Write-Host "`n--- $label ---" -ForegroundColor Cyan }
function Ok($msg)     { Write-Host "  OK   $msg" -ForegroundColor Green }
function Fail($msg)   { Write-Host "  FAIL $msg" -ForegroundColor Red }

function Invoke($method, $path, $body = $null) {
    $url = "$BaseUrl$path"
    $params = @{ Method = $method; Uri = $url; Headers = $headers; ErrorAction = "Stop" }
    if ($body) { $params.Body = ($body | ConvertTo-Json -Compress) }
    try {
        $resp = Invoke-RestMethod @params
        return $resp
    } catch {
        $status = $_.Exception.Response.StatusCode.value__
        $detail = $_.ErrorDetails.Message
        throw "HTTP $status - $detail"
    }
}

# 1. Health check
Step "1. Health check (no auth)"
try {
    $r = Invoke-RestMethod "$BaseUrl/health"
    Ok $r
} catch { Fail $_ }

# 2. Create root org
Step "2. POST /api/orgs (super admin - create root org)"
$orgId = $null
try {
    $r = Invoke POST /api/orgs @{ name = "Ricovr" }
    $orgId = $r.id
    Ok "Created org id=$orgId  name=$($r.name)"
} catch { Fail $_ }

# 3. List orgs
Step "3. GET /api/orgs (list orgs you can admin)"
try {
    $r = Invoke GET /api/orgs
    Ok "Orgs returned: $($r.Count)"
    $r | ForEach-Object { Write-Host "    id=$($_.id)  name=$($_.name)" }
} catch { Fail $_ }

# 4. Create sub-org
if ($orgId) {
    Step "4. POST /api/orgs/$orgId/suborgs (create child org)"
    $subOrgId = $null
    try {
        $r = Invoke POST "/api/orgs/$orgId/suborgs" @{ name = "Hospital A" }
        $subOrgId = $r.id
        Ok "Created sub-org id=$subOrgId  name=$($r.name)  parent=$($r.parent_org_id)"
    } catch { Fail $_ }
} else {
    Write-Host "`n--- 4. Skipped (no org id from step 2) ---" -ForegroundColor Yellow
}

# 5. Add a user by email (Firebase lookup - no email sent)
if ($orgId) {
    Step "5. POST /api/orgs/$orgId/users (add $InviteEmail - silent grant)"
    try {
        $r = Invoke POST "/api/orgs/$orgId/users" @{
            email      = $InviteEmail
            role       = "user"
            can_read   = $true
            can_upload = $false
        }
        Ok "Upserted user_id=$($r.user_id)"
    } catch { Fail $_ }
} else {
    Write-Host "`n--- 5. Skipped (no org id from step 2) ---" -ForegroundColor Yellow
}

# 6. List org users
if ($orgId) {
    Step "6. GET /api/orgs/$orgId/users (list members)"
    try {
        $r = Invoke GET "/api/orgs/$orgId/users"
        Ok "Members returned: $($r.Count)"
        $r | ForEach-Object {
            Write-Host "    uid=$($_.user_id)  email=$($_.email)  role=$($_.role)  read=$($_.can_read)  upload=$($_.can_upload)"
        }
    } catch { Fail $_ }
} else {
    Write-Host "`n--- 6. Skipped (no org id from step 2) ---" -ForegroundColor Yellow
}

Write-Host "`nDone." -ForegroundColor Cyan

function Get-OwnedFirewallRule {
    $rules = @(Get-NetFirewallRule -PolicyStore ActiveStore -ErrorAction Stop | Where-Object { $_.Name -ceq $script:Plan.rule_name })
    if ($rules.Count -gt 1) { throw '같은 이름의 규칙이 여러 개입니다. 자동 변경하지 않습니다.' }
    if ($rules.Count -eq 0) { return $null }
    $rule = $rules[0]
    $apps = @($rule | Get-NetFirewallApplicationFilter -ErrorAction Stop)
    if ($rule.Group -cne $script:Plan.rule_group -or $apps.Count -ne 1 -or
        -not [string]::Equals($apps[0].Program, $script:Plan.program, [StringComparison]::OrdinalIgnoreCase)) {
        throw '규칙 이름이 다른 설정과 충돌합니다. 기존 규칙은 변경하지 않았습니다.'
    }
    return $rule
}
function Set-OwnedFirewallRule {
    $port = [int]$script:Plan.port
    if (($port -ne 443 -and $port -lt 1024) -or $port -gt 65535) { throw '외부 HTTPS 주소의 포트를 먼저 확인하세요.' }
    if (-not (Test-Path -LiteralPath $script:Plan.program -PathType Leaf)) { throw '설치된 채팅 서버 실행 파일을 찾을 수 없습니다.' }
    $existing = Get-OwnedFirewallRule
    $params = @{
        PolicyStore='ActiveStore'; Name=$script:Plan.rule_name; Direction='Inbound'
        Action='Allow'; Enabled='True'; Profile='Any'; Protocol='TCP'; LocalPort=[string]$port
        RemotePort='Any'; Program=[string]$script:Plan.program; LocalAddress='Any'; RemoteAddress='Any'
        EdgeTraversalPolicy='Block'; ErrorAction='Stop'
    }
    if ($null -eq $existing) {
        New-NetFirewallRule @params -DisplayName 'OnTech Chat Server - HTTPS TCP' -Group $script:Plan.rule_group -Description 'OnTech 관리 도구로 승인한 단일 TCP 수신 규칙' | Out-Null
    } else {
        Set-NetFirewallRule @params | Out-Null
    }
    $check = Get-OwnedFirewallRule
    $ports = @($check | Get-NetFirewallPortFilter -ErrorAction Stop)
    if ($null -eq $check -or [string]$check.Enabled -ne 'True' -or [string]$check.Action -ne 'Allow' -or
        [string]$check.Direction -ne 'Inbound' -or $ports.Count -ne 1 -or
        ($ports[0].LocalPort -join ',') -ne [string]$port -or [string]$ports[0].Protocol -notin @('TCP','6')) {
        throw '저장 후 규칙 검증에 실패했습니다. 진단 결과와 Windows 방화벽을 확인하세요.'
    }
}
function Remove-OwnedFirewallRule {
    $existing = Get-OwnedFirewallRule
    if ($null -ne $existing) {
        $existing | Remove-NetFirewallRule -ErrorAction Stop
    }
    if ($null -ne (Get-OwnedFirewallRule)) { throw '이 앱의 허용 규칙을 제거하지 못했습니다.' }
}

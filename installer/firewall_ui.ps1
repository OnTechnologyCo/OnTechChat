Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'

if ([string]::IsNullOrWhiteSpace($env:ONTECH_FIREWALL_PLAN)) {
    throw '방화벽 설정 정보가 없습니다.'
}
try {
    $script:Plan = $env:ONTECH_FIREWALL_PLAN | ConvertFrom-Json -ErrorAction Stop
} catch {
    throw '방화벽 설정 정보를 읽지 못했습니다.'
}
$mode = [string]$env:ONTECH_FIREWALL_MODE
if ($mode -eq 'remove') {
    Remove-OwnedFirewallRule
    return
}
if ($mode -eq 'inspect') {
    $existing = Get-OwnedFirewallRule
    if ($null -ne $existing) { Write-Output 'managed-rule-present' }
    return
}
if ($mode -cne 'ui') { throw '지원하지 않는 방화벽 작업입니다.' }

Add-Type -AssemblyName System.Windows.Forms
$dialogPolicy = 'YesNo','Warning','Button2'
$nl = [Environment]::NewLine
$portText = if ([int]$script:Plan.port -gt 0) { [string]$script:Plan.port } else { '결정되지 않음' }
$text = "온테크 채팅 서버 전용 Windows 방화벽 규칙을 저장할까요?" + $nl + $nl +
    "외부 HTTPS 주소: $($script:Plan.public_url)" + $nl +
    "허용 TCP 포트: $portText" + $nl +
    "프로그램: $($script:Plan.program)" + $nl + $nl +
    "$($script:Plan.warning)" + $nl + $nl +
    "다른 프로그램의 방화벽 규칙은 변경하지 않습니다."

$choice = [System.Windows.Forms.MessageBox]::Show(
    $text, '온테크 채팅 방화벽',
    [System.Windows.Forms.MessageBoxButtons]::YesNo,
    [System.Windows.Forms.MessageBoxIcon]::Warning,
    [System.Windows.Forms.MessageBoxDefaultButton]::Button2)
if ($choice -ne [System.Windows.Forms.DialogResult]::Yes) { return }

try {
    Set-OwnedFirewallRule
} catch {
    [System.Windows.Forms.MessageBox]::Show(
        "규칙 저장 실패" + $nl + $nl + $_.Exception.Message,
        '온테크 채팅 방화벽',
        [System.Windows.Forms.MessageBoxButtons]::OK,
        [System.Windows.Forms.MessageBoxIcon]::Error) | Out-Null
    throw
}
[System.Windows.Forms.MessageBox]::Show(
    "OnTech Chat 전용 TCP 규칙을 저장하고 다시 확인했습니다." + $nl + $nl +
    "이 결과는 인터넷 접속 성공을 의미하지 않습니다. 공유기 포트 전달, HTTPS 프록시와 인증서 설정은 별도로 확인하세요.",
    '온테크 채팅 방화벽',
    [System.Windows.Forms.MessageBoxButtons]::OK,
    [System.Windows.Forms.MessageBoxIcon]::Information) | Out-Null

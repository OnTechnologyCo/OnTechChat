param([ValidateSet('All','Android','Emulator','Server','Tests')][string]$Target='All', [ValidateSet('Core','Full')][string]$Validation='Core')
$ErrorActionPreference='Stop'
$root=$PSScriptRoot
if($root -ne 'D:\OnMakeSrc\OnTechChat') { throw 'Use D:\OnMakeSrc\OnTechChat\build.ps1 only.' }
$config=Get-Content "$root\build.config.json" -Raw | ConvertFrom-Json
[xml]$appProject=Get-Content "$root\android\OnTechChat\OnTechChat.csproj" -Raw
$androidVersion=[string]$appProject.Project.PropertyGroup.ApplicationDisplayVersion
$androidCode=[string]$appProject.Project.PropertyGroup.ApplicationVersion
$serverVersion=[regex]::Match((Get-Content "$root\installer\payload.go" -Raw),'const installerVersion = "([0-9.]+)"').Groups[1].Value
if($androidVersion -notmatch '^\d+\.\d+\.\d+$' -or $serverVersion -notmatch '^\d+\.\d+\.\d+$') { throw 'Invalid release version.' }
New-Item -ItemType Directory -Force "$root\.build\logs","$root\output" | Out-Null
try { $lock=[IO.File]::Open("$root\.build\project.lock",'OpenOrCreate','ReadWrite','None') }
catch { throw 'Another project task owns the lock. Do not delete it or start a second build.' }
$started=(Get-Date).ToString('o')
$success=$false
$originalLocation=Get-Location
$savedEnv=@{}
foreach($name in @('DOTNET_ROOT','PATH','JAVA_HOME','ANDROID_HOME','GOTOOLCHAIN','GOCACHE','CGO_ENABLED','GOOS','GOARCH')) {
    $savedEnv[$name]=[Environment]::GetEnvironmentVariable($name,'Process')
}
function Native([string]$exe,[string[]]$arguments,[string]$log) {
    $previous=$ErrorActionPreference
    $ErrorActionPreference='Continue'
    & $exe @arguments 2>&1 | Tee-Object -FilePath "$root\.build\logs\$log"
    $code=$LASTEXITCODE
    $ErrorActionPreference=$previous
    if($code -ne 0) { throw "$exe failed (exit $code). See .build\logs\$log" }
}
function Publish-Atomic([string]$source,[string]$destination) {
    $temp=$destination+'.new'
    Copy-Item -LiteralPath $source -Destination $temp -Force
    if(Test-Path $destination) { [IO.File]::Replace($temp,$destination,(Join-Path "$root\.build" ('previous-'+[IO.Path]::GetFileName($destination))),$true) }
    else { [IO.File]::Move($temp,$destination) }
}
function Publish-Release([string]$source,[string]$filename,[string]$pattern) {
    $destination=Join-Path "$root\output" $filename
    Publish-Atomic $source $destination
    Get-ChildItem "$root\output" -File -Filter $pattern | Where-Object Name -ne $filename | ForEach-Object {
        Move-Item -LiteralPath $_.FullName -Destination (Join-Path "$root\.build" ('previous-'+$_.Name)) -Force
    }
}
function Build-Android([string]$rid,[string]$flavor) {
    $project="$root\android\OnTechChat\OnTechChat.csproj"
    $arguments=@($project,'/restore','/t:SignAndroidPackage','/m:2','/v:minimal','/nologo',
        '/p:Configuration=Release',"/p:RuntimeIdentifier=$rid","/p:BuildFlavor=$flavor",
        '/p:OnTechCanonicalBuild=true',"/p:AndroidSdkDirectory=$($config.androidSdk)",
        "/p:JavaSdkDirectory=$($config.javaSdk)",'/p:EmbedAssembliesIntoApk=true',
        '/p:AndroidEnableFastDeployment=false')
    Native $config.msbuild $arguments "android-$flavor-build.log"
    $apks=@(Get-ChildItem "$root\.build\bin\$flavor\OnTechChat" -Recurse -Filter '*-Signed.apk')
    if($apks.Count -ne 1) { throw "Expected one signed $rid APK; found $($apks.Count)" }
    $apk=$apks[0].FullName
    $bt=Join-Path $config.androidSdk "build-tools\$($config.androidBuildTools)"
    Native "$($config.javaSdk)\bin\java.exe" @('-jar',"$bt\lib\apksigner.jar",'verify','--verbose','--print-certs',$apk) "android-$flavor-signature.log"
    Native "$bt\zipalign.exe" @('-c','-P','16','4',$apk) "android-$flavor-alignment.log"
    Native "$bt\aapt2.exe" @('dump','badging',$apk) "android-$flavor-package.log" | Out-Null
    $destination=if($rid -eq 'android-arm64') { "$root\output\OnTechChat-v$androidVersion-arm64.apk" } else { "$root\.build\OnTechChat-emulator-x64.apk" }
    if($rid -eq 'android-arm64') { Publish-Release $apk "OnTechChat-v$androidVersion-arm64.apk" 'OnTechChat*.apk' }
    else { Publish-Atomic $apk $destination }
    Write-Output "VERIFIED APK: $destination"
}
function Prepare-SetupAPKBundle {
    $apk="$root\output\OnTechChat-v$androidVersion-arm64.apk"
    if(!(Test-Path $apk)) { throw "Server Setup requires $apk. Run build.ps1 -Target Android or -Target All first." }
    $bt=Join-Path $config.androidSdk "build-tools\$($config.androidBuildTools)"
    $sigOutput=@(& "$($config.javaSdk)\bin\java.exe" -jar "$bt\lib\apksigner.jar" verify --verbose --print-certs $apk 2>&1)
    if($LASTEXITCODE -ne 0) { throw 'Official Android APK signature verification failed.' }
    $sigText=$sigOutput -join "`n"
    $signer=[regex]::Match($sigText,'Signer #1 certificate SHA-256 digest:\s*([0-9a-fA-F]{64})')
    $signerCount=[regex]::Match($sigText,'Number of signers:\s*(\d+)')
    if(!$signer.Success -or !$signerCount.Success -or $signerCount.Groups[1].Value -ne '1') { throw 'Expected exactly one verified Android APK signer.' }
    $badging=@(& "$bt\aapt2.exe" dump badging $apk 2>&1)
    if($LASTEXITCODE -ne 0) { throw 'Official Android APK package inspection failed.' }
    $badgeText=$badging -join "`n"
    $package=[regex]::Match($badgeText,"package: name='([^']+)' versionCode='(\d+)' versionName='([^']+)'")
    $minSdk=[regex]::Match($badgeText,"minSdkVersion:'(\d+)'")
    if(!$package.Success -or !$minSdk.Success) { throw 'Android APK metadata is incomplete.' }
    if($package.Groups[1].Value -ne 'com.ontechdev.chat' -or $package.Groups[2].Value -ne $androidCode -or $package.Groups[3].Value -ne $androidVersion) {
        throw "Android APK does not match project version/package. APK=$($package.Groups[1].Value) $($package.Groups[3].Value) ($($package.Groups[2].Value)); project=com.ontechdev.chat $androidVersion ($androidCode)."
    }
    & "$bt\zipalign.exe" -c -P 16 4 $apk | Out-Null
    if($LASTEXITCODE -ne 0) { throw 'Official Android APK zip alignment verification failed.' }
    $sha=(Get-FileHash $apk -Algorithm SHA256).Hash.ToLowerInvariant()
    $size=(Get-Item $apk).Length
    $bundle="$root\.build\setup-apk"
    if(Test-Path $bundle) { Remove-Item $bundle -Recurse -Force }
    New-Item -ItemType Directory -Force $bundle | Out-Null
    $filename="$sha.apk"
    Copy-Item -LiteralPath $apk -Destination (Join-Path $bundle $filename) -Force
    $release=[ordered]@{
        schema=1; package_name='com.ontechdev.chat'; version_code=[int64]$androidCode; version_name=$androidVersion
        filename=$filename; sha256=$sha; size=[int64]$size; min_sdk=[int]$minSdk.Groups[1].Value
        signing_cert_sha256=$signer.Groups[1].Value.ToLowerInvariant()
        notes="OnTech Chat Android $androidVersion ($androidCode) · 서버 Setup 자동 포함 APK"
    }
    $json=$release | ConvertTo-Json -Depth 4
    $manifest=Join-Path $bundle 'latest.json'
    [IO.File]::WriteAllText($manifest,$json,[Text.UTF8Encoding]::new($false))
    return [pscustomobject]@{Manifest=$manifest;APK=(Join-Path $bundle $filename);Filename=$filename;SHA256=$sha;Size=$size;Signer=$release.signing_cert_sha256}
}
function Test-Android {
    foreach($name in @('Tests','NotificationPolicy.Tests')) {
        Native $config.msbuild @("$root\android\$name\$name.csproj",'/restore','/t:Build','/v:minimal','/nologo','/p:Configuration=Release','/p:BuildFlavor=tests','/p:OnTechCanonicalBuild=true') "$name-build.log"
        Native $config.dotnet @("$root\.build\bin\tests\$name\Release\net10.0\$name.dll") "$name-test.log"
    }
}
function Test-Server {
    if($Validation -eq 'Core') {
        Set-Location "$root\server"
        Native (Join-Path $root $config.go) @('test','./...','-v','-timeout','5m','-run','Test(PersistentSession|HTTPUserAdministrationAndSessionRevocation|Location|AllFriends|AI|Agent|Mail|IMAP|Discovery|SelectedAuthentication|InvalidRecipient|RoomDelete|RoomLeave|ChatClear|AuthInboxClear)') 'server-core-test.log'
    } else {
        foreach($module in @('server','tray','installer')) {
            Set-Location "$root\$module"
            Native (Join-Path $root $config.go) @('test','./...') "$module-test.log"
        }
    }
    Set-Location $root
}
function Build-Server {
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $apkBundle=Prepare-SetupAPKBundle
    Write-Output "EMBEDDED APK: Android $androidVersion ($androidCode), SHA256 $($apkBundle.SHA256)"
    $zipPath="$root\.build\payload.zip"
    if(Test-Path $zipPath) { Remove-Item $zipPath }
    $zip=[IO.Compression.ZipFile]::Open($zipPath,'Create')
    try {
        $entries=@{
            'README.md'="$root\docs\SERVER-README.md"
            'INSTALL-WINDOWS.txt'="$root\docs\INSTALL-WINDOWS.txt"
            'config.example.json'="$root\config.example.json"
            'app.ico'="$root\tray\icons\running.ico"
            'apk-release/latest.json'=$apkBundle.Manifest
            ("apk-release/"+$apkBundle.Filename)=$apkBundle.APK
        }
        Get-ChildItem "$root\docs\server" -File | ForEach-Object { $entries["docs/$($_.Name)"]=$_.FullName }
        foreach($entry in $entries.GetEnumerator()) {
            [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip,$entry.Value,$entry.Key) | Out-Null
        }
    } finally { $zip.Dispose() }
    Copy-Item $zipPath "$root\installer\payload.zip" -Force
    Test-Server
    Set-Location "$root\installer"
    Native (Join-Path $root $config.go) @('build','-trimpath','-ldflags=-s -w -H=windowsgui','-o',"$root\.build\OnTechChat-Server-Setup-x64.exe",'.') 'server-build.log'
    Set-Location $root
    $exe="$root\.build\OnTechChat-Server-Setup-x64.exe"
    $pe=[IO.File]::ReadAllBytes($exe)
    $header=[BitConverter]::ToInt32($pe,0x3c)
    if([BitConverter]::ToUInt16($pe,$header+4) -ne 0x8664 -or [BitConverter]::ToUInt16($pe,$header+24+68) -ne 2) {
        throw 'The unified Windows executable must be x64 GUI subsystem (no command window).'
    }
    Native $exe @('--verify-payload') 'server-payload-check.log'
    Native $exe @('--version') 'server-version-check.log'
    Publish-Release $exe "OnTechChat-Server-Setup-v$serverVersion-x64.exe" 'OnTechChat-Server-Setup*.exe'
}
try {
    Set-Location $root
    foreach($path in @('releases','artifacts')) {
        if(Test-Path "$root\$path") { throw "Duplicate path detected: $path. Read AGENTS.md before proceeding." }
    }
    foreach($path in @($config.msbuild,$config.dotnet,(Join-Path $root $config.go))) {
        if(!(Test-Path $path)) { throw "Configured tool missing: $path" }
    }
    $env:DOTNET_ROOT=Split-Path $config.dotnet
    $env:JAVA_HOME=$config.javaSdk
    $env:ANDROID_HOME=$config.androidSdk
    $env:PATH=$env:DOTNET_ROOT+';'+$env:JAVA_HOME+'\bin;'+$env:PATH
    $env:GOTOOLCHAIN='local'
    $env:GOCACHE="$root\.build\go-cache"
    $env:CGO_ENABLED='0'; $env:GOOS='windows'; $env:GOARCH='amd64'
    [pscustomobject]@{Status='running';Target=$Target;PID=$PID;Started=$started} | ConvertTo-Json | Set-Content "$root\.build\task.json" -Encoding UTF8
    switch($Target) {
        'All' { Test-Android; Build-Android 'android-arm64' 'arm64'; Build-Server }
        'Android' { Test-Android; Build-Android 'android-arm64' 'arm64' }
        'Emulator' { Build-Android 'android-x64' 'x64' }
        'Server' { Build-Server }
        'Tests' { Test-Android; Test-Server }
    }
    $files=Get-ChildItem "$root\output" -File | Where-Object Extension -in '.apk','.exe'
    $manifest=@($files | ForEach-Object {
        [pscustomobject]@{File=$_.Name;Bytes=$_.Length;SHA256=(Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant();Modified=$_.LastWriteTime.ToString('o')}
    })
    $manifest | ConvertTo-Json -Depth 4 | Set-Content "$root\output\manifest.json" -Encoding UTF8
    $manifest | ForEach-Object { "$($_.SHA256)  $($_.File)" } | Set-Content "$root\output\SHA256SUMS.txt" -Encoding UTF8
    $success=$true
    Write-Output "BUILD SUCCESS: $Target"
} finally {
    Set-Location $originalLocation
    foreach($name in $savedEnv.Keys) { [Environment]::SetEnvironmentVariable($name,$savedEnv[$name],'Process') }
    [pscustomobject]@{Status=$(if($success){'completed'}else{'failed'});Target=$Target;Started=$started;Finished=(Get-Date).ToString('o');PID=$PID} | ConvertTo-Json | Set-Content "$root\.build\task.json" -Encoding UTF8
    $lock.Dispose()
}


param([int]$Repeats = 5)
$ErrorActionPreference = 'Stop'
$root = 'D:\Downloads\Lumen'
$env:GOCACHE = 'D:\tmp\phase7\gocache'
$env:CGO_ENABLED = '1'
$env:PATH = "$root\deps\runtime;" + $env:PATH
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$cases = @()
foreach ($file in Get-ChildItem "$root\spikes\.cache\heic" -File -Filter '*.heic') {
    foreach ($mode in 'thumbnail','render','wic') {
        $cases += [pscustomobject]@{ File=$file.FullName; Mode=$mode; Side=$(if ($mode -eq 'thumbnail') {1024} else {4096}) }
    }
}
foreach ($file in Get-ChildItem "$root\spikes\phase7\fixtures" -File | Where-Object Extension -In '.tif','.jpg') {
    $cases += [pscustomobject]@{ File=$file.FullName; Mode='thumbnail'; Side=1024 }
    foreach ($side in 1024,4096,6000) {
        $cases += [pscustomobject]@{ File=$file.FullName; Mode='render'; Side=$side }
    }
}
$rows = @()
foreach ($case in $cases) {
    $env:PHASE7_FILE = $case.File
    $env:PHASE7_MODE = $case.Mode
    $env:PHASE7_SIDE = [string]$case.Side
    for ($repeat=1; $repeat -le $Repeats; $repeat++) {
        $output = & "$root\spikes\phase7\probe.exe" '-test.run=^TestRender$' '-test.v' '-test.timeout=120s'
        $exitCode = $LASTEXITCODE
        $line = $output | Where-Object { $_ -like 'MEASURE *' } | Select-Object -First 1
        if (!$line) { throw "No measurement for $($case.File): $output" }
        $row = $line.Substring(8) | ConvertFrom-Json
        $row | Add-Member NoteProperty repeat $repeat
        $row | Add-Member NoteProperty exit_code $exitCode
        $rows += $row
        $row | ConvertTo-Json -Compress | Add-Content "$root\spikes\phase7\measurements.jsonl" -Encoding UTF8
    }
    Write-Output "Measured $([IO.Path]::GetFileName($case.File)) $($case.Mode) $($case.Side) ($Repeats runs)"
}
$rows | Export-Csv "$root\spikes\phase7\measurements.csv" -NoTypeInformation -Encoding UTF8
$summaries = foreach ($group in $rows | Group-Object file,mode,side) {
    $sorted = @($group.Group.ms | Sort-Object)
    $first = $group.Group[0]
    [pscustomobject]@{
        file=$first.file; mode=$first.mode; side=$first.side; w=$first.output_w; h=$first.output_h
        median_ms=$sorted[[int][math]::Floor($sorted.Count/2)]; min_ms=$sorted[0]; max_ms=$sorted[-1]
        median_decode_resize_ms=@($group.Group.decode_resize_ms | Sort-Object)[[int][math]::Floor($sorted.Count/2)]
        median_jpeg_encode_ms=@($group.Group.jpeg_encode_ms | Sort-Object)[[int][math]::Floor($sorted.Count/2)]
        peak_ws_mib=[math]::Round(($group.Group.peak_ws_bytes | Measure-Object -Maximum).Maximum/1MB,2)
        peak_commit_mib=[math]::Round(($group.Group.peak_commit_bytes | Measure-Object -Maximum).Maximum/1MB,2)
        max_output_bytes=($group.Group.output_bytes | Measure-Object -Maximum).Maximum
        failures=@($group.Group | Where-Object exit_code -ne 0).Count
    }
}
$summaries | Export-Csv "$root\spikes\phase7\summary.csv" -NoTypeInformation -Encoding UTF8
$summaries | Format-Table -AutoSize

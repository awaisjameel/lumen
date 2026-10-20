$ErrorActionPreference = 'Stop'
# The repository root, two folders above this script.
$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$env:PATH = "$root\deps\runtime;" + $env:PATH
$env:PHASE7_FILE = "$root\spikes\phase7\fixtures\enlarged-24mp-tiled.tif"
$rows = @()
foreach ($case in @(@{mode='thumbnail';side=1024},@{mode='render';side=1024},@{mode='render';side=4096},@{mode='render';side=6000})) {
    $env:PHASE7_MODE = $case.mode
    $env:PHASE7_SIDE = [string]$case.side
    for ($repeat=1; $repeat -le 5; $repeat++) {
        $output = & "$root\spikes\phase7\probe.exe" '-test.run=^TestRender$' '-test.v' '-test.timeout=120s'
        if ($LASTEXITCODE -ne 0) { throw "Tiled probe failed: $output" }
        $line = $output | Where-Object { $_ -like 'MEASURE *' }
        $row = $line.Substring(8) | ConvertFrom-Json
        $row | Add-Member NoteProperty repeat $repeat
        $row | Add-Member NoteProperty exit_code 0
        $rows += $row
    }
}
$rows | Export-Csv "$root\spikes\phase7\tiled-measurements.csv" -NoTypeInformation -Encoding UTF8
foreach ($g in $rows | Group-Object mode,side) {
    $s = @($g.Group.ms | Sort-Object)
    [pscustomobject]@{case=$g.Name;median_ms=$s[2];min_ms=$s[0];max_ms=$s[-1];peak_ws_mib=($g.Group.peak_ws_bytes|Measure-Object -Maximum).Maximum/1MB;peak_commit_mib=($g.Group.peak_commit_bytes|Measure-Object -Maximum).Maximum/1MB} | Format-List
}
$ffrows = @()
foreach ($file in 'autumn_1440x960.heic','iphone.heic') {
    foreach ($side in 1024,4096) {
        for ($repeat=1; $repeat -le 5; $repeat++) {
            $filter = "scale=w='min(iw,$side)':h='min(ih,$side)':force_original_aspect_ratio=decrease"
            $args = @('-hide_banner','-nostdin','-benchmark','-threads','1','-i',"$root\spikes\.cache\heic\$file",'-frames:v','1')
            if ($file -eq 'iphone.heic') { $args += @('-filter_complex',"[0:g:0]$filter") }
            else { $args += @('-vf',$filter) }
            $args += @('-threads','1','-q:v','2','-y',"$root\spikes\phase7\ffmpeg-$file-$side.jpg")
            # PowerShell 5.1 treats native stderr as error records. Preserve it as text.
            $ErrorActionPreference = 'Continue'
            $output = & "$root\deps\runtime\ffmpeg\ffmpeg.exe" @args 2>&1 | ForEach-Object ToString
            $exitCode = $LASTEXITCODE
            $ErrorActionPreference = 'Stop'
            $output | Set-Content "$root\spikes\phase7\ffmpeg-$file-$side-$repeat.txt"
            if ($exitCode -ne 0) { throw "FFmpeg probe failed: $output" }
            $time = ($output | Select-String 'rtime=(\d+(?:\.\d+)?)s').Matches.Groups[1].Value
            $rss = ($output | Select-String 'maxrss=(\d+)KiB').Matches.Groups[1].Value
            $ffrows += [pscustomobject]@{file=$file;side=$side;repeat=$repeat;ms=[double]$time*1000;peak_ws_mib=[double]$rss/1024}
        }
        Write-Output "Measured FFmpeg $file $side (5 runs)"
    }
}
$ffrows | Export-Csv "$root\spikes\phase7\ffmpeg-measurements.csv" -NoTypeInformation -Encoding UTF8
foreach ($g in $ffrows | Group-Object file,side) {
    $s=@($g.Group.ms|Sort-Object)
    [pscustomobject]@{case=$g.Name;median_ms=$s[2];min_ms=$s[0];max_ms=$s[-1];peak_ws_mib=($g.Group.peak_ws_mib|Measure-Object -Maximum).Maximum} | Format-List
}

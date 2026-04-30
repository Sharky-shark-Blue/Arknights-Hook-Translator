param(
    [string]$InputFile = ".\capture_raw.txt",
    [string]$OutputFile = ".\capture_decoded.txt"
)

Get-Content $InputFile -Wait | ForEach-Object {
    $line = $_

    if ($line -match '^\[unicode\]\s+(.+)$') {
        $u = $Matches[1]

        $decoded = [regex]::Replace($u, '\\u([0-9a-fA-F]{4})', {
            param($m)
            [char]([Convert]::ToInt32($m.Groups[1].Value, 16))
        })

        $decoded | Tee-Object -FilePath $OutputFile -Append
    }
}
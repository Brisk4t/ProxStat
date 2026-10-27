# Sends mock data to StatMonitorEPD over serial and prints anything the device replies.
#   .\tests\send-mock.ps1                      # COM10, tests\mock-data.txt
#   .\tests\send-mock.ps1 -Port COM5 -File other.txt
#   .\tests\send-mock.ps1 -Line '2|b=85'       # send single lines instead of the file
# Close any serial monitor on the port first.
param(
  [string]$Port = 'COM10',
  [string]$File = (Join-Path $PSScriptRoot 'mock-data.txt'),
  [string[]]$Line,
  [int]$Baud = 115200,
  [int]$ReplyMs = 500
)

$lines = if ($Line) { $Line } else { Get-Content $File | Where-Object { $_.Trim() -and -not $_.StartsWith('#') } }

$sp = New-Object System.IO.Ports.SerialPort $Port, $Baud, 'None', 8, 'One'
$sp.DtrEnable = $true # USB CDC on the Blue Pill / ESP32 only delivers data once DTR is set
$sp.NewLine = "`n"
try {
  $sp.Open()
  foreach ($l in $lines) {
    $sp.WriteLine($l)
    Start-Sleep -Milliseconds 20
  }
  Start-Sleep -Milliseconds $ReplyMs
  $reply = $sp.ReadExisting()
  Write-Host "Sent $(@($lines).Count) line(s) to $Port"
  if ($reply) { Write-Host "Device replied:`n$reply" }
}
finally {
  $sp.Close()
}

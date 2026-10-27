# Sends mock data to StatMonitorEPD over serial and prints anything the device replies.
#   .\tests\send-mock.ps1                          # COM10, all of tests\mock-data.txt
#   .\tests\send-mock.ps1 -Port COM5 -File other.txt
#   .\tests\send-mock.ps1 -Line '2|b=85'           # send literal lines
#   .\tests\send-mock.ps1 -Test bar -Item 2        # one field of one list item, random value
#   .\tests\send-mock.ps1 -Test disk -Item H       # header works too
#   .\tests\send-mock.ps1 -Test bar -Count 20 -IntervalMs 100   # rapid updates, should coalesce
#
# -Test modes (random values each time, so every send is a real change):
#   mock     the mock file (default)
#   item     whole list item: name, ip, disk, ram, bar, online
#   name, ip, disk, ram, bar, online     only that field (an update within the item)
#   fields   name, ip, disk, ram, bar one after another, -IntervalMs apart
# Close any serial monitor on the port first.
param(
  [string]$Port = 'COM10',
  [string]$File = (Join-Path $PSScriptRoot 'mock-data.txt'),
  [string[]]$Line,
  [ValidateSet('mock', 'item', 'name', 'ip', 'disk', 'ram', 'bar', 'online', 'fields')]
  [string]$Test = 'mock',
  [string]$Item = '1',
  [int]$Count = 1,
  [int]$IntervalMs = 0,
  [int]$Baud = 115200,
  [int]$ReplyMs = 500
)

function Get-Field([string]$field) {
  switch ($field) {
    'name'   { "n=Container $Item-$(Get-Random -Maximum 100)" }
    'ip'     { "ip=192.168.1.$(Get-Random -Minimum 2 -Maximum 255)" }
    'disk'   { $t = 32, 64, 128, 256, 512 | Get-Random; "d=$(Get-Random -Minimum 1 -Maximum ($t + 1))/$t" }
    'ram'    { $t = 4, 8, 16, 32 | Get-Random; "r=$(Get-Random -Minimum 0 -Maximum ($t + 1))/$t" }
    'bar'    { "b=$(Get-Random -Maximum 101)" }
    'online' { "on=$(Get-Random -Maximum 2)" }
  }
}

function Get-TestLines {
  switch ($Test) {
    'mock'   { Get-Content $File | Where-Object { $_.Trim() -and -not $_.StartsWith('#') } }
    'item'   { "$Item|" + ((('name', 'ip', 'disk', 'ram', 'bar', 'online') | ForEach-Object { Get-Field $_ }) -join '|') }
    'fields' { 'name', 'ip', 'disk', 'ram', 'bar' | ForEach-Object { "$Item|$(Get-Field $_)" } }
    default  { "$Item|$(Get-Field $Test)" }
  }
}

$sp = New-Object System.IO.Ports.SerialPort $Port, $Baud, 'None', 8, 'One'
$sp.DtrEnable = $true # USB CDC on the Blue Pill / ESP32 only delivers data once DTR is set
$sp.NewLine = "`n"
$sent = 0
try {
  $sp.Open()
  for ($i = 0; $i -lt $Count; $i++) {
    $lines = if ($Line) { $Line } else { @(Get-TestLines) }
    foreach ($l in $lines) {
      $sp.WriteLine($l)
      Write-Host "> $l"
      $sent++
      Start-Sleep -Milliseconds ([Math]::Max(20, $IntervalMs))
      $reply = $sp.ReadExisting()
      if ($reply) { Write-Host $reply.TrimEnd() }
    }
  }
  Start-Sleep -Milliseconds $ReplyMs
  $reply = $sp.ReadExisting()
  if ($reply) { Write-Host $reply.TrimEnd() }
  Write-Host "Sent $sent line(s) to $Port"
}
finally {
  $sp.Close()
}

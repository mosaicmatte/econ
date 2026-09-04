$wifi = (Get-NetConnectionProfile | Select-Object -ExpandProperty Name | Select-Object -First 1) -replace '[^a-zA-Z0-9]', '_'
if (-not $wifi) { $wifi = "offline_network" }
$wifi = $wifi.ToLower()
Write-Host "Detected Internet Signature: $wifi"
$env:COMPOSE_PROJECT_NAME = "econ_$wifi"
cd D:\ECON1\econ\server
docker compose up -d
cd D:\ECON1\econ\dashboard
Start-Process npm -ArgumentList "run", "dev" -NoNewWindow
cd D:\ECON1\econ
Start-Sleep -Seconds 2
python bridge.py

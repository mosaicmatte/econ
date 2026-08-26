$wifi = (Get-NetConnectionProfile | Select-Object -ExpandProperty Name | Select-Object -First 1) -replace '[^a-zA-Z0-9]', '_'
if (-not $wifi) { $wifi = "Offline_Network" }
Write-Host "Detected Internet Signature: $wifi"
$env:COMPOSE_PROJECT_NAME = "econ_$wifi"
cd D:\ECON1\econ\server
docker compose up --build -d

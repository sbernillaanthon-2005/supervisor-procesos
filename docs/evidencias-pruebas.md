# Evidencias de pruebas

La validación final reproducible está en el README y en
`.github/workflows/test.yml`. Las pruebas cubren carga y rechazo de configuración,
defaults HTTP, políticas, límite e infinito de backoff, cancelación, salida
natural, arranque concurrente único, stop/restart, recarga válida e inválida,
contenido/creación de logs y API con `httptest`.

Para una evidencia manual en Windows:

1. Compile y ejecute `.\supervisor.exe .\config.windows.yaml`.
2. Consulte `GET /status`.
3. Ejecute stop, start, restart y reload sobre `worker-never`.
4. Revise `logs\*.out.log` y `logs\*.err.log`.
5. Pulse Ctrl+C y compruebe:
   `Get-Process supervisor,powershell -ErrorAction SilentlyContinue`.

No se presenta `taskkill /F` como cierre ordenado: es la limitación observable
de Windows para aplicaciones arbitrarias.

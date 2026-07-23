# Supervisor de procesos en Go

Supervisor concurrente orientado a una demostración reproducible en Windows 11,
sin perder compatibilidad Linux. Ejecuta comandos, separa `stdout` y `stderr`,
aplica políticas de reinicio/backoff y expone una API HTTP.

## Inicio rápido en Windows

Requiere Go (la versión indicada en `go.mod` o compatible) y PowerShell.
Desde PowerShell:

```powershell
go mod download
go build -o supervisor.exe .
.\supervisor.exe .\config.windows.yaml
```

Los ejemplos usan `powershell.exe`; no requieren Python. Los logs se crean como
`logs\<nombre>.out.log` y `logs\<nombre>.err.log`. Argumentos, variables `env` y
`working_dir` se transfieren al proceso.

Ctrl+C cancela el supervisor, apaga la API y finaliza los procesos administrados.
En Windows se usa `taskkill /T /F` para terminar el árbol del hijo. Esto es
confiable, pero no constituye un SIGTERM ni garantiza que una aplicación
arbitraria pueda guardar su estado.

## Configuración

`http_api` admite `enabled`, `host` y `port`; el host predeterminado es
`127.0.0.1` y el puerto, `8080`. Cada proceso admite `name`, `command`, `args`,
`env`, `working_dir`, `restart_policy`, `stop_signal`, `stop_wait` y `backoff`.

Las políticas son:

- `never`: no reinicia;
- `on-failure`: reinicia únicamente tras fallo;
- `always`: reinicia tras cualquier salida.

El backoff acepta `base`, `factor`, `max` y `max_tries`. `max_tries` cuenta
reinicios permitidos tras el primer arranque; `-1` significa sin límite. La API
informa por separado `starts` (arranques totales) y `restarts`.

La configuración completa se valida antes de iniciar procesos. Una recarga
inválida se rechaza sin cambiar la configuración activa.

## API y recarga

```powershell
Invoke-RestMethod http://127.0.0.1:8080/status
Invoke-RestMethod -Method Post http://127.0.0.1:8080/processes/worker-never/stop
Invoke-RestMethod -Method Post http://127.0.0.1:8080/processes/worker-never/start
Invoke-RestMethod -Method Post http://127.0.0.1:8080/processes/worker-never/restart
Invoke-RestMethod -Method Post http://127.0.0.1:8080/reload
```

`POST /reload` relee el mismo YAML, lo valida y calcula diferencias: detiene
eliminados, inicia nuevos y reinicia sólo modificados, esperando siempre la
instancia anterior. La API permanece activa aunque terminen todos los procesos.

## Pruebas

```powershell
gofmt -w .
git diff --check
go mod tidy
go build -o supervisor.exe .
go vet ./...
go test ./... -count=1
go test ./... -count=10
```

En Linux también se ejecuta `go test -race ./...` mediante GitHub Actions.
En Windows el race detector necesita CGO y un compilador C.

## Diferencias entre Windows y Linux

Windows recibe Ctrl+C mediante `os.Interrupt` y recarga por `POST /reload`.
Linux conserva SIGINT/SIGTERM para cierre y SIGHUP para recarga. SIGKILL es una
señal Unix y nunca es ordenada; SIGHUP y SIGTERM no tienen equivalencia general
para procesos de consola Windows. Consulta [la máquina de estados](docs/maquina-estados.md)
y [las evidencias](docs/evidencias-pruebas.md).

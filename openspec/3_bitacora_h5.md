# Bitácora de Decisiones y Avances - Hito 5 (Servidor HTTP Integrado)

## Resumen de la sesión
Durante esta sesión se abordó e implementó exitosamente el Hito 5 (H5) del Supervisor de Procesos, el cual exigía proveer un mecanismo (endpoint HTTP) para consultar y controlar el estado de los procesos supervisados (start, stop, restart, status).

Al finalizar la sesión, el código superó de manera impecable las pruebas con el detector de carreras de concurrencia (`go test -race -count=10 ./...`).

## 1. Servidor HTTP y Seguridad por Defecto
**Decisión:** El servidor HTTP se incluye de manera embebida nativa (usando `net/http` de la librería estándar) pero se encuentra deshabilitado por defecto.
**Justificación:** Exponer un puerto de control a la red es un riesgo de seguridad. Para que el servidor arranque, el operador debe activarlo explícitamente en el archivo de configuración a través del bloque `http_api.enabled = true`.

## 2. Respuestas Asíncronas (Eventual Consistency)
**Decisión:** Las acciones mutables (`/start`, `/stop`, `/restart`) responden inmediatamente un HTTP 200 OK con el cuerpo `{"message": "señal enviada", "status": "pending"}` sin bloquear la petición hasta que la acción se concrete.
**Justificación:** En el H4 configuramos un "grace period" (`StopWait`) antes de forzar el SIGKILL de un proceso. Si la API HTTP esperara a que el proceso muriera, la petición podría colgar varios segundos en espera, lo cual es considerado un mal diseño de APIs.

## 3. Comportamiento frente a Intervenciones Manuales (Reseteo de Contadores)
**Decisión:** Cuando un humano interviene e invoca un reinicio (`/restart` o `/start` sobre un proceso detenido/fallido), los contadores de fallos (y en consecuencia, la penalización de reinicios de la política de `Backoff`) se resetean a cero.
**Justificación:** Si un proceso falló repetidas veces y la máquina de estados lo catalogó como `StateFailed`, es de asumir que el operador investigó e intervino la raíz del error antes de reiniciarlo a mano. Mantener los fallos anteriores vigentes ocasionaría castigos indebidos en el nuevo arranque.

## 4. Reutilización del Motor Concurrente (Wrappers Delgados)
**Decisión:** Se encapsuló la lógica de los endpoints HTTP en métodos finos (`StartProcess`, `StopProcess`, `RestartProcess`) los cuales gestionan los cancelFuncs del contexto.
**Justificación:** Se evitó tajantemente duplicar la lógica de lanzar goroutines. Los wrappers únicamente adquieren el mutex `s.mu`, manipulan los diccionarios de cancelación, y reutilizan el método fundamental `startProcessLocked` (igual que lo usa la recarga de configuración `SIGHUP`).

## 5. Pruebas de API con httptest y Limpieza de Goroutines (Bugfixes Post-Revisión)
Se corrigieron hallazgos cruciales identificados tras revisar la implementación de los tests HTTP:
* **Higiene de Threads (`TestAPI_ProcessesActions`):** Dado que se testeaban endpoints de arranque invocando comandos reales, se implementó orquestación completa (`sv.Start(ctx)` + `defer sv.Wait()`) dentro del test. Esto asegura que el test limpie las goroutines hijas (evitando procesos "zombis") antes de finalizar, indispensable para no acumular ruido en corridas agresivas de validación (`-race -count=10`).
* **Protección del Contexto Global:** Si un cliente consultaba un endpoint de `/start` previo a que el propio supervisor inicie, disparaba un `panic` debido a un `context.WithCancel(nil)`. Se unificaron protecciones para validar que `globalCtx != nil` garantizando terminación controlada (HTTP 500).
* **Manejo de Errores Go Idiomático:** Se reemplazó el control de errores basado en fragmentos de string (frágil) por un Error Centinela (`ErrProcessNotFound`) explotado limpiamente mediante `errors.Is`.

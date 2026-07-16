# Supervisor de Procesos - Bitácora de Desarrollo (Hito 4)

## Hito 4 Completado: Señales del OS y Apagado Ordenado

### ¿Qué hicimos?
Hemos implementado con éxito la interceptación de señales del sistema operativo y la recarga en caliente (hot-reload) de la configuración, elevando la robustez del supervisor sin sacrificar la compatibilidad.

1. **Apagado Ordenado (Graceful Shutdown) con Go 1.20+:**
   - En lugar de depender del comportamiento abrupto (SIGKILL inmediato) que expone `exec.CommandContext` por defecto tras cancelar un contexto, implementamos una interceptación manual aprovechando las propiedades `Cancel` y `WaitDelay` del paquete `os/exec`.
   - Ahora, al detener un proceso, el supervisor le envía un aviso (por defecto `SIGTERM`, configurable vía `StopSignal`), le otorga un periodo de gracia (configurable vía `StopWait`, default de 5s), y si el proceso no acata, procede a liquidarlo (`SIGKILL`).

2. **Compatibilidad con Windows (Honestidad Técnica):**
   - Windows no maneja las señales POSIX (`SIGTERM`, `SIGINT`) a nivel de procesos hijos de manera nativa mediante `os.Process.Signal()`. Intentar enviarlas de forma cruzada suele requerir hacks inestables.
   - Decisión de Diseño: Aislamos el manejo de señales usando _Build Tags_ (`signals_unix.go` y `signals_windows.go`). En Windows documentamos e implementamos un fallback transparente donde, ante una petición de apagado, se registra un `[WARN]` aclarando la limitación del SO y se hace un `.Kill()` inmediato de forma determinista y segura.

3. **Recarga Dinámica (Diffing) sin reiniciar TODO:**
   - Para soportar la recarga del `config.yaml` sin afectar procesos ajenos a los cambios, rediseñamos la orquestación interna. Cada proceso ahora tiene su propio `context.CancelFunc` derivado, almacenado en el supervisor.
   - El método `ReloadConfig` ejecuta una estrategia de **Diff**:
     - **Removidos:** Cancela solo a los procesos que ya no figuran en el YAML.
     - **Agregados:** Inicia la máquina de estados de los nuevos.
     - **Modificados:** Usa `reflect.DeepEqual` para comparar si el proceso en caliente difiere de su homólogo en la nueva configuración (por ejemplo, si cambiaron los argumentos o variables de entorno); de ser así, lo apaga ordenadamente y lo reinicia.
   - Decisión de Diseño: Proteger este mapeo compartido con el mismo `sync.Mutex` del H3 fue crucial para garantizar concurrencia libre de "data races".

4. **Tests Programáticos Aislados del SO:**
   - Escribimos pruebas automatizadas (`TestSupervisor_ReloadConfig`, `TestSupervisor_GracefulShutdown`) que operan consumiendo los métodos internos programáticamente en lugar de intentar orquestar el envío de señales reales (lo cual es muy frágil en entornos de CI o multiplataforma).
   - Comprobamos el código iterativamente 10 veces mediante el flag `-race` confirmando una sincronización impecable.

---

## Pendientes / Qué falta por hacer

### Hito 5: API de Control Local y Monitoreo Remoto
Con nuestro supervisor controlando finamente los ciclos de vida, backoffs y recargas dinámicas, el siguiente (y último) paso natural es hacerlo interactivamente inspeccionable.

- **Servidor HTTP o RPC Integrado:**
  - Montar un servidor concurrente ligero junto al supervisor que exponga los endpoints de estado.
- **Obtención de Estado en Vivo:**
  - Crear un mecanismo para que un cliente pueda consultar `GET /status` y visualizar el estado actual (`RUNNING`, `FAILED`, `STOPPED`, `BACKOFF`) y metadatos (reintentos actuales) de todos los procesos.
- **Control Manual de Procesos (Opcional):**
  - Permitir comandos atómicos vía API (`POST /stop/{name}`, `POST /start/{name}`) para interactuar manualmente con un proceso sin requerir la edición del YAML.
- **Consolidación Final:**
  - Asegurar la limpieza y cierre correcto del servidor HTTP cuando el propio supervisor reciba el `SIGTERM` final.

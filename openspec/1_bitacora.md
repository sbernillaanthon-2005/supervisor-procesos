# Supervisor de Procesos / Job Scheduler - Bitácora de Desarrollo

## Objetivo
Construir un supervisor de procesos en Go (estilo mini-systemd) de forma incremental, asegurando un diseño robusto, manejo seguro de la concurrencia y alta testabilidad, cumpliendo con la política anti-copia (defensa oral de código).

## Hitos Completados

### Hito 1: Parsing de Configuración y Orquestación Básica
- **Qué hicimos:** Definimos la estructura `Config` y `ProcessConfig`. Implementamos lectura de archivos YAML (`config.yaml`) usando `gopkg.in/yaml.v3`. Diseñamos el modelo concurrente básico usando `sync.WaitGroup` para esperar a múltiples procesos concurrentes.
- **Decisiones de Diseño:** 
  - Elegimos YAML sobre TOML por ser el estándar en orquestación (K8s, Compose) y soportar mejor listas anidadas.
  - Usamos `os/exec` para lanzar los comandos.
  - Omitimos campos complejos en esta fase inicial para centrarnos en arrancar múltiples `goroutines`.
- **Estado:** ✅ Validado y testeado.

### Hito 2: Ciclo de Vida y Políticas de Reinicio
- **Qué hicimos:** Introdujimos `context.Context` para atar el ciclo de vida de los procesos al supervisor (usando `exec.CommandContext`). Implementamos un bucle infinito en `superviseProcess` que evalúa las políticas `always`, `on-failure` y `never`. Agregamos un estado compartido seguro (`startCounts` con `sync.Mutex`) para testear los reinicios.
- **Decisiones de Diseño:**
  - `exec.CommandContext` delega en el OS la terminación forzosa (`TerminateProcess` en Windows, `SIGKILL` en Unix) si se cancela el contexto.
  - Los tests dejaron de verificar solo archivos de log y pasaron a comprobar matemáticamente cuántas veces arrancó un proceso consultando el contador thread-safe, eliminando los "tests ciegos".
- **Estado:** ✅ Validado (tests y `-race`).

### Hito 3: Backoff Exponencial y Máquina de Estados
- **Qué hicimos:** Diseñamos una verdadera máquina de estados (`STOPPED`, `RUNNING`, `BACKOFF`, `FAILED`) observable a través del struct `ProcessStatus`. Reemplazamos los bloqueos ingenuos por un backoff exponencial paramétrico (`Base * Factor^(intentos-1)`).
- **Decisiones de Diseño:**
  - Tratamos la omisión de `max_tries` inyectando un default seguro de `5` intentos para prevenir tormentas de reinicio (Protección UX). El valor explícito `-1` activa reintentos infinitos.
  - Saneamos individualmente los "Zero Values" de Go para evitar caídas matemáticas (ej. `factor: 0` se neutraliza a `2.0`).
  - Sustituimos `time.Sleep` por `select { case <-time.After(waitDur): case <-ctx.Done(): }` en los estados de espera. Esto garantiza que la cancelación global del sistema se escuche instantáneamente sin dejar hilos zombie.
- **Estado:** ✅ Validado con pruebas rigurosas para inyección matemática en ceros y bucles infinitos.

---

## Estado Actual
Nos encontramos al **final del Hito 3**. El supervisor puede:
- Arrancar N procesos simultáneamente con entornos y directorios dinámicos.
- Reiniciar procesos que fallan guiado por políticas (`always`, `on-failure`, `never`).
- Escalar la espera de reinicio exponencialmente (backoff) si hay fallos recurrentes.
- Protegerse a sí mismo de configuraciones YAML en blanco o matemáticamente inválidas.
- Reportar en tiempo real un estado interno seguro mediante `sync.Mutex`.

---

## Pendientes / Qué falta por hacer

### Hito 4: Señales del OS y Apagado Ordenado (Graceful Shutdown)
- **El Problema Actual:** `exec.CommandContext` envía un `SIGKILL` (o `TerminateProcess`), matando los procesos de forma abrupta y brutal. Esto puede corromper datos de las aplicaciones hijas si estaban guardando archivos.
- **Solución Propuesta (H4):** 
  - Interceptar las señales del sistema (`SIGINT`, `SIGTERM`) en el `main.go`.
  - Propagar un aviso amable a los procesos hijos (ej. `SIGTERM`) permitiendo un periodo de gracia (*grace period*, ej. 10s) para que limpien sus recursos. 
  - Solo si el hijo no obedece el periodo de gracia, usar la fuerza bruta (`SIGKILL`).
  - Incorporar soporte para `SIGHUP` con el objetivo de recargar el `config.yaml` sin apagar los procesos existentes.

### Hito 5: API de Control (Opcional/Avanzado)
- Exponer un servidor HTTP local o un *Unix Socket* para permitir inspeccionar el estado en vivo de los procesos (listar quién está `RUNNING`, quién está `FAILED`).
- Permitir comandos manuales como `start`, `stop` o `restart` desde otra terminal o CLI cliente, conectándose al supervisor de fondo.

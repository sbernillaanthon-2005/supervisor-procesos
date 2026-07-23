# Máquina de estados

```text
stopped/failed -- start --> running
running -- salida con reinicio --> backoff (si falló) --> running
running/backoff -- stop --> stopping --> stopped
running -- salida sin reinicio --> stopped (éxito) | failed (fallo)
```

Una ejecución activa posee contexto, cancelación y canal `done`. `stop` cancela
y espera `done`; `restart` ejecuta ese mismo cierre antes de crear una sola
instancia. Durante backoff un `time.Timer` escucha el contexto, por lo que la
cancelación es inmediata. `starts` cuenta arranques totales y `restarts` los
arranques posteriores al primero.

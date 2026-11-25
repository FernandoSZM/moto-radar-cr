# MotoRadar CR

Registrá motos y seguí sus precios en colones. La interfaz usa HTML, CSS y JavaScript. Go sirve la página y la API, y SQLite guarda los datos en `moto-radar.db`.

## Iniciar y probar

Desde la raíz del proyecto, con Go 1.27.4 o una versión compatible:

```sh
go run .
```

Abrí [MotoRadar CR](http://localhost:8080/). Al arrancar, la app crea la base si hace falta y aplica la migración del enlace al anuncio a las bases existentes.

```sh
go test ./...
go build ./...
```

Las pruebas crean sus propias bases SQLite temporales.

## API

| Método y ruta | Operación |
| --- | --- |
| `GET /motos` | Lista las motos por ID. |
| `GET /motos?precio_max=1200000` | Lista las motos con un precio igual o inferior al presupuesto. |
| `POST /motos` | Crea una moto y registra su precio inicial. |
| `GET /motos/{id}` | Devuelve una moto. |
| `PATCH /motos/{id}` | Edita los campos enviados y registra el precio si cambió. |
| `DELETE /motos/{id}` | Elimina la moto y su historial. |
| `GET /motos/{id}/precios` | Devuelve el historial, desde el primer precio registrado. |

Los campos editables son `marca`, `modelo`, `anio`, `kilometraje`, `precio_crc` y `anuncio_url`. El enlace es opcional y debe usar HTTP o HTTPS. Para quitarlo, enviá `{"anuncio_url":""}` en un PATCH. Por ejemplo, `{"kilometraje":0}` guarda cero kilómetros y conserva los demás datos.

# RTMP Web

Aplicación web local para recibir la señal RTMP de un DJI Mini 4 Pro y administrarla desde el navegador.

## Idea

El DJI Mini 4 Pro publica por RTMP en el ordenador. Un único proceso SRS recibe la señal. La aplicación web administra el servicio y muestra las URLs para usar la señal en OBS/VLC.

```text
DJI Mini 4 Pro ── RTMP ──> SRS :1935 ──> RTMP Web :17890
                                      └── RTMP playback ──> OBS/VLC
```

No hay transcodificación: la señal conserva los codecs enviados por la cámara. Esta versión está pensada para una red local de confianza y no implementa autenticación.

## Requisitos

- Go 1.23 o superior.
- SRS 6.x instalado o copiado en `data/runtime/srs/srs`.
- El DJI Mini 4 Pro y el ordenador en la misma LAN.
- Windows 10/11 o macOS.

## Ejecutar

```bash
go run .
```

Abre automáticamente:

```text
http://127.0.0.1:17890
```

La aplicación abre el puerto RTMP 1935. El firewall solo necesita permitir ese puerto TCP en la red privada.

## Configurar el DJI Mini 4 Pro

Usa los datos que aparecen en la web:

- Servidor: `rtmp://IP_DEL_PC:1935/live`
- Stream key: la clave mostrada en la interfaz
- Vídeo: H.264
- Resolución: 1920×1080
- FPS: 30
- Audio: AAC
- Bitrate: 4–8 Mbps
- GOP/keyframe: 1–2 segundos

Para una conexión Wi-Fi irregular, empieza con 1080p/30 a 4 Mbps. Sube gradualmente a 6–8 Mbps solo si no aparecen cortes ni pérdida de paquetes. El servidor no transcodifica ni puede recuperar paquetes que se pierdan entre el dron y el ordenador.

## Ver la señal

- **Navegador:** la aplicación web funciona únicamente como panel de control.
- **OBS:** añade una fuente **VLC Video Source** y pega la URL RTMP de reproducción mostrada en la web.
- **VLC:** abre directamente la URL RTMP.

## Instalar SRS

Descarga un binario SRS 6.x compatible con tu sistema desde [la página de releases de SRS](https://github.com/ossrs/srs/releases).

### macOS

Compila SRS 6.x y copia el binario a:

```text
data/runtime/srs/srs
```

### Windows

Copia `srs.exe` a:

```text
data\runtime\srs\srs.exe
```

También puedes indicar otra ruta al arrancar:

```bash
go run . --srs /ruta/completa/a/srs
```

## Compilar

```bash
go build -o rtmp-web .
```

En Windows:

```powershell
go build -o rtmp-web.exe .
```

## Configuración avanzada

Se puede cambiar el directorio de datos y los puertos desde la línea de comandos:

```bash
go run . --data-dir ./data --rtmp-port 1935 --web-port 17890
```

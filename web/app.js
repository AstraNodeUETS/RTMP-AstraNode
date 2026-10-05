"use strict";

const $ = (id) => document.getElementById(id);
let hls = null;
let loadedKey = "";
let toastTimer;

function showToast(message) {
  const toast = $("toast");
  toast.textContent = message;
  toast.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove("show"), 2600);
}

function setMessage(message, hidden = false) {
  const el = $("videoMessage");
  el.textContent = message;
  el.classList.toggle("hidden", hidden);
}

function loadStream(key) {
  if (loadedKey === key) return;
  loadedKey = key;
  if (hls) {
    hls.destroy();
    hls = null;
  }
  const source = `/hls/live/${encodeURIComponent(key)}.m3u8`;
  setMessage("Esperando señal de la Osmo…");

  if (window.Hls && Hls.isSupported()) {
    hls = new Hls({
      enableWorker: true,
      lowLatencyMode: false,
      backBufferLength: 30,
      maxBufferLength: 60,
      maxMaxBufferLength: 120,
      liveSyncDuration: 3,
      liveMaxLatencyDuration: 10,
      startLevel: -1,
      capLevelToPlayerSize: true,
    });
    hls.loadSource(source);
    hls.attachMedia($("player"));
    hls.on(Hls.Events.MANIFEST_PARSED, () => {
      $("player").play().catch(() => {});
    });
    hls.on(Hls.Events.ERROR, (_, data) => {
      if (data && data.fatal) {
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
          hls.startLoad();
        } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
          hls.recoverMediaError();
        } else {
          hls.destroy();
          hls = null;
          setMessage("Error de reproducción. Verifica que la Osmo esté transmitiendo.");
        }
      } else if (data && !data.fatal) {
        setMessage("Conectando con la señal…");
      }
    });
    hls.on(Hls.Events.FRAG_LOADED, () => {
      setMessage("", true);
    });
  } else if ($("player").canPlayType("application/vnd.apple.mpegurl")) {
    $("player").src = source;
    $("player").play().catch(() => {});
    $("player").addEventListener("loadeddata", () => setMessage("", true), { once: true });
  } else {
    setMessage("Este navegador no puede reproducir HLS. Usa la URL RTMP en VLC/OBS.");
  }
}

async function refresh() {
  try {
    const response = await fetch("/api/status", { cache: "no-store" });
    if (!response.ok) throw new Error("No se pudo consultar el servidor");
    const status = await response.json();
    const state = $("serviceState");
    state.className = `state ${status.running ? "running" : status.error ? "error" : "loading"}`;
    state.querySelector("strong").textContent = status.running ? "Servidor activo" : status.error ? "SRS no disponible" : "Comprobando servidor…";
    $("srsStatus").textContent = status.running ? "Activo" : "Detenido";
    $("keyStatus").textContent = status.stream_key || "—";
    $("droneKeyStatus").textContent = status.drone_key || "—";
    $("errorStatus").textContent = status.error || "—";
    $("ipStatus").textContent = status.local_ip || "No detectada";
    $("serverUrl").value = status.ingest_url || "";
    $("streamKey").value = status.stream_key || "";
    $("playUrl").value = status.rtmp_play || "";
    $("hlsUrl").value = status.hls_url || "";
    // Drone fields
    $("droneServerUrl").value = status.drone_url || "";
    $("droneKey").value = status.drone_key || "";
    $("dronePlayUrl").value = status.drone_play || "";
    $("droneHlsUrl").value = status.drone_hls || "";
    $("startButton").disabled = !!status.running;
    $("stopButton").disabled = !status.running;
    if (status.stream_key) {
      loadStream(status.stream_key);
      fetchStreamInfo(status.stream_key);
    } else {
      hideStreamDetails();
    }
  } catch (error) {
    const state = $("serviceState");
    state.className = "state error";
    state.querySelector("strong").textContent = "Sin conexión";
    $("errorStatus").textContent = error.message;
  }
}

async function fetchStreamInfo(key) {
  try {
    const resp = await fetch("/api/stream-info", { cache: "no-store" });
    if (!resp.ok) return;
    const info = await resp.json();
    if (!info || info.error) {
      hideStreamDetails();
      return;
    }
    showStreamDetails(info);
  } catch {
    hideStreamDetails();
  }
}

function showStreamDetails(info) {
  $("detailResolution").textContent = info.resolution || "—";
  $("detailFps").textContent = info.fps || "—";
  $("detailVcodec").textContent = info.vcodec || "—";
  $("detailAcodec").textContent = info.acodec || "—";
  $("detailBitrate").textContent = info.bitrate || "—";
  $("streamDetails").hidden = false;
}

function hideStreamDetails() {
  $("streamDetails").hidden = true;
}

async function control(action) {
  try {
    const response = await fetch(`/api/${action}`, { method: "POST" });
    if (!response.ok) throw new Error((await response.text()) || "No se pudo ejecutar la acción");
    await refresh();
    showToast(action === "start" ? "Servidor iniciado" : "Servidor detenido");
  } catch (error) {
    showToast(error.message);
  }
}

async function copyValue(id) {
  const input = $(id);
  if (!input.value) return;
  try {
    await navigator.clipboard.writeText(input.value);
  } catch {
    input.focus();
    input.select();
    document.execCommand("copy");
  }
  showToast("Copiado");
}

$("player").addEventListener("playing", () => setMessage("", true));
$("player").addEventListener("waiting", () => setMessage("Buffering…"));
$("player").addEventListener("error", () => setMessage("Error de reproducción. ¿Está la Osmo transmitiendo?"));
$("startButton").addEventListener("click", () => control("start"));
$("stopButton").addEventListener("click", () => control("stop"));
$("refreshButton").addEventListener("click", refresh);
document.querySelectorAll("[data-copy]").forEach((button) => {
  button.addEventListener("click", () => copyValue(button.dataset.copy));
});

refresh();
setInterval(refresh, 2000);
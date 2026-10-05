"use strict";

const $ = (id) => document.getElementById(id);
let toastTimer;

// Cada fuente tiene su propio player, sus propios campos y sus estadísticas.
const SOURCES = [
  {
    id: "drone",
    label: "el dron",
    player: "playerDrone",
    message: "msgDrone",
    badge: "badgeDrone",
    note: "noteDrone",
    link: "droneHlsLink",
    fields: {
      ingest: "droneServerUrl",
      key: "droneKey",
      play: "dronePlayUrl",
      hls: "droneHlsUrl",
    },
    stats: ["Resolution", "Fps", "Vcodec", "Acodec", "Bitrate", "Clients"],
    values: {
      ingest: "drone_url",
      key: "drone_key",
      play: "drone_play",
      hls: "drone_hls",
    },
  },
  {
    id: "osmo",
    label: "la Osmo",
    player: "playerOsmo",
    message: "msgOsmo",
    badge: "badgeOsmo",
    note: "noteOsmo",
    link: "osmoHlsLink",
    fields: {
      ingest: "osmoServerUrl",
      key: "osmoKey",
      play: "osmoPlayUrl",
      hls: "osmoHlsUrl",
    },
    stats: ["Resolution", "Fps", "Vcodec", "Acodec", "Bitrate", "Clients"],
    values: {
      ingest: "ingest_url",
      key: "stream_key",
      play: "rtmp_play",
      hls: "hls_url",
    },
  },
];

const players = new Map();
const onlineSince = new Map();

function showToast(message) {
  const toast = $("toast");
  toast.textContent = message;
  toast.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove("show"), 2600);
}

function setMessage(source, message, hidden = false) {
  const el = $(source.message);
  if (!el) return;
  el.textContent = message;
  el.classList.toggle("hidden", hidden);
}

function setBadge(source, state, text) {
  const badge = $(source.badge);
  if (!badge) return;
  badge.className = `badge badge-${state}`;
  badge.querySelector(".badge-text").textContent = text;
}

function badgeFor(source, online) {
  if (online === undefined || online === null) {
    setBadge(source, "wait", "Conectando…");
    return;
  }
  if (online) setBadge(source, "live", "En directo");
  else setBadge(source, "idle", "Sin señal");
}

// Crea (o recupera) el estado del player de una fuente y engancha sus eventos.
function playerFor(source) {
  const existing = players.get(source.id);
  if (existing) return existing;

  const video = $(source.player);
  const state = { hls: null, key: "" };
  players.set(source.id, state);

  video.addEventListener("playing", () => setMessage(source, "", true));
  video.addEventListener("waiting", () => setMessage(source, "Buffering…"));
  video.addEventListener("error", () =>
    setMessage(source, `Error de reproducción. ¿${source.label} está transmitiendo?`)
  );
  return state;
}

function loadStream(source, key) {
  const state = playerFor(source);
  if (!key) {
    state.key = "";
    if (state.hls) {
      state.hls.destroy();
      state.hls = null;
    }
    $(source.player).removeAttribute("src");
    $(source.player).load();
    setMessage(source, `Configura ${source.label} para comenzar.`);
    return;
  }
  if (state.key === key) return;
  state.key = key;

  const video = $(source.player);
  if (state.hls) {
    state.hls.destroy();
    state.hls = null;
  }
  const url = `/hls/live/${encodeURIComponent(key)}.m3u8`;
  setMessage(source, `Esperando señal de ${source.label}…`);

  if (window.Hls && Hls.isSupported()) {
    const hls = new Hls({
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
    state.hls = hls;
    hls.loadSource(url);
    hls.attachMedia(video);
    hls.on(Hls.Events.MANIFEST_PARSED, () => video.play().catch(() => {}));
    hls.on(Hls.Events.ERROR, (_, data) => {
      if (state.hls !== hls) return;
      if (data && data.fatal) {
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
          hls.startLoad();
        } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
          hls.recoverMediaError();
        } else {
          hls.destroy();
          state.hls = null;
          setMessage(source, `Error de reproducción. Revisa que ${source.label} esté transmitiendo.`);
        }
      } else if (data) {
        setMessage(source, `Conectando con ${source.label}…`);
      }
    });
    hls.on(Hls.Events.FRAG_LOADED, () => setMessage(source, "", true));
  } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
    video.src = url;
    video.play().catch(() => {});
    video.addEventListener("loadeddata", () => setMessage(source, "", true), { once: true });
  } else {
    setMessage(source, "Este navegador no puede reproducir HLS. Usa la URL RTMP en VLC/OBS.");
  }
}

function renderStatus(status) {
  const state = $("serviceState");
  if (!status) {
    state.className = "state error";
    state.querySelector("strong").textContent = "Sin conexión";
    $("errorStatus").textContent = "Sin conexión con la aplicación web";
    $("srsStatus").textContent = "—";
    $("ipStatus").textContent = "—";
    $("activeSources").textContent = "—";
    return;
  }

  state.className = `state ${status.running ? "running" : status.error ? "error" : "loading"}`;
  state.querySelector("strong").textContent = status.running
    ? "Servidor activo"
    : status.error
      ? "SRS no disponible"
      : "Comprobando servidor…";
  $("srsStatus").textContent = status.running ? "Activo" : "Detenido";
  $("errorStatus").textContent = status.error || "—";
  $("ipStatus").textContent = status.local_ip || "No detectada";
  $("startButton").disabled = !!status.running;
  $("stopButton").disabled = !status.running;

  for (const source of SOURCES) {
    $(source.fields.ingest).value = status[source.values.ingest] || "";
    $(source.fields.key).value = status[source.values.key] || "";
    $(source.fields.play).value = status[source.values.play] || "";
    const hls = status[source.values.hls] || "";
    $(source.fields.hls).value = hls;
    const link = $(source.link);
    if (link) {
      if (hls) {
        link.href = hls;
        link.removeAttribute("aria-disabled");
      } else {
        link.removeAttribute("href");
        link.setAttribute("aria-disabled", "true");
      }
    }
    loadStream(source, status[source.values.key] || "");
  }
}

function setStats(source, stats) {
  for (const name of source.stats) {
    const el = $(`stat${capitalize(source.id)}${name}`);
    if (!el) continue;
    if (name === "Clients") {
      el.textContent = stats && typeof stats.clients === "number" ? String(stats.clients) : "—";
      continue;
    }
    const value = stats ? stats[name.toLowerCase()] : "";
    el.textContent = value || "—";
    el.title = value || "";
  }
}

function capitalize(text) {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function renderSources(payload) {
  const available = !!payload && payload.stats_available;
  $("statsStatus").textContent = available ? "En vivo" : "No disponibles";
  $("statsStatus").classList.toggle("status-warn", !available);

  let active = 0;
  for (const source of SOURCES) {
    const stats = available ? payload[source.id] : null;
    const online = !!(stats && stats.online);
    if (online) active += 1;
    badgeFor(source, available ? online : undefined);
    setStats(source, stats);

    const note = $(source.note);
    if (note) {
      if (available) {
        note.hidden = true;
        note.textContent = "";
      } else {
        note.hidden = false;
        note.textContent = payload && payload.error
          ? `Estadísticas no disponibles: ${payload.error}`
          : "Estadísticas no disponibles: la API de SRS no responde.";
      }
    }

    if (online && !onlineSince.has(source.id)) onlineSince.set(source.id, Date.now());
    if (!online) onlineSince.delete(source.id);
  }
  $("activeSources").textContent = available ? `${active} / ${SOURCES.length}` : "—";
}

// El tiempo en directo avanza solo, sin depender del refresco de la API.
function tickUptime() {
  const now = Date.now();
  for (const source of SOURCES) {
    const badge = $(source.badge);
    if (!badge || !badge.classList.contains("badge-live")) continue;
    const since = onlineSince.get(source.id);
    const seconds = since ? Math.floor((now - since) / 1000) : 0;
    badge.querySelector(".badge-text").textContent = `En directo · ${formatUptime(seconds)}`;
  }
}

function formatUptime(seconds) {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  const pad = (value) => String(value).padStart(2, "0");
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(rest)}` : `${pad(minutes)}:${pad(rest)}`;
}

async function refresh() {
  const read = async (url) => {
    try {
      const response = await fetch(url, { cache: "no-store" });
      if (!response.ok) return null;
      return await response.json();
    } catch {
      return null;
    }
  };
  const [status, sources] = await Promise.all([read("/api/status"), read("/api/sources")]);
  renderStatus(status);
  renderSources(sources);
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

$("startButton").addEventListener("click", () => control("start"));
$("stopButton").addEventListener("click", () => control("stop"));
$("refreshButton").addEventListener("click", refresh);
document.querySelectorAll("[data-copy]").forEach((button) => {
  button.addEventListener("click", () => copyValue(button.dataset.copy));
});

refresh();
setInterval(refresh, 2000);
setInterval(tickUptime, 1000);

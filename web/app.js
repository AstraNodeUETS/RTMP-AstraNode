"use strict";

const $ = (id) => document.getElementById(id);
let toastTimer;

function showToast(message) {
  const toast = $("toast");
  toast.textContent = message;
  toast.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove("show"), 2600);
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
    $("errorStatus").textContent = status.error || "—";
    $("ipStatus").textContent = status.local_ip || "No detectada";
    $("serverUrl").value = status.ingest_url || "";
    $("streamKey").value = status.stream_key || "";
    $("playUrl").value = status.rtmp_play || "";
    $("startButton").disabled = !!status.running;
    $("stopButton").disabled = !status.running;
  } catch (error) {
    const state = $("serviceState");
    state.className = "state error";
    state.querySelector("strong").textContent = "Sin conexión";
    $("errorStatus").textContent = error.message;
  }
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
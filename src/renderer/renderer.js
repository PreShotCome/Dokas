const els = {
  apiKey: document.getElementById('apiKey'),
  saveKey: document.getElementById('saveKey'),
  keyStatus: document.getElementById('keyStatus'),
  source: document.getElementById('source'),
  refresh: document.getElementById('refresh'),
  video: document.getElementById('video'),
  regionBox: document.getElementById('regionBox'),
  selectionLayer: document.getElementById('selectionLayer'),
  clearRegion: document.getElementById('clearRegion'),
  regionLabel: document.getElementById('regionLabel'),
  record: document.getElementById('record'),
  pause: document.getElementById('pause'),
  timer: document.getElementById('timer'),
  aiToggle: document.getElementById('aiToggle'),
  aiInterval: document.getElementById('aiInterval'),
  status: document.getElementById('status'),
  summary: document.getElementById('summary'),
  reminders: document.getElementById('reminders'),
  timeline: document.getElementById('timeline'),
  cost: document.getElementById('cost'),
};

// Haiku 4.5 pricing, USD per 1M tokens.
const PRICE_IN = 1.0;
const PRICE_OUT = 5.0;
// Below this mean per-channel pixel difference (0-255), the frame counts as unchanged.
const CHANGE_THRESHOLD = 4;
const MAX_TIMELINE_ITEMS = 50;
const RECORD_FPS = 30;

let previewStream = null;
let region = null; // { x, y, w, h } in source pixels, or null for the whole source

let recording = false;
let isPaused = false;
let mediaRecorder = null;
let recordedChunks = [];
let recordMime = '';
let recordStartTime = 0;
let pauseStartedAt = 0;
let pausedTotal = 0;
let recordTimerId = null;

let regionCanvas = null;
let regionCtx = null;
let drawRaf = null;

let aiWasOn = false;
let aiTimerId = null;
let analyzing = false;
let prevSignature = null;
let sessionSummaries = [];
let sessionCost = 0;
const seenReminders = new Set();

const captureCanvas = document.createElement('canvas');
const diffCanvas = document.createElement('canvas');
diffCanvas.width = 32;
diffCanvas.height = 32;

function setStatus(text, isError) {
  els.status.textContent = text;
  els.status.classList.toggle('error', !!isError);
}

function escapeHtml(text) {
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}

function formatTime(totalSeconds) {
  const m = Math.floor(totalSeconds / 60);
  const s = totalSeconds % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

// --- API key ---------------------------------------------------------------

function showKeyStatus(hasKey) {
  els.keyStatus.textContent = hasKey ? 'Key saved.' : 'No key — AI recap is disabled.';
  els.keyStatus.classList.toggle('ok', hasKey);
  els.keyStatus.classList.toggle('missing', !hasKey);
}

async function refreshKeyStatus() {
  try {
    const status = await window.api.getStatus();
    showKeyStatus(status.hasKey);
  } catch {
    showKeyStatus(false);
  }
}

async function saveKey() {
  const res = await window.api.setApiKey(els.apiKey.value.trim());
  if (res.ok) {
    showKeyStatus(res.hasKey);
    setStatus('API key saved.');
  } else {
    setStatus(`Could not save key: ${res.error}`, true);
  }
}

// --- Sources & preview -----------------------------------------------------

async function loadSources() {
  const sources = await window.api.getCaptureSources();
  els.source.innerHTML = '';
  for (const s of sources) {
    const opt = document.createElement('option');
    opt.value = s.id;
    opt.textContent = `${s.kind === 'screen' ? '[Screen] ' : ''}${s.name}`;
    els.source.appendChild(opt);
  }
  if (els.source.value) await startPreview(els.source.value);
}

async function startPreview(sourceId) {
  stopPreview();
  clearRegion();
  try {
    previewStream = await navigator.mediaDevices.getUserMedia({
      audio: false,
      video: {
        mandatory: {
          chromeMediaSource: 'desktop',
          chromeMediaSourceId: sourceId,
          maxWidth: 1920,
          maxHeight: 1080,
        },
      },
    });
    els.video.srcObject = previewStream;
    await els.video.play();
    els.record.disabled = false;
    setStatus('Ready. Drag a region if you want, then start recording.');
  } catch (err) {
    els.record.disabled = true;
    setStatus(`Could not capture that source: ${err.message}`, true);
  }
}

function stopPreview() {
  if (previewStream) {
    previewStream.getTracks().forEach((t) => t.stop());
    previewStream = null;
  }
  els.video.srcObject = null;
}

// --- Region selection ------------------------------------------------------

let dragStart = null;

function clearRegion() {
  region = null;
  els.regionBox.classList.add('hidden');
  els.regionLabel.textContent = '';
}

function onPointerDown(event) {
  if (recording || !previewStream) return;
  const rect = els.video.getBoundingClientRect();
  dragStart = { x: event.clientX - rect.left, y: event.clientY - rect.top };
}

function onPointerMove(event) {
  if (!dragStart) return;
  const rect = els.video.getBoundingClientRect();
  const cx = Math.min(Math.max(event.clientX - rect.left, 0), rect.width);
  const cy = Math.min(Math.max(event.clientY - rect.top, 0), rect.height);
  const left = Math.min(dragStart.x, cx);
  const top = Math.min(dragStart.y, cy);
  const w = Math.abs(cx - dragStart.x);
  const h = Math.abs(cy - dragStart.y);
  els.regionBox.classList.remove('hidden');
  els.regionBox.style.left = `${left}px`;
  els.regionBox.style.top = `${top}px`;
  els.regionBox.style.width = `${w}px`;
  els.regionBox.style.height = `${h}px`;
}

function onPointerUp(event) {
  if (!dragStart) return;
  const rect = els.video.getBoundingClientRect();
  const cx = Math.min(Math.max(event.clientX - rect.left, 0), rect.width);
  const cy = Math.min(Math.max(event.clientY - rect.top, 0), rect.height);
  const left = Math.min(dragStart.x, cx);
  const top = Math.min(dragStart.y, cy);
  const w = Math.abs(cx - dragStart.x);
  const h = Math.abs(cy - dragStart.y);
  dragStart = null;

  if (w < 12 || h < 12) {
    clearRegion();
    return;
  }
  const scaleX = els.video.videoWidth / rect.width;
  const scaleY = els.video.videoHeight / rect.height;
  region = {
    x: Math.round(left * scaleX),
    y: Math.round(top * scaleY),
    w: Math.max(2, Math.round(w * scaleX)),
    h: Math.max(2, Math.round(h * scaleY)),
  };
  els.regionLabel.textContent = `Region: ${region.w}×${region.h}px`;
}

function currentRect() {
  if (region) return region;
  return { x: 0, y: 0, w: els.video.videoWidth, h: els.video.videoHeight };
}

// --- Recording -------------------------------------------------------------

function pickMime() {
  const candidates = ['video/webm;codecs=vp9', 'video/webm;codecs=vp8', 'video/webm'];
  return candidates.find((c) => MediaRecorder.isTypeSupported(c)) || '';
}

function regionDrawLoop() {
  if (!recording || !region) return;
  regionCtx.drawImage(
    els.video,
    region.x, region.y, region.w, region.h,
    0, 0, regionCanvas.width, regionCanvas.height,
  );
  drawRaf = requestAnimationFrame(regionDrawLoop);
}

function elapsedSeconds() {
  let pausedMs = pausedTotal;
  if (isPaused) pausedMs += Date.now() - pauseStartedAt;
  return Math.floor((Date.now() - recordStartTime - pausedMs) / 1000);
}

function tickTimer() {
  els.timer.textContent = formatTime(elapsedSeconds());
}

function startAiTimer() {
  const intervalMs = Math.max(10, Number(els.aiInterval.value) || 30) * 1000;
  aiTimerId = setInterval(aiTick, intervalMs);
}

function startRecording() {
  if (!previewStream || recording) return;

  let recordStream;
  if (region) {
    regionCanvas = document.createElement('canvas');
    regionCanvas.width = region.w;
    regionCanvas.height = region.h;
    regionCtx = regionCanvas.getContext('2d');
    recordStream = regionCanvas.captureStream(RECORD_FPS);
  } else {
    recordStream = previewStream;
  }

  recordMime = pickMime();
  try {
    mediaRecorder = new MediaRecorder(recordStream, recordMime ? { mimeType: recordMime } : {});
  } catch (err) {
    setStatus(`Could not start recorder: ${err.message}`, true);
    return;
  }

  recordedChunks = [];
  mediaRecorder.ondataavailable = (e) => {
    if (e.data && e.data.size > 0) recordedChunks.push(e.data);
  };
  mediaRecorder.onstop = finalizeRecording;

  recording = true;
  isPaused = false;
  pausedTotal = 0;
  if (region) regionDrawLoop();
  mediaRecorder.start(1000);

  recordStartTime = Date.now();
  els.timer.textContent = '00:00';
  els.timer.classList.add('running');
  recordTimerId = setInterval(tickTimer, 1000);

  aiWasOn = els.aiToggle.checked;
  if (aiWasOn) {
    sessionSummaries = [];
    seenReminders.clear();
    prevSignature = null;
    setTimeout(aiTick, 1500);
    startAiTimer();
  }

  els.record.textContent = 'Stop recording';
  els.record.classList.add('running');
  els.pause.disabled = false;
  els.pause.textContent = 'Pause';
  els.source.disabled = true;
  els.refresh.disabled = true;
  els.clearRegion.disabled = true;
  els.aiToggle.disabled = true;
  els.aiInterval.disabled = true;
  els.selectionLayer.classList.add('locked');
  setStatus(region ? 'Recording region…' : 'Recording…');
}

function pauseRecording() {
  if (!recording || isPaused) return;
  if (mediaRecorder && mediaRecorder.state === 'recording') mediaRecorder.pause();
  isPaused = true;
  pauseStartedAt = Date.now();
  if (aiTimerId) {
    clearInterval(aiTimerId);
    aiTimerId = null;
  }
  els.pause.textContent = 'Resume';
  els.timer.classList.remove('running');
  els.timer.classList.add('paused');
  setStatus('Paused.');
}

function resumeRecording() {
  if (!recording || !isPaused) return;
  pausedTotal += Date.now() - pauseStartedAt;
  isPaused = false;
  if (mediaRecorder && mediaRecorder.state === 'paused') mediaRecorder.resume();
  if (aiWasOn) startAiTimer();
  els.pause.textContent = 'Pause';
  els.timer.classList.remove('paused');
  els.timer.classList.add('running');
  setStatus(region ? 'Recording region…' : 'Recording…');
}

function togglePause() {
  if (!recording) return;
  if (isPaused) resumeRecording();
  else pauseRecording();
}

function stopRecording() {
  if (!recording) return;
  recording = false;
  isPaused = false;

  if (recordTimerId) clearInterval(recordTimerId);
  recordTimerId = null;
  if (aiTimerId) clearInterval(aiTimerId);
  aiTimerId = null;
  if (drawRaf) cancelAnimationFrame(drawRaf);
  drawRaf = null;

  if (mediaRecorder && mediaRecorder.state !== 'inactive') {
    mediaRecorder.stop();
  }
}

async function finalizeRecording() {
  const blob = new Blob(recordedChunks, { type: recordMime || 'video/webm' });
  recordedChunks = [];
  regionCanvas = null;
  regionCtx = null;

  setStatus('Saving recording…');
  try {
    const bytes = new Uint8Array(await blob.arrayBuffer());
    const res = await window.api.saveRecording(bytes);
    if (res.ok) {
      setStatus(`Saved to ${res.path}`);
    } else if (res.canceled) {
      setStatus('Recording discarded (save canceled).');
    } else {
      setStatus(`Could not save: ${res.error || 'unknown error'}`, true);
    }
  } catch (err) {
    setStatus(`Could not save: ${err.message}`, true);
  }

  if (aiWasOn && sessionSummaries.length > 0) {
    const roll = await window.api.summarizeSession(sessionSummaries);
    if (roll.ok) {
      els.summary.textContent = roll.summary;
      addCost(roll.usage);
      for (const reminder of roll.reminders) addReminder(reminder);
    }
  }

  els.timer.classList.remove('running', 'paused');
  els.record.textContent = 'Start recording';
  els.record.classList.remove('running');
  els.pause.disabled = true;
  els.pause.textContent = 'Pause';
  els.source.disabled = false;
  els.refresh.disabled = false;
  els.clearRegion.disabled = false;
  els.aiToggle.disabled = false;
  els.aiInterval.disabled = false;
  els.selectionLayer.classList.remove('locked');
}

function toggleRecording() {
  if (els.record.disabled) return;
  if (recording) stopRecording();
  else startRecording();
}

// --- AI analysis -----------------------------------------------------------

function computeSignature() {
  const r = currentRect();
  if (!r.w || !r.h) return null;
  const ctx = diffCanvas.getContext('2d', { willReadFrequently: true });
  ctx.drawImage(els.video, r.x, r.y, r.w, r.h, 0, 0, 32, 32);
  return ctx.getImageData(0, 0, 32, 32).data;
}

function changeAmount(a, b) {
  if (!a || !b) return Infinity;
  let sum = 0;
  for (let i = 0; i < a.length; i += 4) {
    sum += Math.abs(a[i] - b[i]) + Math.abs(a[i + 1] - b[i + 1]) + Math.abs(a[i + 2] - b[i + 2]);
  }
  return sum / ((a.length / 4) * 3);
}

// Captures the recorded area as a width-capped JPEG to keep token cost low.
function grabFrame() {
  const r = currentRect();
  if (!r.w || !r.h) return null;
  const scale = Math.min(1, 1280 / r.w);
  captureCanvas.width = Math.round(r.w * scale);
  captureCanvas.height = Math.round(r.h * scale);
  const ctx = captureCanvas.getContext('2d');
  ctx.drawImage(els.video, r.x, r.y, r.w, r.h, 0, 0, captureCanvas.width, captureCanvas.height);
  return captureCanvas.toDataURL('image/jpeg', 0.6).split(',')[1];
}

function addCost(usage) {
  if (!usage) return;
  sessionCost += (usage.inputTokens / 1e6) * PRICE_IN + (usage.outputTokens / 1e6) * PRICE_OUT;
  els.cost.textContent = `Cloud cost this session: $${sessionCost.toFixed(4)}`;
}

function addReminder(reminder) {
  const key = reminder.text.trim().toLowerCase();
  if (!key || seenReminders.has(key)) return;
  seenReminders.add(key);

  const empty = els.reminders.querySelector('.empty');
  if (empty) empty.remove();

  const li = document.createElement('li');
  const scheduled = reminder.remindInMinutes > 0;
  li.textContent = scheduled
    ? `${reminder.text}  (in ${reminder.remindInMinutes} min)`
    : reminder.text;
  els.reminders.prepend(li);

  if (scheduled) {
    setTimeout(() => {
      window.api.notify('Reminder', reminder.text);
      li.classList.add('fired');
    }, reminder.remindInMinutes * 60 * 1000);
  }
}

function renderFrameResult(result) {
  els.summary.textContent = result.summary;

  const item = document.createElement('div');
  item.className = 'timeline-item';
  item.innerHTML = `<span class="time">${new Date().toLocaleTimeString()}</span>${escapeHtml(result.summary)}`;
  els.timeline.prepend(item);
  while (els.timeline.children.length > MAX_TIMELINE_ITEMS) {
    els.timeline.lastChild.remove();
  }

  for (const reminder of result.reminders) addReminder(reminder);
}

async function aiTick() {
  if (analyzing || !recording || isPaused || !previewStream) return;

  const signature = computeSignature();
  if (changeAmount(prevSignature, signature) < CHANGE_THRESHOLD) {
    setStatus('Recording — frame unchanged, skipped AI (no cost).');
    return;
  }
  prevSignature = signature;

  const frame = grabFrame();
  if (!frame) return;

  analyzing = true;
  setStatus('Recording — analyzing frame…');
  try {
    const result = await window.api.analyzeScreen(frame);
    if (!result.ok) {
      setStatus(`Recording — AI error: ${result.error}`, true);
    } else {
      sessionSummaries.push(result.summary);
      renderFrameResult(result);
      addCost(result.usage);
      setStatus('Recording…');
    }
  } catch (err) {
    setStatus(`Recording — AI error: ${err.message}`, true);
  } finally {
    analyzing = false;
  }
}

// --- Wiring ----------------------------------------------------------------

els.saveKey.addEventListener('click', saveKey);
els.source.addEventListener('change', () => startPreview(els.source.value));
els.refresh.addEventListener('click', loadSources);
els.clearRegion.addEventListener('click', () => {
  if (!recording) clearRegion();
});
els.record.addEventListener('click', toggleRecording);
els.pause.addEventListener('click', togglePause);

els.selectionLayer.addEventListener('pointerdown', onPointerDown);
window.addEventListener('pointermove', onPointerMove);
window.addEventListener('pointerup', onPointerUp);

window.api.onHotkeyToggle(toggleRecording);
window.api.onHotkeyFailed(() => {
  setStatus('Global hotkey unavailable (in use by another app). Use the button instead.', true);
});

refreshKeyStatus();
loadSources().catch((err) => setStatus(`Could not list sources: ${err.message}`, true));

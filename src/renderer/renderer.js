const els = {
  source: document.getElementById('source'),
  interval: document.getElementById('interval'),
  refresh: document.getElementById('refresh'),
  toggle: document.getElementById('toggle'),
  status: document.getElementById('status'),
  summary: document.getElementById('summary'),
  reminders: document.getElementById('reminders'),
  timeline: document.getElementById('timeline'),
  cost: document.getElementById('cost'),
  video: document.getElementById('video'),
};

// Haiku 4.5 pricing, USD per 1M tokens.
const PRICE_IN = 1.0;
const PRICE_OUT = 5.0;
// Below this mean per-channel pixel difference (0-255), the window counts as unchanged.
const CHANGE_THRESHOLD = 4;
const MAX_TIMELINE_ITEMS = 50;

let stream = null;
let timer = null;
let analyzing = false;
let running = false;
let prevSignature = null;
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

async function loadSources() {
  const sources = await window.api.getCaptureSources();
  els.source.innerHTML = '';
  for (const s of sources) {
    const opt = document.createElement('option');
    opt.value = s.id;
    opt.textContent = `${s.kind === 'screen' ? '[Screen] ' : ''}${s.name}`;
    els.source.appendChild(opt);
  }
}

async function startStream(sourceId) {
  stream = await navigator.mediaDevices.getUserMedia({
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
  els.video.srcObject = stream;
  await els.video.play();
}

function stopStream() {
  if (stream) {
    stream.getTracks().forEach((t) => t.stop());
    stream = null;
  }
  els.video.srcObject = null;
}

function computeSignature() {
  const ctx = diffCanvas.getContext('2d', { willReadFrequently: true });
  ctx.drawImage(els.video, 0, 0, 32, 32);
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

// Captures the current frame as a width-capped JPEG to keep token cost low.
function grabScreenshot() {
  const vw = els.video.videoWidth;
  const vh = els.video.videoHeight;
  if (!vw || !vh) return null;
  const scale = Math.min(1, 1280 / vw);
  captureCanvas.width = Math.round(vw * scale);
  captureCanvas.height = Math.round(vh * scale);
  const ctx = captureCanvas.getContext('2d');
  ctx.drawImage(els.video, 0, 0, captureCanvas.width, captureCanvas.height);
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

function renderResult(result) {
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

async function tick() {
  if (analyzing || !stream) return;

  const signature = computeSignature();
  if (changeAmount(prevSignature, signature) < CHANGE_THRESHOLD) {
    setStatus('Monitoring — window unchanged, skipped (no cost).');
    return;
  }
  prevSignature = signature;

  const imageBase64 = grabScreenshot();
  if (!imageBase64) return;

  analyzing = true;
  setStatus('Analyzing window…');
  try {
    const result = await window.api.analyzeScreen(imageBase64);
    if (!result.ok) {
      setStatus(`Error: ${result.error}`, true);
    } else {
      renderResult(result);
      addCost(result.usage);
      setStatus(`Monitoring — last analyzed ${new Date().toLocaleTimeString()}.`);
    }
  } catch (err) {
    setStatus(`Error: ${err.message}`, true);
  } finally {
    analyzing = false;
  }
}

async function start() {
  const sourceId = els.source.value;
  if (!sourceId) {
    setStatus('Pick a window first.', true);
    return;
  }
  try {
    await startStream(sourceId);
  } catch (err) {
    setStatus(`Could not capture that window: ${err.message}`, true);
    return;
  }

  running = true;
  prevSignature = null;
  els.toggle.textContent = 'Stop';
  els.toggle.classList.add('running');
  els.source.disabled = true;
  els.interval.disabled = true;
  setStatus('Monitoring started…');

  const intervalMs = Math.max(10, Number(els.interval.value) || 30) * 1000;
  setTimeout(tick, 1500);
  timer = setInterval(tick, intervalMs);
}

function stop() {
  running = false;
  if (timer) clearInterval(timer);
  timer = null;
  stopStream();
  els.toggle.textContent = 'Start';
  els.toggle.classList.remove('running');
  els.source.disabled = false;
  els.interval.disabled = false;
  setStatus('Stopped.');
}

els.toggle.addEventListener('click', () => (running ? stop() : start()));
els.refresh.addEventListener('click', loadSources);

loadSources().catch((err) => setStatus(`Could not list windows: ${err.message}`, true));

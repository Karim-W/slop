// kyro's web player. It mirrors the terminal player: one live state
// stream from kyro, the same library sections, zen mode and cover colours.
// "Play here" makes this page a speaker: kyro hands the song over and the
// page plays it, reporting back so the queue, hooks and stats carry on.
'use strict';

// ─── Tiny DOM helpers ───────────────────────────────────────────────────

const $ = (sel, el = document) => el.querySelector(sel);

function h(tag, props, ...kids) {
  const [name, ...classes] = tag.split('.');
  const el = document.createElement(name || 'div');
  if (classes.length) el.className = classes.join(' ');
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'text') el.textContent = v;
    else if (k === 'style') Object.assign(el.style, v);
    else if (k in el && k !== 'list') el[k] = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const k of kids.flat()) if (k != null && k !== false) el.append(k.nodeType ? k : document.createTextNode(String(k)));
  return el;
}

function clock(s) {
  if (!(s > 0)) return '0:00';
  s = Math.round(s);
  const hh = Math.floor(s / 3600), mm = Math.floor(s / 60) % 60, ss = s % 60;
  const p = n => String(n).padStart(2, '0');
  return hh ? `${hh}:${p(mm)}:${p(ss)}` : `${mm}:${p(ss)}`;
}

const lower = s => (s || '').toLowerCase();
const or = (s, d) => (s && s.trim()) ? s : d;
const sortArtist = t => t.albumArtist || t.artist || '';

// ─── Server ─────────────────────────────────────────────────────────────

class Unpaired extends Error {}

const api = {
  async get(path) {
    const r = await fetch(path, { credentials: 'same-origin', cache: 'no-store' });
    if (r.status === 401) throw new Unpaired();
    if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
    return r.json();
  },
  async send(path, body, method = 'POST') {
    const r = await fetch(path, {
      method, credentials: 'same-origin', cache: 'no-store',
      headers: { 'Content-Type': 'application/json', 'X-Kyro': '1' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await r.json().catch(() => ({}));
    if (r.status === 401) throw new Unpaired();
    if (!r.ok) throw new Error(data.error || r.statusText);
    return data;
  },
  async cmd(cmd, extra = {}) {
    try {
      return await api.send('/api/cmd', { cmd, ...extra });
    } catch (e) {
      if (e instanceof Unpaired) return showPairing();
      flash(e.message, false);
    }
  },
};

// ─── State ──────────────────────────────────────────────────────────────

let S = null;                // kyro's state
let recvAt = 0;              // when S arrived (performance.now)
const lib = { rev: -1, tracks: [], byId: new Map(), albums: [], artists: [] };
let lists = { rev: -1, items: [] };
let queue = { rev: '', tracks: [] };
const ui = {
  sec: 'songs', playlist: null, plTracks: [], drill: null,
  filter: '', rows: [], sel: -1, zen: false, modal: null,
};

const store = {
  get(k, d) { try { return localStorage.getItem('kyro.' + k) ?? d; } catch { return d; } },
  set(k, v) { try { localStorage.setItem('kyro.' + k, v); } catch {} },
};

function deviceID() {
  let id = store.get('device');
  if (!id || !/^[A-Za-z0-9_-]{8,64}$/.test(id)) {
    const b = new Uint8Array(12);
    crypto.getRandomValues(b);
    id = btoa(String.fromCharCode(...b)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    store.set('device', id);
  }
  return id;
}
const DEVICE = deviceID();

function guessName() {
  const ua = navigator.userAgent;
  if (/SmartTV|SMART-TV|Tizen|Web0S|webOS|BRAVIA|AFT|CrKey|GoogleTV|Android TV|HbbTV/i.test(ua)) return 'TV';
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return 'iPad';
  if (/Android/.test(ua)) return /Mobile/.test(ua) ? 'Android phone' : 'Android tablet';
  const browser = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Browser';
  const os = /Macintosh/.test(ua) ? 'Mac' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : '';
  return os ? `${browser} on ${os}` : browser;
}
const deviceName = () => store.get('name', guessName());

const tv = /SmartTV|SMART-TV|Tizen|Web0S|webOS|BRAVIA|AFT|CrKey|GoogleTV|Android TV|HbbTV/i.test(navigator.userAgent);
const mobile = /iPhone|iPad|iPod|Android/i.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1);

// ─── Boot and pairing ───────────────────────────────────────────────────

async function boot() {
  const code = new URLSearchParams(location.hash.slice(1)).get('code');
  let hello;
  try { hello = await api.get('/api/hello'); } catch { return setTimeout(boot, 2000); }
  if (!hello.paired && store.get('key')) {
    // The browser dropped its cookie (an app reinstall, a cleanup): the
    // page kept the key, so it comes back paired without a new code.
    try {
      await api.send('/api/resume', { key: store.get('key') });
      hello.paired = true;
    } catch (e) {
      if (e instanceof Unpaired) store.set('key', '');
    }
  }
  if (!hello.paired && code) {
    history.replaceState(null, '', location.pathname);
    if (await pair(code)) return start();
  }
  if (!hello.paired) return showPairing(hello.server);
  if (code) history.replaceState(null, '', location.pathname);
  start();
}

async function pair(code) {
  try {
    const r = await api.send('/api/pair', { code, name: deviceName() });
    if (r.key) store.set('key', r.key);
    return true;
  } catch (e) {
    const err = $('.pair .err');
    if (err) err.textContent = e.message;
    return false;
  }
}

function showPairing(server) {
  stream?.close();
  stream = null;
  store.set('key', ''); // kyro unpaired this browser
  if (el.app) { el.app = null; speaker.on = false; speaker.stop(); }
  const boxes = [];
  const submit = async () => {
    const code = boxes.map(b => b.value).join('');
    if (code.length === 6 && await pair(code)) start();
  };
  for (let i = 0; i < 6; i++) {
    boxes.push(h('input', {
      inputMode: 'numeric', maxLength: 1, autocomplete: 'one-time-code', 'aria-label': `digit ${i + 1}`,
      oninput(e) {
        const v = e.target.value.replace(/\D/g, '');
        if (v.length > 1) { // pasted
          [...v.slice(0, 6)].forEach((c, j) => { if (boxes[j]) boxes[j].value = c; });
          return submit();
        }
        e.target.value = v;
        if (v && boxes[i + 1]) boxes[i + 1].focus();
        submit();
      },
      onkeydown(e) { if (e.key === 'Backspace' && !e.target.value && boxes[i - 1]) boxes[i - 1].focus(); },
    }));
  }
  const app = $('#app');
  app.replaceChildren(h('div.pair', {},
    h('div.panel.dialog', {},
      h('span.ptitle', { text: 'Pair' }),
      h('div.logo.grad', { text: '◖ kyro ◗' }),
      h('div.sub', { text: server ? `Pair this ${deviceName()} with ${server}` : `Pair this ${deviceName()} with kyro` }),
      h('div.code', {}, boxes.slice(0, 3), h('span.gap'), boxes.slice(3)),
      h('div.err'),
      h('p.note', { text: 'In kyro, press w to see the code. Pairing works while that screen is open.' }),
    )));
  boxes[0].focus();
}

// ─── Live stream ────────────────────────────────────────────────────────

let stream = null;

function start() {
  build();
  connect();
  if (store.get('speaker') === 'on') speaker.register().catch(() => {});
  setZen(true); // the big screen first: a TV remote needs no clicks to get there
  clockTick();
  idle();
}

function connect() {
  stream?.close();
  stream = new EventSource('/api/events?device=' + encodeURIComponent(DEVICE));
  stream.addEventListener('state', e => onState(JSON.parse(e.data)));
  stream.addEventListener('cmd', e => speaker.apply(JSON.parse(e.data)));
  stream.onerror = async () => {
    try {
      const hello = await api.get('/api/hello');
      if (!hello.paired) showPairing(hello.server);
    } catch {
      flash('kyro is not reachable · retrying', false);
    }
  };
}

function onState(st) {
  S = st;
  recvAt = performance.now();
  applyTheme(pageTheme());
  if (st.libraryRev !== lib.rev) loadLibrary(st.libraryRev);
  if (st.playlistsRev !== lists.rev) loadPlaylists(st.playlistsRev);
  if (st.queueRev !== queue.rev) loadQueue(st.queueRev);
  speaker.sync();
  render();
}

// pageTheme is the cover's theme while a song plays (each page can turn
// that off with c), else kyro's own.
const coverColours = () => store.get('cover', 'on') !== 'off';
function pageTheme() {
  return (coverColours() && S?.cover) || S?.theme;
}

let themeKey = '';
function applyTheme(t) {
  if (!t) return;
  const key = JSON.stringify(t);
  if (key === themeKey) return;
  themeKey = key;
  const r = document.documentElement.style;
  const roles = { bg: t.bg, surface: t.surface, overlay: t.overlay, border: t.border, text: t.text, subtext: t.subtext,
    muted: t.muted, accent: t.accent, accent2: t.accent2, accent3: t.accent3, success: t.success,
    warning: t.warning, error: t.error, selection: t.selection };
  for (const [k, v] of Object.entries(roles)) if (v) r.setProperty('--' + k, v);
  document.documentElement.style.colorScheme = t.dark ? 'dark' : 'light';
  $('meta[name=theme-color]')?.setAttribute('content', t.bg);
}

async function loadLibrary(rev) {
  lib.rev = rev;
  try {
    const { tracks } = await api.get('/api/library');
    lib.tracks = tracks;
    lib.byId = new Map(tracks.map(t => [t.id, t]));
    lib.albums = groups(tracks, t => [or(t.album, 'Unknown album'), sortArtist(t)]);
    lib.artists = groups(tracks, t => [or(sortArtist(t), 'Unknown artist'), '']);
    rebuild();
  } catch (e) { if (e instanceof Unpaired) showPairing(); }
}

async function loadPlaylists(rev) {
  lists.rev = rev;
  try {
    lists.items = await api.get('/api/playlists');
    if (ui.sec === 'playlist' && ui.playlist) await openPlaylist(ui.playlist, true);
    renderSide();
  } catch {}
}

async function loadQueue(rev) {
  queue.rev = rev;
  try {
    const q = await api.get('/api/queue');
    queue.tracks = q.tracks;
    if (ui.sec === 'queue') rebuild();
  } catch {}
}

function groups(ts, key) {
  const idx = new Map(), out = [];
  for (const t of ts) {
    const [name, sub] = key(t);
    const k = lower(name) + '\0' + lower(sub);
    let g = idx.get(k);
    if (!g) { g = { group: name, sub, tracks: [] }; idx.set(k, g); out.push(g); }
    g.tracks.push(t);
  }
  return out.sort((a, b) => lower(a.group).localeCompare(lower(b.group)));
}

// ─── Position ───────────────────────────────────────────────────────────

function position() {
  if (!S || !S.active) return 0;
  if (speaker.playingHere()) return speaker.audio.currentTime || 0;
  const p = S.pos + (S.paused ? 0 : (performance.now() - recvAt) / 1000);
  return S.duration > 0 ? Math.min(p, S.duration) : p;
}

// ─── Layout ─────────────────────────────────────────────────────────────

const el = {};

const SECTIONS = [
  ['songs', '♪', 'Songs'], ['albums', '◎', 'Albums'], ['artists', '☺', 'Artists'], ['queue', '≡', 'Queue'], ['loved', '♥', 'Loved'],
];

function build() {
  el.crumb = h('span.crumb');
  el.count = h('span.count');
  el.sideScroll = h('div.scroll');
  el.side = h('nav.panel.side', {}, h('span.ptitle', { text: 'Browse' }), el.sideScroll);
  el.filter = h('input', { placeholder: 'filter', 'aria-label': 'filter',
    oninput(e) { ui.filter = e.target.value; rebuild(); },
    onkeydown(e) { if (e.key === 'Escape' || e.key === 'Enter') { if (e.key === 'Escape') { e.target.value = ''; ui.filter = ''; rebuild(); } closeFilter(); } } });
  el.filterBox = h('label.filter', {}, '/', el.filter);
  el.spacer = h('div.spacer');
  el.list = h('div.list', { onscroll: () => paintRows() }, el.spacer);
  el.mainTitle = h('span.ptitle');
  el.main = h('section.panel.main.focus', {}, el.mainTitle, h('span.plabel', { text: '/ filter' }), el.filterBox, el.list);
  el.tabs = h('div.tabs');
  el.now = h('section.panel.now', {}, h('span.ptitle', { text: 'Now playing' }), nowPlaying());
  el.foot = h('div.foot');
  el.bottom = h('div.bottom', {},
    h('button', { text: '◂◂', 'aria-label': 'previous', onclick: () => api.cmd('prev') }),
    el.bPlay = h('button.play', { text: '▶', 'aria-label': 'play or pause', onclick: toggle }),
    h('button', { text: '▸▸', 'aria-label': 'next', onclick: () => api.cmd('next') }),
    h('button', { text: '⇢', 'aria-label': 'play on', onclick: openOutputs }),
    h('button', { text: '◐', 'aria-label': 'zen', onclick: () => setZen(true, true) }),
  );
  el.app = h('div.app', {},
    h('header.head', {}, h('span.logo.grad', { text: '◖ kyro ◗' }), el.crumb, h('span.spacer'), el.count),
    el.side, el.main, el.tabs, el.now, el.foot, el.bottom);
  el.zen = zenView();
  el.layer = h('div');
  $('#app').replaceChildren(el.app, el.zen, el.layer);
  el.zen.hidden = true;
  new ResizeObserver(() => paintRows()).observe(el.list);
  renderSide();
  rebuild();
  footer();
}

function nowPlaying() {
  el.npArt = artBox();
  el.npArt.onclick = () => setZen(true);
  el.npTitle = h('span.grow.title');
  el.npRating = h('span.rating');
  el.npToggles = toggles();
  el.npSub = h('div.sub.grow');
  el.npQuality = h('span.grow.quality');
  el.npViz = viz(14);
  el.npProg = progress();
  el.npPlay = h('button.play', { text: '▶', onclick: toggle, 'aria-label': 'play or pause' });
  el.npVol = h('input', { type: 'range', min: 0, max: 100, step: 1, 'aria-label': 'volume',
    oninput: e => debounce('vol', () => api.cmd('volume', { arg: String(e.target.value) }), 120) });
  el.npOut = h('button.chip', { onclick: openOutputs, title: 'play on (o)' });
  el.npSound = h('button.chip', { onclick: () => api.cmd('sound'), title: 'sound mode (m)' });
  el.npCover = h('button.chip.cover', { text: 'cover colours', onclick: toggleCover, title: 'theme from the cover (c)' });
  return h('div.np', {},
    el.npArt,
    h('div.info', {},
      h('div.line', {}, el.npTitle, el.npRating, el.npToggles),
      h('div.line', {}, el.npSub),
      el.npProg.el,
      h('div.line', {}, el.npQuality, el.npViz.el),
      h('div.line.controls', {},
        h('div.transport', {},
          h('button', { text: '◂◂', onclick: () => api.cmd('prev'), 'aria-label': 'previous' }), el.npPlay,
          h('button', { text: '▸▸', onclick: () => api.cmd('next'), 'aria-label': 'next' })),
        h('span.grow'),
        h('label.vol', {}, '♪', el.npVol),
        el.npSound, el.npCover, el.npOut,
        h('button.chip', { text: 'zen', onclick: () => setZen(true, true), title: 'zen (z)' })),
    ));
}

function artBox(size, fade) {
  const img = h('img', { alt: '', decoding: 'async', onerror() { this.classList.add('broken'); }, onload() { this.classList.remove('broken'); } });
  const ph = h('div.ph');
  const box = h('div.art', {}, img, ph);
  if (fade) return fadingArt(box, img, ph, size);
  box.set = (t, px) => {
    const key = t ? `${t.art}:${px}` : '';
    if (box.key === key) return;
    box.key = key;
    if (!t) { img.removeAttribute('src'); img.classList.add('broken'); ph.textContent = '♪'; return; }
    ph.textContent = initials(t.album || t.title);
    ph.style.filter = `hue-rotate(${hash(t.art) % 360}deg)`;
    img.classList.remove('broken');
    img.src = `/api/art/${encodeURIComponent(t.id)}?s=${px || size || 256}`;
  };
  return box;
}

// fadingArt swaps covers with a crossfade: the new one loads unseen,
// then fades in over the old.
function fadingArt(box, img, ph, size) {
  box.classList.add('fading');
  img.classList.add('broken');
  let key = '';
  box.set = (t, px) => {
    const k = t ? `${t.art}:${px}` : '';
    if (k === key) return;
    key = k;
    ph.textContent = t ? initials(t.album || t.title) : '♪';
    ph.style.filter = t ? `hue-rotate(${hash(t.art) % 360}deg)` : '';
    const shown = () => [...box.querySelectorAll('img')].filter(i => !i.classList.contains('broken'));
    if (!t) { shown().forEach(i => i.classList.add('broken')); return; }
    const next = h('img.in', { alt: '', decoding: 'async' });
    next.onload = () => {
      if (key !== k) return next.remove();
      const old = shown().filter(i => i !== next);
      old.forEach(i => i.classList.add('out'));
      requestAnimationFrame(() => requestAnimationFrame(() => next.classList.remove('in')));
      setTimeout(() => old.forEach(i => i.remove()), 900);
    };
    next.onerror = () => { next.remove(); if (key === k) shown().forEach(i => i.classList.add('broken')); };
    next.src = `/api/art/${encodeURIComponent(t.id)}?s=${px || size}`;
    box.insertBefore(next, ph);
  };
  return box;
}

// colourField paints zen with the cover's own colours: the server sends
// them as a 3×3 grid, toned for the text, and each lands in the matching
// part of the screen, so the art's colours run out to the edges. A new
// song's field fades in over the old one; nothing moves in between.
function colourField() {
  const wrap = h('div.field');
  let key = '';
  return {
    el: wrap,
    set(cells) {
      const k = cells ? cells.join() : '';
      if (k === key) return;
      key = k;
      const old = [...wrap.children];
      old.forEach(o => o.classList.remove('on'));
      setTimeout(() => old.forEach(o => o.remove()), 1600);
      wrap.parentElement?.classList.toggle('fielded', !!cells);
      if (!cells) return;
      const at = i => `${(i % 3) * 50}% ${Math.floor(i / 3) * 50}%`;
      const spots = cells.map((c, i) => i === 4 ? null : `radial-gradient(70% 70% at ${at(i)}, ${c}, transparent)`).filter(Boolean);
      const layer = h('div', { style: { background: `${spots.join(', ')}, ${cells[4]}` } });
      wrap.append(layer);
      requestAnimationFrame(() => requestAnimationFrame(() => layer.classList.add('on')));
    },
  };
}

const initials = s => (s || '♪').split(/\s+/).filter(Boolean).slice(0, 2).map(w => [...w][0].toUpperCase()).join('');
const hash = s => { let x = 7; for (const c of s || '') x = (x * 31 + c.charCodeAt(0)) >>> 0; return x; };

function toggles() {
  const sh = h('button', { text: '⇄', title: 'shuffle (s)', onclick: () => api.cmd('shuffle') });
  const rp = h('button', { text: '↻', title: 'repeat (r)', onclick: () => api.cmd('repeat') });
  const vol = h('span');
  const box = h('span.toggles', {}, sh, rp, vol);
  box.update = () => {
    sh.classList.toggle('on', !!S?.shuffle);
    rp.classList.toggle('on', S && S.repeat !== 'off');
    rp.textContent = S?.repeat === 'one' ? '↻¹' : '↻';
    vol.textContent = `♪ ${S?.volume ?? 0}%`;
  };
  return box;
}

function ratingView(box) {
  const t = S?.track;
  if (!t) { box.replaceChildren(); return; }
  const love = h('button.love', { text: t.loved ? '♥' : '♡', title: 'love (l)', onclick: () => api.cmd('love') });
  love.classList.toggle('on', !!t.loved);
  const stars = [1, 2, 3, 4, 5].map(n => {
    const b = h('button.star', { text: '★', title: `${n} star${n > 1 ? 's' : ''}`, onclick: () => api.cmd('rate', { arg: String(t.stars === n ? 0 : n) }) });
    b.classList.toggle('on', n <= (t.stars || 0));
    return b;
  });
  box.replaceChildren(love, h('span', {}, stars));
}

// progress is "▶ 1:23 ━━━━●──── 4:56". It moves with transforms only and
// writes nothing that didn't change, so it costs next to nothing; the bar
// is a tall touch target and drags like a slider.
function progress() {
  const icon = h('span.icon', { text: '■' });
  const t0 = h('span.t0', { text: '0:00' }), t1 = h('span.t1', { text: '0:00' });
  const fill = h('div.fill'), knob = h('div.knob');
  const bar = h('div.seek', { role: 'slider', 'aria-label': 'seek', tabIndex: -1 }, h('div.rail'), fill, knob);
  let drag = null, width = 0, last = {};
  new ResizeObserver(([e]) => { width = e.contentRect.width; last = {}; }).observe(bar);
  const at = e => {
    const r = bar.getBoundingClientRect();
    return Math.min(Math.max((e.clientX - r.left) / r.width, 0), 1);
  };
  const set = (k, v, fn) => { if (last[k] !== v) { last[k] = v; fn(v); } };
  const self = {
    el: h('div.prog', {}, icon, t0, bar, t1),
    update(pos) {
      const d = S?.duration || 0;
      const f = drag != null ? drag : d > 0 ? Math.min(pos / d, 1) : 0;
      set('icon', !S?.active ? '■' : S.paused ? '❚❚' : '▶', v => icon.textContent = v);
      set('t0', clock(drag != null ? drag * d : pos), v => t0.textContent = v);
      set('t1', clock(d), v => t1.textContent = v);
      set('f', Math.round(f * width * 2) / 2, v => {
        fill.style.transform = `scaleX(${width ? v / width : 0})`;
        knob.style.transform = `translateX(${v}px)`;
      });
      set('knob', !!S?.active, v => knob.style.visibility = v ? '' : 'hidden');
    },
  };
  bar.addEventListener('pointerdown', e => {
    if (!S?.active) return;
    e.preventDefault();
    drag = at(e);
    bar.setPointerCapture(e.pointerId);
    bar.classList.add('drag');
    self.update(position());
  });
  bar.addEventListener('pointermove', e => { if (drag != null) { e.preventDefault(); drag = at(e); self.update(position()); } });
  const end = () => {
    if (drag == null) return;
    const s = drag * (S?.duration || 0);
    drag = null;
    bar.classList.remove('drag');
    if (speaker.playingHere()) speaker.audio.currentTime = s;
    api.cmd('seek', { arg: s.toFixed(1) });
    if (S) { S.pos = s; recvAt = performance.now(); }
  };
  bar.addEventListener('pointerup', end);
  bar.addEventListener('pointercancel', () => { drag = null; bar.classList.remove('drag'); });
  return self;
}

// viz is the level meter: real levels when this page plays and can
// measure them, else the terminal's dance.
function viz(n) {
  const bars = Array.from({ length: n }, (_, i) => h('i', { style: {
    background: `color-mix(in oklab, var(--accent3), var(--accent2) ${Math.round(i / Math.max(n - 1, 1) * 100)}%)`,
  } }));
  return {
    el: h('span.viz', {}, bars),
    update(t) {
      const levels = speaker.levels(n);
      const seed = (S?.track?.title?.length || 0) + (S?.track?.artist?.length || 0);
      bars.forEach((b, i) => {
        let v = 0.12;
        if (levels) v = Math.max(levels[i], 0.06);
        else if (S?.active && !S.paused) v = 0.12 + 0.88 * (Math.sin(t * 4.4 + i * 0.9 + seed) + Math.sin(t * 7.5 + i * 0.4) + 2) / 4;
        b.style.transform = `scaleY(${v.toFixed(2)})`;
      });
    },
  };
}

function zenView() {
  el.zArt = artBox(1024, true);
  el.zField = colourField();
  el.zCover = h('button.chip.cover', { text: 'cover colours', onclick: toggleCover, title: 'colours from the cover (c)' });
  el.zTitle = h('div.ztitle.grad');
  el.zSub = h('div.zsub');
  el.zRating = h('div.rating');
  el.zQuality = h('div.quality');
  el.zViz = viz(16);
  el.zPlay = h('button.play', { text: '▶', onclick: toggle, 'aria-label': 'play or pause' });
  el.zProg = progress();
  el.zToggles = toggles();
  el.zOut = h('button.chip', { onclick: openOutputs, title: 'play on (o)' });
  el.zHere = h('button.chip', { text: 'play here', onclick: () => speaker.playHere(), title: 'play on this device' });
  return h('div.zen', {},
    h('div.glow'),
    el.zField.el,
    h('div.zbar', {},
      h('button.chip', { text: '✕ zen', onclick: () => setZen(false), title: 'exit zen (z)' }),
      h('span.spacer'),
      el.zHere, el.zOut, el.zCover),
    h('div.top', {}, el.zArt, h('div.meta', {}, el.zTitle, el.zSub, el.zRating, el.zQuality, el.zViz.el)),
    h('div.ctl', {},
      h('button', { text: '◂◂', onclick: () => api.cmd('prev'), 'aria-label': 'previous' }), el.zPlay,
      h('button', { text: '▸▸', onclick: () => api.cmd('next'), 'aria-label': 'next' })),
    h('div.bar', {}, el.zProg.el),
    el.zToggles,
    h('div.keys', {}, ...hints(tv
      ? [['OK', 'play / pause'], ['← →', 'previous / next'], ['↑ ↓', 'volume']]
      : [['space', 'play'], ['[ ]', 'prev/next'], ['z', 'exit zen'], ['l', 'love'], ['0-5', 'stars'],
        ['c', 'cover colours'], ['m', 'sound'], ['o', 'play on'], ['←→', 'seek'], ['+-', 'volume'], ['s', 'shuffle'], ['r', 'repeat']])),
  );
}

let idleTimer = 0;
function wake() {
  document.body.classList.remove('idle');
  clearTimeout(idleTimer);
  if (mobile) return; // on a phone a hidden button would swallow the tap
  idleTimer = setTimeout(() => { if (ui.zen && !ui.modal) document.body.classList.add('idle'); }, 4000);
}
function idle() {
  for (const ev of ['pointermove', 'pointerdown', 'touchstart', 'wheel']) document.addEventListener(ev, wake, { passive: true });
  wake();
}

function toggleCover() {
  store.set('cover', coverColours() ? 'off' : 'on');
  applyTheme(pageTheme());
  render();
  flash(coverColours() ? 'Colours follow the cover' : 'Colours follow kyro');
}

function hints(pairs) {
  return pairs.flatMap(([k, d], i) => [i ? '  ' : '', h('b', { text: k }), ' ' + d]);
}

function setZen(on, gesture) {
  ui.zen = on;
  el.zen.hidden = !on;
  if (on && gesture && document.documentElement.requestFullscreen && !document.fullscreenElement && !mobile && store.get('fullscreen') !== 'off') {
    document.documentElement.requestFullscreen().catch(() => {});
  }
  if (!on && document.fullscreenElement) document.exitFullscreen().catch(() => {});
  render();
}

// ─── Render ─────────────────────────────────────────────────────────────

function render() {
  if (!el.app || !S) return;
  const t = S.track;
  const title = t ? t.title : 'Nothing playing';
  document.title = t ? `${t.title} · kyro` : 'kyro';
  el.count.textContent = `${lib.tracks.length} tracks`;

  el.npArt.set(t, 256);
  el.npTitle.textContent = title;
  el.npTitle.classList.toggle('muted', !t);
  el.npSub.replaceChildren(...subline(t));
  ratingView(el.npRating);
  el.npToggles.update();
  el.npQuality.replaceChildren(...quality());
  const play = S.active && !S.paused ? '❚❚' : '▶';
  el.npPlay.textContent = el.zPlay.textContent = el.bPlay.textContent = play;
  if (document.activeElement !== el.npVol) el.npVol.value = S.volume;
  const out = (S.outputs || []).find(o => o.id === S.output);
  el.npOut.textContent = el.zOut.textContent = `⇢ ${out ? (out.id === DEVICE ? 'this device' : out.name) : 'play on'}`;
  el.npSound.textContent = `◆ ${S.sound || 'pure'}`;
  el.npCover.classList.toggle('on', coverColours());
  el.zCover.classList.toggle('on', coverColours());
  el.zField.set(coverColours() && S.cover && S.track ? S.field : null);
  el.zHere.hidden = S.output === DEVICE;

  el.zArt.set(t, 1024);
  el.zTitle.textContent = title;
  el.zTitle.classList.toggle('grad', !!t);
  el.zSub.replaceChildren(...subline(t));
  ratingView(el.zRating);
  el.zQuality.replaceChildren(...quality());
  el.zToggles.update();
  el.zen.classList.toggle('paused', !S.active || S.paused);

  paintRows();
  speaker.metadata();
  clockTick();
}

function subline(t) {
  if (!t) return [h('span.muted', { text: 'pick a song to play' })];
  const parts = [h('span', { text: or(t.artist, 'Unknown artist') })];
  if (t.album) parts.push(h('span.muted', { text: ' — ' }), h('span.al', { text: t.album }));
  return parts;
}

function quality() {
  const t = S?.track;
  if (!t) return [];
  const parts = [h(t.hires ? 'span.hi' : 'span', { text: S.quality || t.quality || '' })];
  const out = (S.outputs || []).find(o => o.id === S.output);
  if (out && out.kind !== 'local') parts.push(h('span', { text: ' · ' }), h('span.out', { text: '⇢ ' + (out.id === DEVICE ? 'this device' : out.name) }));
  // What this device actually receives: the file untouched, or a lossless copy.
  if (out?.id === DEVICE && speaker.form) parts.push(h('span', { text: ` · ${speaker.form}` }));
  else if (S.sound && S.sound !== 'pure') parts.push(h('span', { text: ' · ' }), h('span.mode', { text: '◆ ' + S.sound }));
  if (t.year) parts.push(h('span', { text: ` · ${t.year}` }));
  return parts;
}

let flashTimer = 0;
function flash(text, ok = true) {
  el.foot.className = 'foot ' + (ok ? 'ok' : 'bad');
  el.foot.textContent = (ok ? '✓ ' : '✗ ') + text;
  clearTimeout(flashTimer);
  flashTimer = setTimeout(footer, ok ? 4000 : 8000);
}

function footer() {
  if (!el.foot) return;
  el.foot.className = 'foot';
  el.foot.replaceChildren(...hints([['space', 'play'], ['[ ]', 'prev/next'], ['←→', 'seek'], ['+-', 'volume'], ['l', 'love'],
    ['s', 'shuffle'], ['r', 'repeat'], ['z', 'zen'], ['/', 'filter'], ['o', 'play on'], ['m', 'sound'], ['c', 'cover colours'], ['?', 'keys']]));
}

// The clock moves the time and the bar twice a second, and only while a
// song plays on a page someone can see; the level meter animates on
// computers and TVs only. Nothing runs while paused, idle or hidden, so a
// phone can sleep.
let clockTimer = 0, vizRaf = 0;
function clockTick() {
  const moving = S && S.active && !S.paused && document.visibilityState === 'visible';
  const pos = position();
  if (S) { if (ui.zen) el.zProg.update(pos); else el.npProg.update(pos); }
  clearTimeout(clockTimer);
  clockTimer = moving ? setTimeout(clockTick, 500) : 0;
  if (moving && !mobile && !vizRaf) vizRaf = requestAnimationFrame(vizFrame);
}
let lastViz = 0;
function vizFrame(now) {
  vizRaf = 0;
  if (!S || !S.active || S.paused || document.visibilityState !== 'visible') {
    (ui.zen ? el.zViz : el.npViz).update(0);
    return;
  }
  if (now - lastViz > 66) {
    lastViz = now;
    (ui.zen ? el.zViz : el.npViz).update(now / 1000);
  }
  vizRaf = requestAnimationFrame(vizFrame);
}

// ─── Sidebar, tabs and rows ─────────────────────────────────────────────

function renderSide() {
  if (!el.side) return;
  const loved = lib.tracks.filter(t => t.loved).length;
  const counts = { songs: lib.tracks.length, albums: lib.albums.length, artists: lib.artists.length, queue: queue.tracks.length, loved };
  const item = (key, icon, name, n, on, go) => h('button.item' + (on ? '.on' : ''), { onclick: go }, h('span.ic', { text: icon }), h('span', { text: name }), h('span.n', { text: n ?? '' }));
  el.sideScroll.replaceChildren(
    h('div.heading', { text: 'LIBRARY' }),
    ...SECTIONS.map(([k, ic, name]) => item(k, ic, name, counts[k], ui.sec === k, () => openSection(k))),
    h('div.heading', { text: 'PLAYLISTS' }),
    ...lists.items.map(p => item('pl', '♫', p.name, p.count, ui.sec === 'playlist' && ui.playlist === p.name, () => openPlaylist(p.name))),
  );
  el.tabs.replaceChildren(
    ...SECTIONS.map(([k, , name]) => h('button' + (ui.sec === k ? '.on' : ''), { text: name, onclick: () => openSection(k) })),
    ...lists.items.map(p => h('button' + (ui.sec === 'playlist' && ui.playlist === p.name ? '.on' : ''), { text: '♫ ' + p.name, onclick: () => openPlaylist(p.name) })),
  );
}

function openSection(sec) {
  ui.sec = sec; ui.drill = null; ui.playlist = null; ui.sel = -1;
  el.list.scrollTop = 0;
  rebuild();
  renderSide();
}

async function openPlaylist(name, keep) {
  try {
    const p = await api.get('/api/playlists/' + encodeURIComponent(name));
    ui.plTracks = p.tracks;
  } catch { ui.plTracks = []; }
  if (!keep) { ui.sec = 'playlist'; ui.playlist = name; ui.drill = null; ui.sel = -1; el.list.scrollTop = 0; }
  rebuild();
  renderSide();
}

function rebuild() {
  if (!el.list) return;
  const q = lower(ui.filter.trim());
  const match = t => !q || lower(`${t.title} ${t.artist} ${t.album} ${t.albumArtist || ''} ${t.genre || ''}`).includes(q);
  const tracks = ts => ts.filter(match).map(t => ({ track: t }));
  let rows;
  if (ui.drill) rows = tracks(ui.drill.tracks);
  else switch (ui.sec) {
    case 'songs': rows = tracks(lib.tracks); break;
    case 'albums': rows = lib.albums.filter(g => !q || lower(g.group + ' ' + g.sub).includes(q)).map(g => ({ group: g })); break;
    case 'artists': rows = lib.artists.filter(g => !q || lower(g.group).includes(q)).map(g => ({ group: g })); break;
    case 'queue': rows = queue.tracks.map((t, i) => ({ track: t, qi: i })).filter(r => match(r.track)); break;
    case 'loved': rows = tracks(lib.tracks.filter(t => t.loved)); break;
    case 'playlist': rows = ui.plTracks.map((t, i) => ({ track: t, pi: i })).filter(r => match(r.track)); break;
    default: rows = [];
  }
  ui.rows = rows;
  ui.sel = Math.min(ui.sel, rows.length - 1);
  const names = { songs: 'Songs', albums: 'Albums', artists: 'Artists', queue: 'Queue', loved: 'Loved', playlist: '♫ ' + ui.playlist };
  let crumb = names[ui.sec] || '';
  if (ui.drill) crumb += ' › ' + ui.drill.group;
  el.crumb.textContent = crumb;
  el.mainTitle.textContent = crumb;
  el.list.querySelectorAll('.row').forEach(r => r.remove());
  el.list.querySelector('.empty')?.remove();
  if (!rows.length) el.list.append(h('div.empty', { text: emptyText() }));
  paintRows(true);
  renderSide();
}

function emptyText() {
  if (ui.filter) return 'nothing matches the filter';
  if (!lib.tracks.length) return 'the library is empty · import music in kyro (press i there)';
  if (ui.sec === 'queue') return 'the queue is empty · play something';
  if (ui.sec === 'loved') return 'no loved songs yet · press l while one plays';
  return 'nothing here';
}

let rowH = 42;
const painted = new Map();

// paintRows draws only the rows in view, so a library of any size scrolls
// smoothly.
function paintRows(force) {
  if (!el.list) return;
  const probe = getComputedStyle(el.list).getPropertyValue('--row');
  rowH = parseFloat(probe) || 42;
  if (matchMedia('(max-width: 900px)').matches) rowH = 54;
  el.spacer.style.height = `${ui.rows.length * rowH}px`;
  const top = el.list.scrollTop, view = el.list.clientHeight || 600;
  const from = Math.max(0, Math.floor(top / rowH) - 8), to = Math.min(ui.rows.length, Math.ceil((top + view) / rowH) + 8);
  if (force) { painted.forEach(r => r.remove()); painted.clear(); }
  for (const [i, r] of painted) if (i < from || i >= to) { r.remove(); painted.delete(i); }
  for (let i = from; i < to; i++) {
    let r = painted.get(i);
    if (!r) { r = rowView(ui.rows[i], i); painted.set(i, r); el.spacer.append(r); }
    r.style.top = `${i * rowH}px`;
    const t = ui.rows[i].track;
    const playing = t && S?.active && (ui.rows[i].qi != null ? ui.rows[i].qi === S.index : S.track?.id === t.id);
    r.classList.toggle('playing', !!playing);
    r.classList.toggle('sel', i === ui.sel);
    const idx = r.querySelector('.i');
    if (idx && t) idx.textContent = playing ? (S.paused ? '❚' : '▶') : String(t.no || i + 1);
  }
}

function rowView(r, i) {
  if (r.group) {
    const g = r.group;
    const art = artBox();
    art.classList.add('thumb');
    art.set(g.tracks[0], 96);
    const n = `${g.tracks.length} song${g.tracks.length > 1 ? 's' : ''}`;
    return h('div.row.group', { onclick: () => drill(g) },
      h('span.i', { text: '' }), art,
      h('div.tt', {}, h('span.t', { text: g.group }), h('span.a', { text: [g.sub, matchMedia('(max-width: 900px)').matches ? n : ''].filter(Boolean).join(' · ') })),
      h('span.d', { text: n }),
      acts(g.tracks));
  }
  const t = r.track;
  const art = artBox();
  art.classList.add('thumb');
  art.set(t, 96);
  return h('div.row' + (t.missing ? '.missing' : ''), { onclick: () => playRow(i), ondblclick: e => e.preventDefault() },
    h('span.i'), art,
    h('div.tt', {}, h('span.t', { text: t.title }), h('span.a', { text: or(t.artist, 'Unknown artist') })),
    h('span.al', { text: t.album || '' }),
    h(t.hires ? 'span.q.hi' : 'span.q', { text: t.quality || '' }),
    h('span.d', { text: clock(t.duration) }),
    acts([t]));
}

function acts(ts) {
  const stop = fn => e => { e.stopPropagation(); fn(); };
  const ids = ts.map(t => t.id);
  return h('span.acts', {},
    h('button', { text: '»', title: 'play next', onclick: stop(async () => { await api.cmd('play-next', { ids }); flash('Playing next: ' + describe(ts)); }) }),
    h('button', { text: '+', title: 'add to queue', onclick: stop(async () => { await api.cmd('enqueue', { ids }); flash('Queued ' + describe(ts)); }) }));
}

const describe = ts => ts.length === 1 ? ts[0].title : `${ts.length} songs`;

function drill(g) {
  ui.drill = g; ui.sel = -1;
  el.list.scrollTop = 0;
  rebuild();
}

function playRow(i) {
  const r = ui.rows[i];
  if (!r) return;
  if (r.group) return drill(r.group);
  ui.sel = i;
  if (r.qi != null) return api.cmd('jump', { index: r.qi });
  if (r.pi != null) return api.cmd('play-playlist', { arg: ui.playlist, index: r.pi });
  const ids = ui.rows.filter(x => x.track).map(x => x.track.id);
  const ctx = ui.drill ? `${ui.sec === 'albums' ? 'album' : 'artist'}:${ui.drill.group}` : ui.sec;
  api.cmd('play-tracks', { ids, index: ids.indexOf(r.track.id), arg: ctx });
}

function openFilter() { el.filterBox.classList.add('show'); el.filter.focus(); }
function closeFilter() { if (!ui.filter) el.filterBox.classList.remove('show'); el.filter.blur(); }

// ─── Dialogs ────────────────────────────────────────────────────────────

function dialog(title, ...body) {
  const close = () => { ui.modal = null; el.layer.replaceChildren(); };
  const box = h('div.panel.dialog', { role: 'dialog', 'aria-label': title }, h('span.ptitle', { text: title }), ...body);
  const scrim = h('div.scrim', { onclick: e => { if (e.target === scrim) close(); } }, box);
  ui.modal = { close };
  el.layer.replaceChildren(scrim);
  box.querySelector('button')?.focus();
  return close;
}

function openOutputs() {
  const outs = S?.outputs || [];
  const here = outs.find(o => o.id === DEVICE);
  let close;
  const opt = (o) => h('button.opt' + (o.active ? '.sel' : ''), {
    onclick: async () => {
      close();
      if (o.id === DEVICE) return speaker.playHere();
      await api.cmd('output', { arg: o.id });
    },
  }, h('span.dot', { text: o.active ? '◉' : '◌' }), h('span', { text: o.id === DEVICE ? `This ${deviceName()}` : o.name }),
    h('span.kind', { text: o.kind === 'local' ? 'computer' : o.id === DEVICE ? 'here' : 'browser' }));
  const items = outs.map(opt);
  if (!here) items.push(opt({ id: DEVICE, name: deviceName(), kind: 'browser', active: false }));
  close = dialog('Play on',
    h('div.opts', {}, items),
    here ? h('button.opt', { onclick: () => { close(); speaker.unregister(); } }, h('span.dot', { text: '✕' }), h('span', { text: 'Stop using this device as a speaker' })) : null,
    h('button.opt', { onclick: () => { const n = prompt('Name this device', deviceName()); if (n) { store.set('name', n.slice(0, 40)); if (speaker.on) speaker.register(); } close(); } },
      h('span.dot', { text: '✎' }), h('span', { text: 'Rename this device' })),
    h('p.note', { text: 'The song moves where you pick, at the same spot. Picking this device plays it here, on its speakers or whatever they connect to.' }));
}

function openHelp() {
  const keys = [['space', 'play / pause'], ['] [', 'next / previous'], ['← →', 'seek 5s (zen)'], ['⇧← ⇧→', 'seek 30s'],
    ['+ -', 'volume'], ['s', 'shuffle'], ['r', 'repeat'], ['z', 'zen'], ['l', 'love'], ['0-5', 'stars (zen)'],
    ['/', 'filter'], ['↑ ↓ ⏎', 'move, play'], ['esc', 'back'], ['e / E', 'queue / play next'], ['1-5', 'sections'],
    ['o', 'play on'], ['m', 'sound mode'], ['c', 'cover colours']];
  dialog('Keys', h('div.opts', {}, keys.map(([k, d]) => h('div.line', {}, h('b', { text: k, style: { color: 'var(--accent2)', minWidth: '7em' } }), h('span', { text: d })))));
}

// ─── Speaker ────────────────────────────────────────────────────────────

const speaker = {
  audio: $('#audio'),
  on: false,          // registered with kyro
  applied: 0,         // newest command applied
  track: null,        // what kyro asked this page to play
  next: null,         // queued follow-up {track, url}
  reports: [],
  sending: false,
  switching: false,   // changing songs: the browser's own pause/play are noise
  lastPaused: null,
  gate: null,
  ctx: null, analyser: null, bins: null,

  playingHere() { return this.on && S?.output === DEVICE && !!this.track; },

  async playHere() {
    this.unlock();
    try {
      await this.register();
      await api.cmd('output', { arg: DEVICE });
    } catch (e) { flash(e.message, false); }
  },

  // unlock plays a moment of silence inside the tap, so the browser lets
  // this page play sound later, when kyro hands it a song.
  unlock() {
    const a = this.audio;
    if (!a.src || a.src.endsWith('/silence.wav')) {
      a.src = '/silence.wav';
      a.play().then(() => a.pause()).catch(() => {});
    }
    if (!this.ctx && !mobile && window.AudioContext && /Chrome|Firefox/.test(navigator.userAgent)) {
      try {
        this.ctx = new AudioContext();
        const src = this.ctx.createMediaElementSource(a);
        this.analyser = this.ctx.createAnalyser();
        this.analyser.fftSize = 256;
        this.analyser.smoothingTimeConstant = 0.78;
        src.connect(this.analyser);
        this.analyser.connect(this.ctx.destination);
        this.bins = new Uint8Array(this.analyser.frequencyBinCount);
      } catch { this.ctx = null; this.analyser = null; }
    }
    this.ctx?.resume();
  },

  async register() {
    const formats = formatsSupported();
    await api.send('/api/devices', { id: DEVICE, name: deviceName(), formats });
    this.on = true;
    store.set('speaker', 'on');
    this.loop();
  },

  async unregister() {
    store.set('speaker', 'off');
    this.on = false;
    this.stop();
    try { await api.send('/api/devices/' + DEVICE, undefined, 'DELETE'); } catch {}
  },

  // sync notices kyro forgetting this speaker (it restarted, or the page
  // slept too long) and quietly signs up again.
  sync() {
    if (this.on && S && !(S.outputs || []).some(o => o.id === DEVICE)) {
      this.on = false;
      this.stop();
      this.register().catch(() => {});
    }
  },

  apply(c) {
    if (!c || c.seq <= this.applied) return;
    this.applied = c.seq;
    const a = this.audio;
    switch (c.op) {
      case 'play':
        this.track = c.track;
        this.form = c.form || '';
        this.lastPaused = null; // say "playing" again, so kyro's clock starts
        this.switching = true;
        a.src = c.url;
        if (c.at > 0) a.addEventListener('loadedmetadata', () => { try { a.currentTime = c.at; } catch {} }, { once: true });
        this.report({ kind: 'started', track: c.track, pos: c.at || 0 });
        if (c.paused) { a.pause(); this.switching = false; this.lastPaused = true; } else this.play();
        break;
      case 'next':
        this.next = c.track ? { track: c.track, url: c.url, form: c.form || '' } : null;
        break;
      case 'pause':
        if (c.paused) a.pause(); else this.play();
        break;
      case 'seek':
        try { a.currentTime = c.at; } catch {}
        this.report({ kind: 'pos', pos: c.at });
        break;
      case 'volume':
        a.volume = Math.min(Math.max(c.volume / 100, 0), 1);
        break;
      case 'stop':
        this.stop();
        break;
    }
    this.metadata();
  },

  play() {
    this.ctx?.resume();
    this.audio.play().then(() => this.hideGate()).catch(err => {
      this.switching = false;
      if (err.name === 'NotAllowedError') this.showGate();
      this.report({ kind: 'paused', paused: true });
    });
  },

  stop() {
    const a = this.audio;
    this.track = null; this.next = null; this.switching = false;
    a.pause();
    a.removeAttribute('src');
    a.load();
    this.hideGate();
    if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'none';
  },

  // The browser blocked sound (no tap yet on this page): one tap allows it.
  showGate() {
    if (this.gate) return;
    this.gate = h('div.gate', {}, h('button.btn', { text: '▶ tap to play here', onclick: () => { this.unlock(); this.hideGate(); this.play(); } }));
    document.body.append(this.gate);
  },
  hideGate() { this.gate?.remove(); this.gate = null; },

  report(r) {
    if (!this.on) return;
    if (r.kind === 'pos') this.reports = this.reports.filter(x => x.kind !== 'pos');
    this.reports.push(r);
    if (r.kind !== 'pos') this.flush();
  },

  async flush() {
    if (this.sending || !this.on) return;
    this.sending = true;
    const reports = this.reports;
    this.reports = [];
    try {
      const res = await api.send(`/api/devices/${DEVICE}/reports`, { ack: this.applied, reports });
      for (const c of res.cmds || []) this.apply(c);
    } catch (e) {
      if (e instanceof Unpaired) { this.on = false; showPairing(); }
      else if (/not a speaker/.test(e.message)) { this.on = false; this.register().catch(() => {}); }
      else this.reports = reports.concat(this.reports);
    } finally {
      this.sending = false;
    }
    if (this.reports.some(r => r.kind !== 'pos')) this.flush();
  },

  // loop reports the position each second and picks up any command the
  // live stream missed (a phone's stream sleeps; its reports don't).
  // loop reports the position every ten seconds while a song plays here
  // (kyro fills in between) and, while nothing plays, checks in twice a
  // minute so kyro keeps this speaker listed. Each report also collects
  // any command the live stream missed: a phone's stream sleeps, its
  // reports don't. Rare wake-ups keep the phone's Wi-Fi radio asleep.
  loop() {
    if (this.timer) return;
    let n = 0;
    this.timer = setInterval(() => {
      if (!this.on) return;
      n++;
      const a = this.audio;
      if (this.track && !a.paused) this.report({ kind: 'pos', pos: a.currentTime });
      else if (n % 3) return;
      this.flush();
    }, 10000);
  },

  levels(n) {
    if (!this.analyser || !this.playingHere() || this.audio.paused) return null;
    this.analyser.getByteFrequencyData(this.bins);
    const out = [], use = Math.floor(this.bins.length * 0.7);
    for (let i = 0; i < n; i++) {
      // log-spaced bands: bass gets its own bars
      const a = Math.floor(Math.pow(use, i / n)), b = Math.max(a + 1, Math.floor(Math.pow(use, (i + 1) / n)));
      let m = 0;
      for (let j = a; j < b && j < this.bins.length; j++) m = Math.max(m, this.bins[j]);
      out.push(m / 255);
    }
    return out;
  },

  // metadata feeds the phone's lock screen and Control Center while the
  // song plays here.
  metadata() {
    if (!('mediaSession' in navigator)) return;
    const ms = navigator.mediaSession;
    if (!this.playingHere() || !S?.track) return;
    const t = S.track;
    if (this.metaKey !== t.id) {
      this.metaKey = t.id;
      ms.metadata = new MediaMetadata({ title: t.title, artist: t.artist || '', album: t.album || '',
        artwork: [{ src: `/api/art/${t.id}?s=512`, sizes: '512x512', type: 'image/jpeg' }] });
      ms.setActionHandler('play', () => this.audio.play());
      ms.setActionHandler('pause', () => this.audio.pause());
      ms.setActionHandler('nexttrack', () => api.cmd('next'));
      ms.setActionHandler('previoustrack', () => api.cmd('prev'));
      try { ms.setActionHandler('seekto', d => { this.audio.currentTime = d.seekTime; this.report({ kind: 'pos', pos: d.seekTime }); }); } catch {}
    }
    ms.playbackState = this.audio.paused ? 'paused' : 'playing';
  },
};

(() => {
  const a = speaker.audio;
  const isSilence = () => !speaker.track;
  a.addEventListener('playing', () => {
    if (isSilence()) return;
    speaker.switching = false;
    if (speaker.lastPaused !== false) { speaker.lastPaused = false; speaker.report({ kind: 'paused', paused: false }); }
    speaker.metadata();
  });
  a.addEventListener('pause', () => {
    if (isSilence() || speaker.switching || a.ended) return;
    if (speaker.lastPaused !== true) { speaker.lastPaused = true; speaker.report({ kind: 'paused', paused: true }); }
    speaker.report({ kind: 'pos', pos: a.currentTime });
    speaker.metadata();
  });
  a.addEventListener('durationchange', () => {
    if (!isSilence() && isFinite(a.duration) && a.duration > 0) speaker.report({ kind: 'duration', duration: a.duration });
  });
  a.addEventListener('ended', () => {
    if (isSilence()) return;
    const cur = speaker.track, nx = speaker.next;
    if (nx) {
      // Straight on to the next song, then tell kyro: no round trip gap.
      speaker.track = nx.track; speaker.form = nx.form; speaker.next = null; speaker.switching = true;
      a.src = nx.url;
      speaker.play();
      speaker.report({ kind: 'ended', track: cur, reason: 'eof', continues: true });
      speaker.report({ kind: 'started', track: nx.track, pos: 0 });
    } else {
      speaker.report({ kind: 'ended', track: cur, reason: 'eof' });
    }
  });
  a.addEventListener('error', () => {
    if (isSilence() || !a.error) return;
    speaker.switching = false;
    speaker.report({ kind: 'ended', track: speaker.track, reason: 'error', error: a.error.message || 'the browser can\'t play this song' });
  });
})();

// formatsSupported asks the browser which files it plays itself; kyro
// converts the rest.
function formatsSupported() {
  const a = document.createElement('audio');
  const can = t => a.canPlayType(t) !== '';
  const f = [];
  if (can('audio/mpeg')) f.push('mp3');
  if (can('audio/mp4; codecs="mp4a.40.2"')) f.push('aac');
  if (can('audio/aac')) f.push('adts');
  if (can('audio/mp4; codecs="alac"')) f.push('alac');
  if (can('audio/flac')) f.push('flac');
  if (can('audio/ogg; codecs="opus"')) f.push('opus');
  if (can('audio/ogg; codecs="vorbis"')) f.push('vorbis');
  if (can('audio/wav') || can('audio/wave')) f.push('wav');
  if (can('audio/aiff') || can('audio/x-aiff')) f.push('aiff');
  return f;
}

// ─── Keys ───────────────────────────────────────────────────────────────

const timers = {};
function debounce(k, fn, ms) { clearTimeout(timers[k]); timers[k] = setTimeout(fn, ms); }

function toggle() {
  if (speaker.playingHere()) speaker.unlock();
  api.cmd('toggle');
}

function seekBy(d) {
  const p = position() + d;
  if (speaker.playingHere()) speaker.audio.currentTime = Math.max(p, 0);
  if (S) { S.pos = Math.max(p, 0); recvAt = performance.now(); }
  api.cmd('seek', { arg: (d > 0 ? '+' : '') + d });
}

document.addEventListener('keydown', e => {
  if (!el.app || e.metaKey || e.ctrlKey || e.altKey) return;
  if (e.target.tagName === 'INPUT' && e.target.type !== 'range') return;
  if (ui.modal) { if (e.key === 'Escape') ui.modal.close(); return; }
  const k = e.key;
  const handled = () => e.preventDefault();
  wake();
  // A TV remote has arrows and OK: in zen they skip, change the volume
  // and play or pause, so nothing needs pointing at.
  if (ui.zen && (tv || store.get('remote') === 'on') && !e.target.closest?.('.zbar, .scrim')) {
    switch (k) {
      case 'Enter': handled(); return toggle();
      case 'ArrowRight': handled(); return api.cmd('next');
      case 'ArrowLeft': handled(); return api.cmd('prev');
      case 'ArrowUp': handled(); return api.cmd('volume', { arg: '+5' });
      case 'ArrowDown': handled(); return api.cmd('volume', { arg: '-5' });
    }
  }
  switch (k) {
    case ' ': case 'MediaPlayPause': handled(); return toggle();
    case ']': case '>': case 'MediaTrackNext': return api.cmd('next');
    case '[': case '<': case 'MediaTrackPrevious': return api.cmd('prev');
    case 'ArrowRight': if (ui.zen || e.shiftKey) { handled(); return seekBy(e.shiftKey ? 30 : 5); } break;
    case 'ArrowLeft': if (ui.zen || e.shiftKey) { handled(); return seekBy(e.shiftKey ? -30 : -5); } break;
    case '+': case '=': return api.cmd('volume', { arg: '+5' });
    case '-': case '_': return api.cmd('volume', { arg: '-5' });
    case 's': return api.cmd('shuffle');
    case 'r': return api.cmd('repeat');
    case 'z': return setZen(!ui.zen, true);
    case 'l': return api.cmd('love');
    case 'c': return toggleCover();
    case 'm': return api.cmd('sound');
    case 'o': return openOutputs();
    case '?': return openHelp();
    case '/': handled(); return openFilter();
    case 'Escape':
      if (ui.zen) return setZen(false);
      if (ui.filter) { ui.filter = ''; el.filter.value = ''; closeFilter(); return rebuild(); }
      if (ui.drill) { ui.drill = null; return rebuild(); }
      return;
  }
  if (/^[0-5]$/.test(k) && ui.zen) return api.cmd('rate', { arg: k });
  if (ui.zen) return;
  switch (k) {
    case 'ArrowDown': case 'j': handled(); return moveSel(1);
    case 'ArrowUp': case 'k': handled(); return moveSel(-1);
    case 'PageDown': handled(); return moveSel(10);
    case 'PageUp': handled(); return moveSel(-10);
    case 'Enter': if (ui.sel >= 0) { handled(); playRow(ui.sel); } return;
    case 'Backspace': if (ui.drill) { ui.drill = null; rebuild(); } return;
    case 'e': case 'E': {
      const r = ui.rows[ui.sel];
      if (!r) return;
      const ts = r.group ? r.group.tracks : [r.track];
      api.cmd(k === 'e' ? 'enqueue' : 'play-next', { ids: ts.map(t => t.id) });
      return flash((k === 'e' ? 'Queued ' : 'Playing next: ') + describe(ts));
    }
  }
  if (/^[1-5]$/.test(k)) openSection(SECTIONS[+k - 1][0]);
});

function moveSel(d) {
  if (!ui.rows.length) return;
  ui.sel = Math.min(Math.max(ui.sel + d, 0), ui.rows.length - 1);
  const top = ui.sel * rowH, list = el.list;
  if (top < list.scrollTop) list.scrollTop = top;
  else if (top + rowH > list.scrollTop + list.clientHeight) list.scrollTop = top + rowH - list.clientHeight;
  paintRows();
}

document.addEventListener('visibilitychange', () => {
  if (el.app) clockTick();
  if (document.visibilityState === 'visible' && el.app && (!stream || stream.readyState === EventSource.CLOSED)) connect();
});
document.addEventListener('fullscreenchange', () => { if (!document.fullscreenElement && ui.zen && !mobile) { /* stay in zen, windowed */ } });

boot();

import './style.css';
import './app.css';

import {Application, Events} from '@wailsio/runtime';
import {App} from '../bindings/counterdraft';

const app = document.querySelector('#app');

function section(id, title) {
  return `
    <div class="section">
      <div class="section-head">
        <h2>${title}</h2>
        <span class="manual-tag" id="${id}ManualTag" hidden>manual</span>
        <button class="mini" id="${id}Reset" hidden>reset</button>
        <button class="mini" id="${id}Add" title="Add a hero manually">+</button>
      </div>
      <div class="chips" id="${id}"></div>
      <div class="picker" id="${id}Picker" hidden>
        <select id="${id}Select"></select>
        <button class="mini primary" id="${id}Confirm">Add</button>
      </div>
    </div>`;
}

app.innerHTML = `
  <div class="overlay">
    <div class="header">
      <span class="dot" id="dot"></span>
      <span class="title">counterdraft</span>
      <button class="icon-btn" id="vision" title="Toggle screen reading">vision</button>
      <button class="icon-btn" id="lock" title="Toggle mouse click-through">lock</button>
      <button class="icon-btn" id="quit" title="Quit">x</button>
    </div>
    <div class="body">
      <div class="status" id="status">Starting...</div>
      ${section('enemy', 'Enemy picks')}
      ${section('own', 'Your picks')}
      <div class="section">
        <h2>Recommendations</h2>
        <div id="recs"><span class="empty">waiting for picks</span></div>
      </div>
    </div>
    <div class="footer">
      <div class="path" id="path"></div>
      <div class="path" id="visionStatus"></div>
      <div class="row">
        <button id="install">Install GSI config</button>
        <button class="secondary" id="recheck">Re-detect</button>
      </div>
      <div class="message" id="message"></div>
    </div>
  </div>
`;

const el = {};
for (const id of ['dot', 'status', 'enemy', 'own', 'recs', 'path', 'message',
  'lock', 'quit', 'install', 'recheck', 'vision', 'visionStatus',
  'enemyManualTag', 'enemyReset', 'enemyAdd', 'enemyPicker', 'enemySelect', 'enemyConfirm',
  'ownManualTag', 'ownReset', 'ownAdd', 'ownPicker', 'ownSelect', 'ownConfirm']) {
  el[id] = document.getElementById(id);
}

let state = {clickThrough: false, clickThroughSupported: false};
let heroList = [];
const pickerOpen = {enemy: false, own: false};

function esc(s) {
  const d = document.createElement('div');
  d.textContent = s == null ? '' : String(s);
  return d.innerHTML;
}

function chipHtml(h, side) {
  return `<span class="chip ${side}">${esc(h.name)}<button class="chip-x" data-id="${h.id}" title="Remove">&times;</button></span>`;
}

function chips(list, side) {
  if (!list || list.length === 0) {
    return '<span class="empty">none yet — use + to add</span>';
  }
  return list.map(h => chipHtml(h, side)).join('');
}

function renderRecs(recs) {
  if (!recs || recs.length === 0) {
    return '<span class="empty">no suggestions for this draft</span>';
  }
  return recs.map(r => `
    <div class="rec">
      <span class="score">${r.score}</span>
      <div>
        <div class="name">${esc(r.name)}</div>
        <div class="why">${(r.reasons || []).map(esc).join(' &middot; ')}</div>
      </div>
    </div>
  `).join('');
}

function pickedIds() {
  const ids = new Set();
  for (const h of [...(state.enemy || []), ...(state.own || [])]) ids.add(h.id);
  return ids;
}

function populateSelect(side) {
  const sel = el[side + 'Select'];
  const current = sel.value;
  const picked = pickedIds();
  sel.innerHTML = heroList
    .filter(h => !picked.has(h.id))
    .map(h => `<option value="${h.id}">${esc(h.name)}</option>`)
    .join('');
  if (current && Array.from(sel.options).some(o => o.value === current)) {
    sel.value = current;
  }
}

function setPicker(side, open) {
  pickerOpen[side] = open;
  el[side + 'Picker'].hidden = !open;
  if (open) populateSelect(side);
}

function render(s) {
  if (!s) return;
  state = s;

  el.status.textContent = s.status || 'Listening...';
  el.dot.className = 'dot' + (s.enemy && s.enemy.length > 0 ? ' live' : '');

  el.enemy.innerHTML = chips(s.enemy, 'enemy');
  el.own.innerHTML = chips(s.own, 'own');
  el.recs.innerHTML = renderRecs(s.recommendations);

  el.enemyManualTag.hidden = !s.enemyManual;
  el.ownManualTag.hidden = !s.ownManual;
  el.enemyReset.hidden = !s.enemyManual;
  el.ownReset.hidden = !s.ownManual;

  if (pickerOpen.enemy) populateSelect('enemy');
  if (pickerOpen.own) populateSelect('own');

  el.path.textContent = s.configPath
    ? `GSI config: ${s.configPath}`
    : (s.candidates && s.candidates.length
      ? `Detected: ${s.candidates[0]}`
      : (s.serverUrl ? `Listening on ${s.serverUrl}` : ''));

  el.lock.disabled = !s.clickThroughSupported;
  el.lock.className = 'icon-btn' + (s.clickThrough ? ' active' : '');
  el.lock.title = s.clickThroughSupported
    ? 'Toggle mouse click-through'
    : 'Click-through is only implemented on Windows';

  el.vision.className = 'icon-btn' + (s.visionEnabled ? ' active' : '');
  el.vision.title = s.visionAvailable
    ? 'Toggle screen reading (reads enemy picks from the top bar)'
    : 'Screen reading unavailable';
  el.visionStatus.textContent = s.visionStatus || '';
  el.visionStatus.hidden = !s.visionStatus;
}

function flash(msg) {
  el.message.textContent = msg;
  setTimeout(() => {
    if (el.message.textContent === msg) el.message.textContent = '';
  }, 4000);
}

async function removePick(side, id) {
  try {
    await App.RemovePick(side, id);
  } catch (err) {
    flash(String(err));
  }
}

async function addPick(side) {
  const sel = el[side + 'Select'];
  if (!sel.value) return;
  try {
    await App.AddPick(side, Number(sel.value));
    setPicker(side, false);
  } catch (err) {
    flash(String(err));
  }
}

for (const side of ['enemy', 'own']) {
  el[side].addEventListener('click', e => {
    const btn = e.target.closest('button[data-id]');
    if (btn) removePick(side, Number(btn.dataset.id));
  });
  el[side + 'Add'].addEventListener('click', () => setPicker(side, !pickerOpen[side]));
  el[side + 'Confirm'].addEventListener('click', () => addPick(side));
  el[side + 'Reset'].addEventListener('click', async () => {
    try {
      await App.ResetSide(side);
    } catch (err) {
      flash(String(err));
    }
  });
}

el.lock.addEventListener('click', async () => {
  try {
    await App.SetClickThrough(!state.clickThrough);
  } catch (err) {
    flash(String(err));
  }
});

el.vision.addEventListener('click', async () => {
  try {
    await App.SetVision(!state.visionEnabled);
  } catch (err) {
    flash(String(err));
  }
});

el.quit.addEventListener('click', () => Application.Quit());

el.install.addEventListener('click', async () => {
  try {
    flash('GSI config written: ' + (await App.InstallConfig()));
  } catch (err) {
    flash('Install failed: ' + err);
  }
});

el.recheck.addEventListener('click', async () => {
  render(await App.GetState());
  flash('Re-detected Dota 2 install locations.');
});

Events.On('draft:update', e => render(e.data));
App.GetState().then(render).catch(err => flash(String(err)));
App.Heroes().then(h => {
  heroList = h || [];
  if (pickerOpen.enemy) populateSelect('enemy');
  if (pickerOpen.own) populateSelect('own');
}).catch(err => flash(String(err)));

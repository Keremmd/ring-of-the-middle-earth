/**
 * Ring of the Middle Earth — Game Client
 * Vanilla JS + SSE — No React/Vue/Angular
 */

// ─── CONFIG ───────────────────────────────────────────────────────────────────
// Always use same origin — nginx proxies /game, /order, /events, /analysis to Go
const API_BASE = window.location.origin;

const params = new URLSearchParams(window.location.search);
const PLAYER_ID = params.get('playerId') || 'light';
const IS_LIGHT = PLAYER_ID === 'light' || PLAYER_ID === 'free-peoples' || PLAYER_ID === 'player1';

// ─── STATE ────────────────────────────────────────────────────────────────────
let gameState = {
  turn: 0,
  status: 'WAITING',
  maxTurns: 40,
  turnDurationSeconds: 60,
  hiddenUntilTurn: 3,
  units: {},
  regions: {},
  paths: {},
  ringBearerRegion: '',
  selectedUnit: null,
  eventHistory: [],
};

// Static game data
const REGION_NAMES = {
  'the-shire': 'The Shire', 'bree': 'Bree', 'tharbad': 'Tharbad',
  'weathertop': 'Weathertop', 'rivendell': 'Rivendell', 'fangorn': 'Fangorn',
  'fords-of-isen': 'Fords of Isen', 'rohan-plains': 'Rohan Plains', 'moria': 'Moria',
  'helms-deep': "Helm's Deep", 'isengard': 'Isengard', 'edoras': 'Edoras',
  'lothlorien': 'Lothlórien', 'dead-marshes': 'Dead Marshes', 'emyn-muil': 'Emyn Muil',
  'minas-tirith': 'Minas Tirith', 'ithilien': 'Ithilien', 'osgiliath': 'Osgiliath',
  'minas-morgul': 'Minas Morgul', 'cirith-ungol': 'Cirith Ungol',
  'mordor': 'Mordor', 'mount-doom': 'Mount Doom',
};

const ALL_PATHS = [
  'shire-to-bree','bree-to-weathertop','bree-to-rivendell','bree-to-tharbad',
  'shire-to-tharbad','weathertop-to-rivendell','rivendell-to-moria',
  'rivendell-to-lothlorien','moria-to-lothlorien','lothlorien-to-emyn-muil',
  'lothlorien-to-rohan-plains','rohan-plains-to-fangorn','rohan-plains-to-edoras',
  'rohan-plains-to-minas-tirith','fangorn-to-isengard','isengard-to-rohan-plains',
  'tharbad-to-fords-of-isen','fords-of-isen-to-isengard','fords-of-isen-to-helms-deep',
  'fords-of-isen-to-edoras','edoras-to-helms-deep','helms-deep-to-isengard',
  'edoras-to-minas-tirith','emyn-muil-to-dead-marshes','emyn-muil-to-ithilien',
  'dead-marshes-to-ithilien','dead-marshes-to-mordor','ithilien-to-minas-tirith',
  'ithilien-to-osgiliath','ithilien-to-cirith-ungol','minas-tirith-to-osgiliath',
  'osgiliath-to-minas-morgul','minas-morgul-to-cirith-ungol','minas-morgul-to-mordor',
  'cirith-ungol-to-mordor','cirith-ungol-to-mount-doom','mordor-to-mount-doom',
];

// ─── INITIALIZATION ───────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  initUI();
  loadMapSVG();
  populateSelects();
  connectSSE();
  fetchGameState();

  if (!IS_LIGHT) {
    document.getElementById('side-badge').textContent = 'The Shadow';
    document.getElementById('side-badge').className = 'dark';
    document.querySelectorAll('.light-only').forEach(el => el.style.display = 'none');
    document.querySelectorAll('.dark-only').forEach(el => el.style.display = '');
  } else {
    document.querySelectorAll('.light-only').forEach(el => el.style.display = '');
    document.querySelectorAll('.dark-only').forEach(el => el.style.display = 'none');
  }
});

function initUI() {
  const sideBadge = document.getElementById('side-badge');
  sideBadge.textContent = IS_LIGHT ? 'Free Peoples' : 'The Shadow';
  sideBadge.className = IS_LIGHT ? 'light' : 'dark';
}

// ─── MAP SVG + ZOOM ───────────────────────────────────────────────────────────
let mapScale = 1;
let mapTransX = 0;
let mapTransY = 0;

function applyMapTransform() {
  const svg = document.querySelector('#map-svg-wrapper svg');
  if (svg) {
    svg.style.transform = `scale(${mapScale}) translate(${mapTransX}px, ${mapTransY}px)`;
    svg.style.transformOrigin = 'center center';
  }
}
function mapZoom(factor) {
  mapScale = Math.min(3, Math.max(0.5, mapScale * factor));
  applyMapTransform();
}
function mapReset() {
  mapScale = 1; mapTransX = 0; mapTransY = 0;
  applyMapTransform();
}

function loadMapSVG() {
  fetch('MiddleEarthMap.svg')
    .then(r => r.text())
    .then(svg => {
      const wrapper = document.getElementById('map-svg-wrapper');
      wrapper.innerHTML = svg;
      addMapInteractivity();
    })
    .catch(() => {
      document.getElementById('map-svg-wrapper').innerHTML =
        '<div class="empty-state" style="color:var(--text-secondary);">Map unavailable</div>';
    });
}

function addMapInteractivity() {
  // We attach click listeners to region nodes in the SVG
  // The SVG uses text elements to identify regions
  const svgEl = document.querySelector('#map-svg-wrapper svg');
  if (!svgEl) return;

  // Add click handler on the SVG to detect region clicks
  svgEl.addEventListener('click', (e) => {
    const g = e.target.closest('g[transform]');
    if (g) {
      const text = g.querySelector('text');
      if (text) {
        const regionName = text.textContent.trim();
        const regionId = Object.keys(REGION_NAMES).find(k =>
          REGION_NAMES[k].toLowerCase() === regionName.toLowerCase()
        );
        if (regionId) {
          showRegionPopup(regionId, e.clientX, e.clientY);
        }
      }
    }
  });
}

function showRegionPopup(regionId, x, y) {
  const popup = document.getElementById('region-popup');
  const region = gameState.regions[regionId];

  document.getElementById('popup-name').textContent = REGION_NAMES[regionId] || regionId;
  document.getElementById('popup-terrain').textContent = region?.terrain || '—';

  const ctrl = region?.controlledBy || 'Unknown';
  const ctrlEl = document.getElementById('popup-control');
  ctrlEl.textContent = ctrl.replace('_', ' ');
  ctrlEl.style.color = ctrl === 'FREE_PEOPLES' ? 'var(--light-blue)'
    : ctrl === 'SHADOW' ? '#e07060' : 'var(--text-muted)';

  const threatEl = document.getElementById('popup-threat');
  const threat = region?.threatLevel ?? '—';
  threatEl.textContent = threat;
  threatEl.style.color = threat >= 4 ? 'var(--danger)' : threat >= 2 ? 'var(--warning)' : 'var(--success)';

  const unitsHere = Object.values(gameState.units).filter(u => u.region === regionId);
  document.getElementById('popup-units').textContent = unitsHere.length > 0
    ? unitsHere.map(u => u.id).join(', ')
    : 'None';

  const px = Math.min(x + 12, window.innerWidth - 220);
  const py = Math.min(y + 12, window.innerHeight - 170);
  popup.style.left = px + 'px';
  popup.style.top  = py + 'px';
  popup.style.display = 'block';

  clearTimeout(popup._timeout);
  popup._timeout = setTimeout(() => popup.style.display = 'none', 3500);
}

// ─── UI POPULATION ────────────────────────────────────────────────────────────
function populateSelects() {
  const regionSelect = document.getElementById('target-region-select');
  Object.entries(REGION_NAMES).forEach(([id, name]) => {
    const opt = document.createElement('option');
    opt.value = id;
    opt.textContent = name;
    regionSelect.appendChild(opt);
  });

  const pathSelect = document.getElementById('target-path-select');
  ALL_PATHS.forEach(pid => {
    const opt = document.createElement('option');
    opt.value = pid;
    opt.textContent = pid;
    pathSelect.appendChild(opt);
  });
}

const UNIT_ICONS = {
  'ring-bearer': '💍', 'gandalf': '🧙', 'aragorn': '👑', 'legolas': '🏹',
  'gimli': '⚒', 'gondor-army': '🛡', 'rohan-cavalry': '🐴',
  'sauron': '👁', 'witch-king': '💀', 'saruman': '🔮',
  'nazgul-2': '🦅', 'nazgul-3': '🦅', 'uruk-hai-legion': '⚔',
};

function renderUnits(units) {
  const container = document.getElementById('unit-list');
  container.innerHTML = '';

  const allUnits = Object.values(units);
  if (allUnits.length === 0) {
    container.innerHTML = '<div class="empty-st">Waiting for game state…</div>';
    return;
  }

  // Update count
  const countEl = document.getElementById('unit-count');
  if (countEl) countEl.textContent = allUnits.length + ' units';

  allUnits.forEach(unit => {
    const card = document.createElement('div');
    let mod = '';
    if (unit.status === 'DESTROYED')  mod = 'destroyed';
    if (unit.status === 'RESPAWNING') mod = 'respawning';
    card.className = `unit-card ${mod}`;
    if (gameState.selectedUnit === unit.id) card.classList.add('selected');
    card.dataset.unitId = unit.id;
    card.onclick = () => selectUnit(unit.id);

    const pips = Array.from({length: 5}, (_, i) =>
      `<div class="str-pip ${i < (unit.strength || 0) ? '' : 'off'}"></div>`
    ).join('');

    const badgeCls = unit.status === 'ACTIVE' ? 'b-active'
      : unit.status === 'RESPAWNING' ? 'b-respawn' : 'b-dead';

    const isHiddenRB = unit.id === 'ring-bearer' && !IS_LIGHT;
    const regionDisplay = isHiddenRB
      ? 'Location hidden'
      : (REGION_NAMES[unit.region] || unit.region || '?');

    const icon = UNIT_ICONS[unit.id] || '⚔';
    const extra = unit.cooldown > 0
      ? `<span class="uc-cd">⏳ ${unit.cooldown}cd</span>`
      : unit.respawnTurns > 0
        ? `<span class="uc-cd" style="color:var(--c-blue);">↺ ${unit.respawnTurns}t</span>`
        : '';

    card.innerHTML = `
      <div class="uc-top">
        <div>
          <div class="uc-name"><span class="uc-icon">${icon}</span>${unit.name || unit.id}</div>
          <div class="uc-sub">${unit.class || ''}</div>
        </div>
        <span class="uc-badge ${badgeCls}">${unit.status || 'ACTIVE'}</span>
      </div>
      <div class="uc-loc">
        <div class="uc-loc-dot"></div>${regionDisplay}
      </div>
      <div class="uc-footer">
        <div class="str-bar">${pips}</div>
        ${extra}
      </div>
    `;

    container.appendChild(card);
  });
}

function selectUnit(unitId) {
  gameState.selectedUnit = unitId;
  const unit = gameState.units[unitId] || {};

  document.querySelectorAll('.unit-card').forEach(c => {
    c.classList.toggle('selected', c.dataset.unitId === unitId);
  });

  const nameEl = document.getElementById('selected-unit-name');
  nameEl.textContent = unit.name || unitId;
  nameEl.classList.remove('empty');
  document.getElementById('submit-btn').disabled = false;

  addEvent(`Selected: ${unit.name || unitId}`, 'info');
}

// ─── ORDER HANDLING ───────────────────────────────────────────────────────────
function onOrderTypeChange() {
  const type = document.getElementById('order-type-select').value;
  const routeInput = document.getElementById('route-input');
  const targetRegionInput = document.getElementById('target-region-input');
  const targetPathInput = document.getElementById('target-path-input');

  routeInput.style.display = 'none';
  targetRegionInput.style.display = 'none';
  targetPathInput.style.display = 'none';

  switch (type) {
    case 'ASSIGN_ROUTE':
    case 'REDIRECT_UNIT':
      routeInput.style.display = '';
      break;
    case 'ATTACK_REGION':
    case 'REINFORCE_REGION':
    case 'DEPLOY_NAZGUL':
      targetRegionInput.style.display = '';
      break;
    case 'BLOCK_PATH':
    case 'SEARCH_PATH':
    case 'MAIA_ABILITY':
      targetPathInput.style.display = '';
      break;
    case 'FORTIFY_REGION':
    case 'DESTROY_RING':
      break;
  }
}

async function submitOrder() {
  const unitId = gameState.selectedUnit;
  if (!unitId) { showToast('Select a unit first', 'warning'); return; }

  const orderType = document.getElementById('order-type-select').value;
  if (!orderType) { showToast('Select an order type', 'warning'); return; }

  const order = {
    orderType,
    playerId: PLAYER_ID,
    unitId,
    turn: gameState.turn,
  };

  switch (orderType) {
    case 'ASSIGN_ROUTE':
      order.pathIds = document.getElementById('path-ids-input').value.split(',').map(s => s.trim()).filter(Boolean);
      break;
    case 'REDIRECT_UNIT':
      order.newPathIds = document.getElementById('path-ids-input').value.split(',').map(s => s.trim()).filter(Boolean);
      break;
    case 'ATTACK_REGION':
    case 'REINFORCE_REGION':
    case 'DEPLOY_NAZGUL':
      order.targetRegion = document.getElementById('target-region-select').value;
      break;
    case 'BLOCK_PATH':
    case 'SEARCH_PATH':
      order.pathId = document.getElementById('target-path-select').value;
      break;
    case 'MAIA_ABILITY':
      order.targetPathId = document.getElementById('target-path-select').value;
      break;
  }

  try {
    const res = await fetch(`${API_BASE}/order`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(order),
    });

    if (res.ok) {
      showToast(`Order submitted: ${orderType}`, 'success');
      addEvent(`Order: ${orderType} for ${unitId}`, 'success');
    } else {
      const err = await res.text();
      showToast(`Order rejected: ${err}`, 'danger');
      addEvent(`Rejected: ${err}`, 'danger');
    }
  } catch (e) {
    showToast('Network error', 'danger');
  }
}

// ─── GAME CONTROLS ────────────────────────────────────────────────────────────
async function resetGame(options = {}) {
  const { skipConfirm = false, message = 'Game reset — Turn 1' } = options;

  if (!skipConfirm) {
    const ok = confirm(
      'Oyunu sıfırlamak istiyor musun?\n\nTur 1\'e döner, tüm emirler ve event log temizlenir. (Yeni sekme açınca da sunucudaki oyun devam eder — sıfırlamak için bunu kullan.)'
    );
    if (!ok) return;
  }

  document.getElementById('win-overlay')?.classList.remove('show');
  gameState.gameOverShown = true;

  try {
    const res = await fetch(`${API_BASE}/game/start`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: 'HVH' }),
    });
    if (!res.ok) {
      showToast('Reset failed', 'danger');
      gameState.gameOverShown = false;
      return;
    }
  } catch (e) {
    showToast('Network error', 'danger');
    gameState.gameOverShown = false;
    return;
  }

  gameState.turn = 0;
  gameState.status = 'ACTIVE';
  gameState.winner = '';
  gameState.units = {};
  gameState.regions = {};
  gameState.paths = {};
  gameState.ringBearerRegion = '';
  gameState.selectedUnit = null;
  gameState.eventHistory = [];

  document.getElementById('event-log').innerHTML = '';
  document.getElementById('win-event-log').innerHTML = '';
  document.getElementById('win-event-count').textContent = '0 events';
  document.getElementById('unit-list').innerHTML = '<div class="empty-st">Waiting for game state…</div>';
  const fill = document.getElementById('turn-progress-fill');
  if (fill) fill.style.width = '0%';
  document.getElementById('turn-badge').textContent = 'Turn 1 / ' + gameState.maxTurns;
  document.getElementById('event-count').textContent = '0';
  document.getElementById('start-btn').disabled = false;

  addEvent(message, 'gold');
  showToast('Oyun sıfırlandı — Turn 1', 'success');

  setTimeout(() => {
    gameState.gameOverShown = false;
    fetchGameState();
  }, 1500);
}

async function startGame() {
  await resetGame({ skipConfirm: true, message: 'Game started — HvH mode' });
  document.getElementById('start-btn').disabled = true;
  showToast('Game started!', 'success');
}

// ─── STATE FETCHING ───────────────────────────────────────────────────────────
async function fetchGameState() {
  try {
    const res = await fetch(`${API_BASE}/game/state?playerId=${PLAYER_ID}`);
    if (!res.ok) return;
    const data = await res.json();
    applyGameState(data);
  } catch (e) {
    // silent
  }
}

function applyGameState(data) {
  if (!data) return;

  gameState.turn = data.turn || 0;
  gameState.status = data.status || gameState.status;
  gameState.winner = data.winner || '';
  if (data.maxTurns) gameState.maxTurns = data.maxTurns;
  if (data.turnDurationSeconds) gameState.turnDurationSeconds = data.turnDurationSeconds;
  if (data.hiddenUntilTurn != null) gameState.hiddenUntilTurn = data.hiddenUntilTurn;

  let turnLabel = `Turn ${gameState.turn} / ${gameState.maxTurns}`;
  if (data.status === 'FINISHED') turnLabel += ' · ENDED';
  document.getElementById('turn-badge').textContent = turnLabel;

  const pct = Math.min((gameState.turn / gameState.maxTurns) * 100, 100);
  const fill = document.getElementById('turn-progress-fill');
  if (fill) fill.style.width = pct + '%';

  // Build unit map
  if (data.units) {
    data.units.forEach(u => {
      gameState.units[u.id] = u;
    });
  }

  // Build region map
  if (data.regions) {
    if (Array.isArray(data.regions)) {
      data.regions.forEach(r => { gameState.regions[r.id] = r; });
    } else {
      gameState.regions = data.regions;
    }
  }

  // Build path map
  if (data.paths) {
    if (Array.isArray(data.paths)) {
      data.paths.forEach(p => { gameState.paths[p.id] = p; });
    } else {
      gameState.paths = data.paths;
    }
  }

  renderUnits(gameState.units);
  updateRingBearerTracker();

  // Check game over from polling
  if (data.status === 'FINISHED' && data.winner && !gameState.gameOverShown) {
    gameState.gameOverShown = true;
    handleGameOver({ winner: data.winner, turn: data.turn, cause: 'from-state' });
  }
}

function updateRingBearerTracker() {
  if (IS_LIGHT) {
    const rbEl = document.getElementById('rb-location');
    const rbStatus = document.getElementById('rb-status');
    const tracker = document.getElementById('rb-tracker');
    tracker.style.display = '';

    const rb = gameState.units['ring-bearer'];
    if (rb) {
      const region = rb.region || gameState.ringBearerRegion || 'Unknown';
      rbEl.textContent = REGION_NAMES[region] || region;
      rbStatus.textContent = rb.route && rb.route.length > 0
        ? `Route: ${rb.route.length} paths remaining`
        : 'No route assigned';
    }
  }
}

// ─── SSE CONNECTION ───────────────────────────────────────────────────────────
function connectSSE() {
  const url = `${API_BASE}/events?playerId=${PLAYER_ID}`;
  const source = new EventSource(url);

  source.onopen = () => {
    document.getElementById('status-text').textContent = 'Connected';
    document.getElementById('status-dot').style.background = 'var(--success)';
    addEvent('SSE connected', 'success');
  };

  source.onmessage = (e) => {
    try {
      const event = JSON.parse(e.data);
      handleServerEvent(event);
    } catch (err) {
      // non-JSON event
    }
  };

  source.onerror = () => {
    document.getElementById('status-text').textContent = 'Reconnecting...';
    document.getElementById('status-dot').style.background = 'var(--warning)';
    addEvent('SSE disconnected, reconnecting...', 'warning');
  };
}

function handleServerEvent(event) {
  const type = event.type || event.Type || '';

  switch (type) {
    case 'WorldStateSnapshot':
    case 'game.broadcast':
    case 'world_state':
      fetchGameState();
      if (event.status === 'FINISHED' || event.winner) {
        handleGameOver(event);
      }
      break;

    case 'RingBearerMoved':
      if (IS_LIGHT) {
        gameState.ringBearerRegion = event.trueRegion || event.TrueRegion;
        const rb = gameState.units['ring-bearer'] || {};
        rb.region = gameState.ringBearerRegion;
        gameState.units['ring-bearer'] = rb;
        updateRingBearerTracker();
        addEvent(`Ring Bearer moved to ${REGION_NAMES[gameState.ringBearerRegion] || gameState.ringBearerRegion}`, 'gold');
        showToast(`Ring Bearer: ${REGION_NAMES[gameState.ringBearerRegion]}`, 'info');
      }
      break;

    case 'RingBearerDetected':
    case 'RingBearerSpotted':
      if (!IS_LIGHT) {
        const region = event.regionId || event.pathId;
        document.getElementById('last-detected').textContent =
          `Detected at: ${REGION_NAMES[region] || region} (Turn ${event.turn})`;
        addEvent(`DETECTED: Ring Bearer at ${REGION_NAMES[region] || region}`, 'danger');
        showToast(`Ring Bearer detected at ${REGION_NAMES[region] || region}!`, 'danger');
      }
      break;

    case 'UnitMoved':
    case 'NazgulDeployed': {
      const payload = event.payload || '';
      const parts = typeof payload === 'string' ? payload.split('->') : [];
      if (parts.length === 2) {
        addEvent(`${parts[0]} → ${REGION_NAMES[parts[1]] || parts[1]}`, 'info');
      } else {
        addEvent(`Unit moved: ${payload}`, 'info');
      }
      fetchGameState();
      break;
    }

    case 'BattleResolved':
      addEvent(`Battle at ${REGION_NAMES[event.regionId] || event.regionId}: ${event.attackerWon ? 'Attacker won' : 'Defender held'}`, 'warning');
      fetchGameState();
      break;

    case 'PathBlocked':
      addEvent(`Path BLOCKED: ${event.pathId}`, 'danger');
      break;

    case 'PathOpened':
      addEvent(`Path opened (Gandalf): ${event.pathId}`, 'success');
      break;

    case 'PathCorrupted':
      addEvent(`Path CORRUPTED by Saruman: ${event.pathId}`, 'danger');
      showToast(`Path corrupted: ${event.pathId}`, 'danger');
      break;

    case 'IsengardDestroyed':
      addEvent('ISENGARD FALLS — Saruman disabled!', 'gold');
      showToast('Isengard has fallen! Saruman disabled.', 'success');
      break;

    case 'GameOver':
    case 'game_over':
      handleGameOver(event);
      fetchGameState();
      break;

    case 'UnitRespawned':
      addEvent(`${event.unitId} has respawned`, 'info');
      fetchGameState();
      break;

    case 'RouteCompromised':
      addEvent(`Route compromised for ${event.unitId}!`, 'warning');
      break;

    case 'RouteBlocked':
      addEvent(`Ring Bearer BLOCKED — path is closed!`, 'danger');
      showToast('Ring Bearer route is blocked!', 'danger');
      break;

    case 'RouteAssigned':
      addEvent(`Route assigned for ${event.payload || ''}`, 'info');
      break;

    case 'RouteComplete':
      addEvent(`Route complete for ${event.payload || ''}`, 'gold');
      fetchGameState();
      break;

    case 'DestroyRingAttempt':
      addEvent('⚔️ Ring Bearer at Mount Doom — attempting to destroy the Ring!', 'gold');
      showToast('Ring Bearer at Mount Doom! Destroying the Ring...', 'success');
      fetchGameState();
      break;

    default:
      if (event.turn) {
        fetchGameState();
      }
  }
}

function handleGameOver(event) {
  const winner = event.winner || event.Winner;
  const cause = event.cause || event.Cause;
  const turn = event.turn || event.Turn;

  const title = document.getElementById('win-title');
  const subtitle = document.getElementById('win-subtitle');

  if (winner === 'DRAW') {
    title.textContent = 'DRAW';
    title.style.color = 'var(--accent-neutral)';
    subtitle.textContent = `${gameState.maxTurns} turns passed with no winner.`;
  } else if (winner === 'FREE_PEOPLES') {
    title.textContent = IS_LIGHT ? '🎉 VICTORY!' : '💀 DEFEATED';
    title.style.color = IS_LIGHT ? 'var(--success)' : 'var(--danger)';
    subtitle.textContent = IS_LIGHT
      ? `The Ring is destroyed at Mount Doom! The Free Peoples triumph! (Turn ${turn})`
      : `The Ring was destroyed at Mount Doom. Sauron is defeated. (Turn ${turn})`;
  } else {
    title.textContent = IS_LIGHT ? '💀 DEFEATED' : '🎉 VICTORY!';
    title.style.color = IS_LIGHT ? 'var(--danger)' : 'var(--success)';
    const causeText = cause && cause !== 'from-state' ? cause : 'RING_BEARER_CAUGHT';
    subtitle.textContent = `The Ring Bearer has been caught! ${causeText} (Turn ${turn})`;
  }

  addEvent(`GAME OVER — ${winner} wins! Cause: ${cause || '—'}`, 'gold');
  renderWinEventLog();
  document.getElementById('win-overlay').classList.add('show');
  scrollEventLogToBottom();
}

async function playAgain() {
  await resetGame({ skipConfirm: true, message: 'New game — Play Again' });
}

// ─── ANALYSIS ─────────────────────────────────────────────────────────────────
async function fetchRouteAnalysis() {
  try {
    const res = await fetch(`${API_BASE}/analysis/routes?playerId=${PLAYER_ID}`);
    if (!res.ok) return;
    const data = await res.json();
    renderRouteAnalysis(data);
  } catch (e) {}
}

function renderRouteAnalysis(data) {
  const container = document.getElementById('route-list');
  if (!data || !data.routes || !data.routes.length) {
    container.innerHTML = '<div class="empty-st">No route data available</div>';
    return;
  }
  container.innerHTML = data.routes.map(route => {
    const rec = route.name === data.recommended;
    const blk = route.blockedPaths && route.blockedPaths.length > 0;
    const sClass = route.riskScore < 10 ? 'score-low' : route.riskScore > 25 ? 'score-high' : '';
    return `
      <div class="route-card ${rec ? 'rec' : ''} ${blk ? 'blk' : ''}">
        <div class="rc-name">${rec ? '✅ ' : ''}${route.name}</div>
        <div class="rc-row">
          <span>Risk Score</span>
          <span class="${sClass}" style="font-weight:600;">${route.riskScore}</span>
        </div>
        ${blk ? `<div style="font-size:.62rem;color:#d06050;margin-top:3px;">⛔ ${route.blockedPaths.join(', ')}</div>` : ''}
        ${route.warnings?.length ? `<div style="font-size:.62rem;color:#f0b040;margin-top:2px;">⚠ ${route.warnings.join('; ')}</div>` : ''}
      </div>
    `;
  }).join('');
}

async function fetchInterceptAnalysis() {
  try {
    const res = await fetch(`${API_BASE}/analysis/intercept?playerId=${PLAYER_ID}`);
    if (!res.ok) return;
    const data = await res.json();
    renderInterceptAnalysis(data);
  } catch (e) {}
}

function renderInterceptAnalysis(data) {
  const container = document.getElementById('intercept-list');
  if (!data || !data.byUnit || !data.byUnit.length) {
    container.innerHTML = '<div class="empty-st">No interception data</div>';
    return;
  }
  container.innerHTML = data.byUnit.map(plan => {
    const pct = Math.round((plan.score || 0) * 100);
    return `
      <div class="icept-card">
        <div class="ic-name">🗡 ${plan.unitId}</div>
        <div class="ic-target">→ ${REGION_NAMES[plan.targetRegion] || plan.targetRegion || '?'}</div>
        <div class="ic-bar-bg"><div class="ic-bar-fill" style="width:${pct}%"></div></div>
        <div style="font-size:.6rem;color:#d06050;margin-top:2px;">${pct}% probability</div>
      </div>
    `;
  }).join('');
}

// ─── UTILITIES ────────────────────────────────────────────────────────────────
function formatEventEntry(time, message, type) {
  const el = document.createElement('div');
  el.className = `ev ${type}`;
  el.innerHTML = `<span class="ev-t">${time}</span>${message}`;
  return el;
}

function renderWinEventLog() {
  const container = document.getElementById('win-event-log');
  const countEl = document.getElementById('win-event-count');
  if (!container) return;
  container.innerHTML = '';
  const history = gameState.eventHistory || [];
  if (history.length === 0) {
    container.innerHTML = '<div class="empty-st">No events recorded</div>';
    countEl.textContent = '0 events';
    return;
  }
  history.forEach(({ time, message, type }) => {
    container.appendChild(formatEventEntry(time, message, type));
  });
  countEl.textContent = `${history.length} event${history.length === 1 ? '' : 's'}`;
  container.scrollTop = container.scrollHeight;
}

function scrollEventLogToBottom() {
  const log = document.getElementById('event-log');
  if (log) log.scrollTop = log.scrollHeight;
}

function addEvent(message, type = 'info') {
  const time = new Date().toLocaleTimeString('tr-TR', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  gameState.eventHistory.push({ time, message, type });

  const log = document.getElementById('event-log');
  if (log) {
    log.prepend(formatEventEntry(time, message, type));
    const cnt = document.getElementById('event-count');
    if (cnt) cnt.textContent = gameState.eventHistory.length;
  }

  // Keep win overlay log in sync if game already ended
  if (document.getElementById('win-overlay')?.classList.contains('show')) {
    renderWinEventLog();
  }
}

function showToast(message, type = 'info') {
  const container = document.getElementById('toast-container');
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.textContent = message;
  container.appendChild(toast);
  setTimeout(() => toast.remove(), 4000);
}

// Auto-refresh analysis every 30 seconds
setInterval(() => {
  if (IS_LIGHT) fetchRouteAnalysis();
  else fetchInterceptAnalysis();
}, 30000);

// Poll game state every 5 seconds as fallback
setInterval(fetchGameState, 5000);

// Show analysis panel
if (IS_LIGHT) {
  document.getElementById('route-analysis').style.display = '';
  fetchRouteAnalysis();
} else {
  document.getElementById('intercept-analysis').style.display = '';
  fetchInterceptAnalysis();
}

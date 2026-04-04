const esc = s => { const d = document.createElement('div'); d.textContent = s; return d.innerHTML };
const escA = s => s.replace(/'/g, "\\'");
function showToast(m, t = 'info') { const el = document.getElementById('toast'); el.textContent = m; el.className = 'toast ' + t; setTimeout(() => el.className = 'toast hidden', 3000) }

let currentAliases = {};

async function refreshAll() {
    refreshEndpoints();
    refreshModelsAndAliases();
}

async function refreshEndpoints() {
    try { 
        const r = await fetch('/admin/endpoints'); 
        const d = await r.json(); 
        renderEndpoints(d.endpoints || []);
    }
    catch (e) { showToast('Failed to load: ' + e.message, 'error') }
}

async function refreshLog() {
    try {
        const r = await fetch('/admin/log');
        const d = await r.json();
        const entries = d.entries || [];
        const container = document.getElementById('request-log');
        const countEl = document.getElementById('log-count');
        if (countEl) countEl.textContent = entries.length + ' entries';
        if (!entries.length) {
            container.innerHTML = '<div style="color:var(--muted); padding:8px;">No requests yet.</div>';
            return;
        }
        // Render newest first
        container.innerHTML = entries.slice().reverse().map(e => {
            const icon = e.status === 'success' ? '✅' : '❌';
            const color = e.status === 'success' ? '#10b981' : '#ef4444';
            const detail = e.status === 'success'
                ? `<span style="color:#10b981">${esc(e.endpoint)}</span> → <span style="color:#94a3b8">${esc(e.model)}</span>`
                : `<span style="color:#ef4444">${esc(e.endpoint)}</span> <span style="color:#64748b;font-size:0.78em">${esc(e.error || '').substring(0,80)}</span>`;
            return `<div style="padding:3px 0; border-bottom:1px solid rgba(255,255,255,0.05); display:flex; gap:6px; align-items:baseline;">
                <span style="flex-shrink:0">${icon}</span>
                <span style="color:#64748b; font-size:0.75em; flex-shrink:0; min-width:58px">${esc(e.time.substring(11))}</span>
                <span>${detail}</span>
            </div>`;
        }).join('');
    } catch (e) { /* silent */ }
}

async function refreshModelsAndAliases() {
    try {
        const [mRes, aRes] = await Promise.all([ fetch('/admin/models'), fetch('/admin/aliases') ]);
        const mData = await mRes.json();
        const aData = await aRes.json();
        
        currentAliases = aData.aliases || {};
        const modelsDict = mData.models || {};
        
        renderPools(currentAliases, modelsDict);
        renderCheckboxes(modelsDict);
    } catch (e) { showToast('Models load failed: ' + e.message, 'error') }
}

async function forceRefreshModels() {
    try { 
        showToast('Scanning endpoints for models...', 'info');
        await fetch('/admin/models/refresh', { method: 'POST' }); 
        await refreshModelsAndAliases();
        showToast('Scan complete', 'success');
    } catch (e) { showToast('Scan error: ' + e.message, 'error') }
}

function renderEndpoints(eps) {
    document.getElementById('stat-total').textContent = eps.length;
    document.getElementById('stat-healthy').textContent = eps.filter(e => e.enabled && e.healthy).length;
    document.getElementById('stat-unhealthy').textContent = eps.filter(e => e.enabled && !e.healthy).length;
    document.getElementById('stat-disabled').textContent = eps.filter(e => !e.enabled).length;
    document.getElementById('stat-requests').textContent = eps.reduce((s, e) => s + e.total_requests, 0);
    const tb = document.getElementById('endpoints-body');
    if (!eps.length) { tb.innerHTML = '<tr><td colspan="7" class="empty">No endpoints configured</td></tr>'; return }
    tb.innerHTML = eps.map(e => {
        const errCell = e.last_error
            ? `<div style="display:flex;align-items:center;gap:4px">
                 <div>
                   <span class="err-text" title="${esc(e.last_error)}">${esc(e.last_error.substring(0, 40))}${e.last_error.length > 40 ? '…' : ''}</span><br>
                   <span style="color:#64748b;font-size:.72rem">${e.last_error_time || ''}</span>
                 </div>
                 <button class="btn btn-ghost btn-sm" style="padding:2px 6px;margin-bottom:auto" onclick="clearError('${escA(e.name)}')" title="Clear Error">✕</button>
               </div>`
            : '<span style="color:#64748b">—</span>';

        return `<tr>
    <td><b>${esc(e.name)}</b></td>
    <td style="font-family:monospace;font-size:.82rem;color:#94a3b8">${esc(e.api_base)}</td>
    <td><span class="pill ${e.enabled ? 'pill-green' : 'pill-yellow'}"><span class="dot ${e.enabled ? 'dot-g' : 'dot-y'}"></span>${e.enabled ? 'On' : 'Off'}</span></td>
    <td><span class="pill ${e.healthy ? 'pill-green' : 'pill-red'}"><span class="dot ${e.healthy ? 'dot-g' : 'dot-r'}"></span>${e.healthy ? 'OK' : 'Down'}</span></td>
    <td>${e.total_requests} <span style="color:#64748b;font-size:.78rem">(✓${e.successful} ✗${e.failed})</span></td>
    <td>${errCell}</td>
    <td><div class="actions">
      <button class="btn btn-ghost btn-sm" onclick="toggle('${escA(e.name)}',${!e.enabled})">${e.enabled ? 'Disable' : 'Enable'}</button>
      <button class="btn btn-danger btn-sm" onclick="removeEp('${escA(e.name)}')">Remove</button>
    </div></td></tr>`;
    }).join('');
}

function renderPools(aliases, modelsDict) {
    const tb = document.getElementById('pools-body');
    const keys = Object.keys(aliases);
    if (!keys.length) { tb.innerHTML = '<tr><td colspan="3" class="empty">No pools configured</td></tr>'; return }
    tb.innerHTML = keys.map(k => {
        const targets = aliases[k] || [];
        const modelsHtml = targets.map(t => {
            return `<span class="model-tag" title="Endpoint: ${escA(t.endpoint)}">${esc(t.model)} <span style="font-size:0.75em; opacity:0.7;">(${esc(t.endpoint)})</span></span>`;
        }).join(' ');
        return `<tr>
            <td style="font-weight:600;color:var(--text);white-space:nowrap">${esc(k)}</td>
            <td><div class="models-list">${modelsHtml}</div></td>
            <td style="white-space:nowrap;width:130px">
                <button class="btn btn-ghost btn-sm" onclick="editPool('${escA(k)}')">Edit</button>
                <button class="btn btn-danger btn-sm" onclick="deletePool('${escA(k)}')">Delete</button>
            </td>
        </tr>`;
    }).join('');
}

function syncModelChecks(val, isChecked) {
    const boxes = document.querySelectorAll('#pool-models-checkboxes input[type="checkbox"]');
    for (const b of boxes) {
        if (b.value === val) b.checked = isChecked;
    }
}

function editPool(name) {
    const poolTargets = currentAliases[name] || [];
    const poolValues = poolTargets.map(t => JSON.stringify({endpoint: t.endpoint, model: t.model}));
    document.getElementById('pool-name').value = name;
    const boxes = document.querySelectorAll('#pool-models-checkboxes input[type="checkbox"]');
    for (const b of boxes) {
        b.checked = poolValues.includes(b.value);
    }
    const form = document.getElementById('pool-form');
    if (form) form.scrollIntoView({ behavior: 'smooth', block: 'center' });
    document.getElementById('pool-name').focus();
}

function renderCheckboxes(modelsDict) {
    const box = document.getElementById('pool-models-checkboxes');
    const modelsCount = Object.keys(modelsDict).length;
    if (!modelsCount) { box.innerHTML = '<span class="empty" style="padding:10px!important;font-size:0.8rem">No backend models discovered. Click "Fetch Models" above.</span>'; return }
    
    // Group models by endpoint
    const epsMap = {};
    for (const [model, eps] of Object.entries(modelsDict)) {
        for (const ep of eps) {
            if (!epsMap[ep]) epsMap[ep] = [];
            epsMap[ep].push(model);
        }
    }
    
    const epNames = Object.keys(epsMap).sort();
    let html = '';
    let counter = 0;
    
    for (const ep of epNames) {
        epsMap[ep].sort();
        html += `<div class="ep-model-group">
            <div class="ep-group-title">${esc(ep)} <span style="font-weight:normal;color:#64748b;font-size:0.75rem">(${epsMap[ep].length} models)</span></div>
            <div class="models-grid">`;
        
        for (const m of epsMap[ep]) {
            const valObj = JSON.stringify({endpoint: ep, model: m});
            html += `
                <label class="model-checkbox">
                    <input type="checkbox" value='${escA(valObj)}' id="chk_${counter++}" onchange="syncModelChecks(this.value, this.checked)">
                    <span style="overflow:hidden;text-overflow:ellipsis" title="${escA(m)}">${esc(m)}</span>
                </label>`;
        }
        html += `</div></div>`;
    }
    
    box.innerHTML = html;
}

async function addEndpoint(ev) {
    ev.preventDefault();
    const name = document.getElementById('ep-name').value.trim();
    const api_base = document.getElementById('ep-base').value.trim();
    const api_key = document.getElementById('ep-key').value.trim();
    try {
        await fetch('/admin/endpoints', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, api_base, api_key, enabled: true }) });
        showToast('Added ' + name, 'success'); document.getElementById('add-form').reset(); refreshAll()
    }
    catch (e) { showToast('Error: ' + e.message, 'error') }
}

async function clearError(name) {
    try { 
        await fetch('/admin/endpoints/' + encodeURIComponent(name) + '/clear_error', { method: 'POST' }); 
        showToast('Error cleared', 'success'); 
        refreshEndpoints(); 
    } catch (e) { showToast('Error: ' + e.message, 'error') }
}

async function removeEp(name) {
    if (!confirm('Remove endpoint "' + name + '"?')) return;
    try { await fetch('/admin/endpoints/' + encodeURIComponent(name), { method: 'DELETE' }); showToast('Removed', 'success'); refreshAll() }
    catch (e) { showToast('Error: ' + e.message, 'error') }
}

async function toggle(name, en) {
    try {
        await fetch('/admin/endpoints/' + encodeURIComponent(name), { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: en }) });
        showToast((en ? 'Enabled' : 'Disabled') + ' ' + name, 'success'); refreshEndpoints()
    }
    catch (e) { showToast('Error: ' + e.message, 'error') }
}

async function savePool(ev) {
    ev.preventDefault();
    const name = document.getElementById('pool-name').value.trim();
    const checkboxes = document.querySelectorAll('#pool-models-checkboxes input:checked');
    const selected = [...new Set(Array.from(checkboxes).map(c => c.value))].map(v => JSON.parse(v));
    
    if (!selected.length) { showToast('Please select at least one backend model', 'error'); return }
    
    currentAliases[name] = selected;
    
    try {
        await fetch('/admin/aliases', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ aliases: currentAliases }) });
        showToast('Pool saved', 'success'); 
        document.getElementById('pool-form').reset();
        refreshModelsAndAliases();
    } catch (e) { showToast('Error: ' + e.message, 'error') }
}

async function deletePool(name) {
    if (!confirm('Delete pool "' + name + '"?')) return;
    delete currentAliases[name];
    try {
        await fetch('/admin/aliases', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ aliases: currentAliases }) });
        showToast('Pool deleted', 'success'); 
        refreshModelsAndAliases();
    } catch (e) { showToast('Error: ' + e.message, 'error') }
}

async function saveConfig() {
    try { await fetch('/admin/save', { method: 'POST' }); showToast('Config saved to disk', 'success') }
    catch (e) { showToast('Error: ' + e.message, 'error') }
}

async function resetStats() {
    if (!confirm('Reset all usage stats?')) return;
    try { await fetch('/admin/stats/reset', { method: 'POST' }); showToast('Stats reset', 'success'); refreshEndpoints() }
    catch (e) { showToast('Error: ' + e.message, 'error') }
}

document.addEventListener('DOMContentLoaded', () => {
    const u = document.getElementById('connection-url');
    if (u) u.textContent = 'http://' + location.hostname + ':' + (location.port || '5066');
    refreshAll(); refreshLog();
    setInterval(refreshEndpoints, 10000);
    setInterval(refreshLog, 5000);
});

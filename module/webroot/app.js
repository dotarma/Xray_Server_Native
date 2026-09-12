(() => {
  const $ = (selector) => document.querySelector(selector);
  const modeNames = { mode1: 'Mode 1 - Quick Tunnel', mode2: 'Mode 2 - Domain rieng', mode3: 'Mode 3 - 3x-ui Tunnel', quick: 'Mode 1 - Quick Tunnel', none: 'Chua cau hinh' };
  let moduleDir = '', password = '', toastTimer;

  function toast(message, error = false) {
    const node = $('#toast'); node.textContent = message; node.classList.toggle('error', error); node.classList.add('show');
    clearTimeout(toastTimer); toastTimer = setTimeout(() => node.classList.remove('show'), 4600); window.ksu?.toast?.(message);
  }
  function exec(command) {
    return new Promise((resolve, reject) => {
      if (!window.ksu?.exec) return reject(new Error('KernelSU WebUI API is unavailable.'));
      const callback = `ams_${Date.now()}_${Math.random()}`;
      window[callback] = (errno, stdout, stderr) => { delete window[callback]; errno ? reject(new Error(stderr || 'Root command failed.')) : resolve(stdout || ''); };
      try { window.ksu.exec(command, '{}', callback); } catch (error) { delete window[callback]; reject(error); }
    });
  }
  function encode(value) {
    const bytes = new TextEncoder().encode(typeof value === 'string' ? value : JSON.stringify(value)); let text = '';
    bytes.forEach((byte) => { text += String.fromCharCode(byte); }); return btoa(text);
  }
  async function bridge(action, payload = '') {
    const output = await exec(`"${moduleDir}/scripts/webui-bridge.sh" ${action} ${encode(payload)}`);
    let data; try { data = JSON.parse(output); } catch { throw new Error(output || 'Invalid manager response.'); }
    if (data.error) throw new Error(data.error); return data;
  }
  function copy(value, message) {
    navigator.clipboard?.writeText(value).catch(() => { const area = document.createElement('textarea'); area.value = value; document.body.append(area); area.select(); document.execCommand('copy'); area.remove(); });
    toast(message);
  }
  function state(selector, service) {
    const node = $(selector); node.textContent = service.running ? `Dang chay - PID ${service.pid}` : 'Da dung'; node.style.color = service.running ? 'var(--green)' : 'var(--yellow)';
  }
  function make(tag, text, className) {
    const node = document.createElement(tag); if (text !== undefined) node.textContent = text; if (className) node.className = className; return node;
  }
  function renderModeResult(mode, items) {
    const root = $(`#${mode}-result`); root.replaceChildren(); root.hidden = !items?.length;
    for (const item of items || []) {
      const card = make('section', undefined, 'mode-result-card'); const heading = make('div', undefined, 'mode-result-heading');
      heading.append(make('strong', item.label || modeNames[item.mode] || 'Server'));
      heading.append(make('span', [item.host, item.transport?.toUpperCase(), item.originPort ? `Origin ${item.originPort}` : 'Origin 8888'].filter(Boolean).join(' - '))); card.append(heading);
      if (item.panelUrl) {
        const row = make('div', undefined, 'result-row'); row.append(make('span', 'Panel public'));
        const link = make('a', item.panelUrl); link.href = item.panelUrl; link.target = '_blank'; link.rel = 'noreferrer'; row.append(link); card.append(row);
      }
      if (item.subscriptionUrl) {
        const row = make('div', undefined, 'result-copy-row'); row.append(make('span', 'Link subscription'));
        const button = make('button', 'Copy sub'); button.type = 'button'; button.addEventListener('click', () => copy(item.subscriptionUrl, 'Da sao chep link subscription.')); row.append(button); card.append(row);
        const area = make('textarea', undefined, 'result-link'); area.rows = 2; area.readOnly = true; area.value = item.subscriptionUrl; card.append(area);
      }
      const links = item.links?.length ? item.links : item.link ? [item.link] : [];
      if (links.length) {
        const row = make('div', undefined, 'result-copy-row'); row.append(make('span', 'Link VLESS'));
        const button = make('button', 'Sao chep VLESS'); button.type = 'button'; button.addEventListener('click', () => copy(links.join('\n'), 'Da sao chep link VLESS.')); row.append(button); card.append(row);
        const area = make('textarea', undefined, 'result-link'); area.rows = Math.max(4, links.length * 3); area.readOnly = true; area.value = links.join('\n'); card.append(area);
      }
      root.append(card);
    }
  }
  function renderResults(registry) {
    renderModeResult('mode1', registry?.mode1 ? [registry.mode1] : []); renderModeResult('mode2', registry?.mode2 ? [registry.mode2] : []); renderModeResult('mode3', registry?.mode3 || []);
  }
  function updateTransportFields() {
    document.querySelectorAll('#mode2-form, #mode3-form').forEach((form) => {
      const xhttp = ['xhttp', 'dual'].includes(form.querySelector('[name="transport"]')?.value);
      form.querySelectorAll('.xhttp-field').forEach((field) => { field.hidden = !xhttp; field.querySelector('select').disabled = !xhttp; });
    });
  }
  function applySavedConfig(saved) {
    const set = (form, name, value) => { const node = document.querySelector(`${form} [name="${name}"]`); if (node && value !== undefined && value !== null) node.value = value; };
    for (const mode of ['mode2', 'mode3']) {
      if (!saved?.[mode]) continue;
      Object.entries(saved[mode]).forEach(([key, value]) => key !== 'tokenSaved' && set(`#${mode}-form`, key, value));
      $(`#${mode}-form textarea[name="token"]`).placeholder = saved[mode].tokenSaved ? 'Da luu an toan. De trong de dung lai.' : 'Dan connector token';
    }
    updateTransportFields();
  }
  async function logs() { const data = await bridge('logs', $('#log-target').value); $('#logs').textContent = data.logs || 'Chua co nhat ky.'; }
  async function refresh(withLogs = true) {
    const data = await bridge('status'); state('#panel-status', data.panel); state('#panel-tunnel-status', data.panelTunnel);
    const nativeMode = data.activeMode === 'mode1' || data.activeMode === 'mode2' ? data.activeMode : 'none'; const running = data.quickServer.running || data.tunnel.running;
    $('#mode-status').textContent = nativeMode === 'none' || !running ? 'Chua chay Mode 1/2' : `${modeNames[nativeMode]} - dang chay`; $('#mode-status').style.color = running ? 'var(--green)' : 'var(--yellow)';
    $('#panel-user').textContent = data.panelUsername || '-'; $('#panel-password').textContent = data.panelPassword || '-'; password = data.panelPassword || '';
    $('#panel-link').href = data.panelUrl || 'http://127.0.0.1:2053/'; $('#panel-card-link').href = data.panelUrl || 'http://127.0.0.1:2053/'; $('#panel-tunnel-url').textContent = data.panelTunnelUrl || 'Tunnel 3x-ui chua cau hinh'; renderResults(data.deployments);
    const badge = $('#mode-badge'); badge.textContent = modeNames[data.deployment?.mode || nativeMode] || 'CHUA CAU HINH'; badge.className = `badge ${data.deployment?.mode || nativeMode || 'none'}`;
    applySavedConfig(data.saved); if (withLogs) await logs();
  }
  function bind(formID, action, loading, done) {
    $(formID).addEventListener('submit', async (event) => {
      event.preventDefault(); const form = event.currentTarget, button = event.submitter, label = button.textContent; button.disabled = true; button.textContent = loading;
      try { await bridge(action, Object.fromEntries(new FormData(form).entries())); form.querySelectorAll('textarea[name="token"]').forEach((node) => { node.value = ''; }); toast(done); await refresh(); }
      catch (error) { toast(error.message, true); try { await refresh(false); } catch {} } finally { button.disabled = false; button.textContent = label; }
    });
  }
  document.querySelectorAll('.tab').forEach((button) => button.addEventListener('click', () => {
    document.querySelectorAll('.tab').forEach((item) => item.classList.toggle('active', item === button)); document.querySelectorAll('.tab-pane').forEach((item) => item.classList.toggle('active', item.dataset.pane === button.dataset.tab));
  }));
  document.querySelectorAll('[data-service]').forEach((button) => button.addEventListener('click', async () => {
    const label = button.textContent; button.disabled = true; button.textContent = 'Dang xu ly';
    try { await bridge('service', { service: button.dataset.service, action: button.dataset.action }); toast('Lenh hoan tat.'); await refresh(); }
    catch (error) { toast(error.message, true); } finally { button.disabled = false; button.textContent = label; }
  }));
  document.querySelectorAll('#mode2-form [name="transport"], #mode3-form [name="transport"]').forEach((node) => node.addEventListener('change', updateTransportFields));
  bind('#mode1-form', 'quick', 'Dang tao tunnel', 'Mode 1 da san sang.'); bind('#mode2-form', 'mode2', 'Dang cau hinh', 'Mode 2 da san sang.'); bind('#mode3-form', 'mode3', 'Dang tao inbound', 'Mode 3 da san sang.');
  $('#copy-password').addEventListener('click', () => copy(password, 'Da sao chep password 3x-ui.')); $('#refresh-button').addEventListener('click', () => refresh().catch((error) => toast(error.message, true))); $('#log-target').addEventListener('change', () => logs().catch((error) => toast(error.message, true)));
  try { const info = JSON.parse(window.ksu.moduleInfo()); moduleDir = info.moduleDir; if (!moduleDir) throw new Error('KernelSU did not provide module directory.'); refresh().catch((error) => { $('#logs').textContent = error.message; toast(error.message, true); }); }
  catch (error) { $('#logs').textContent = error.message; toast(error.message, true); }
})();

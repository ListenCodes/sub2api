const apiBase = '/api/v1/admin/extensions-self/account-monitor';
const state = {
  page: 1,
  page_size: 20,
  total: 0,
  sort_by: 'attempts',
  sort_order: 'desc',
  accountId: null,
  tab: 'models',
  autoRefresh: true,
  filters: {
    range: '7d', platform: '', account_id: '', parent_account_id: '', account_status: '',
    model: '', user_id: '', api_key_id: '', request_type: '', result: '',
    error_category: '', status_code: '', rollup: 'physical',
  },
};

const $ = id => document.getElementById(id);
const number = value => new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 2 }).format(Number(value || 0));
const money = value => `$${number(value)}`;
const percent = (ok, total) => total ? `${(Number(ok) * 100 / Number(total)).toFixed(1)}%` : '—';

function escapeHTML(value) {
  const div = document.createElement('div');
  div.textContent = String(value ?? '');
  return div.innerHTML;
}

function dateTime(value) {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1971) return '—';
  return date.toLocaleString([], { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function selectedRange() {
  const to = new Date();
  const from = new Date(to);
  if (state.filters.range === 'today') {
    from.setHours(0, 0, 0, 0);
  } else if (state.filters.range === '24h') {
    from.setTime(to.getTime() - 24 * 60 * 60 * 1000);
  } else if (state.filters.range === 'custom') {
    const customFrom = new Date($('custom-from').value);
    const customTo = new Date($('custom-to').value);
    if (!Number.isNaN(customFrom.getTime()) && !Number.isNaN(customTo.getTime())) {
      return { from: customFrom.toISOString(), to: customTo.toISOString() };
    }
    from.setUTCDate(from.getUTCDate() - 7);
  } else {
    const days = { '7d': 7, '30d': 30, '90d': 90 }[state.filters.range] || 7;
    from.setUTCDate(from.getUTCDate() - days);
  }
  return { from: from.toISOString(), to: to.toISOString() };
}

function query(extra = {}) {
  const params = new URLSearchParams({
    ...selectedRange(), page: String(state.page), page_size: String(state.page_size),
    sort_by: state.sort_by, sort_order: state.sort_order, ...state.filters, ...extra,
  });
  for (const [key, value] of [...params]) {
    if (!value || key === 'range') params.delete(key);
  }
  return params;
}

async function request(path, options) {
  const response = await fetch(`${apiBase}${path}`, {
    credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, ...options,
  });
  const data = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

function metric(label, value, detail = '') {
  return `<div class="metric"><span>${escapeHTML(label)}</span><strong>${escapeHTML(value)}</strong><small>${escapeHTML(detail)}</small></div>`;
}

async function loadOverview() {
  const data = await request(`/overview?${query()}`);
  $('account-overview').innerHTML = [
    metric('账号尝试', number(data.attempts), `${number(data.successes)} 成功 / ${number(data.failures)} 失败`),
    metric('账号成功率', percent(data.successes, data.attempts), '上游账号尝试口径'),
    metric('用户请求', number(data.requests), `${percent(data.request_successes, data.requests)} 最终成功`),
    metric('活跃 / 异常', `${number(data.active_accounts)} / ${number(data.abnormal_accounts)}`, `${number(data.users)} 位调用用户`),
    metric('Token', number(data.tokens), `用户计费 ${money(data.user_cost)}`),
    metric('账号成本', money(data.account_cost), '上游账号成本'),
    metric('平均延迟', data.average_duration_ms ? `${number(data.average_duration_ms)} ms` : '—', '成功与失败尝试'),
    metric('P95 延迟', data.p95_duration_ms ? `${number(data.p95_duration_ms)} ms` : '—', '第 95 百分位'),
  ].join('');
  $('sync-status').textContent = data.last_sync_at
    ? `最近同步 ${new Date(data.last_sync_at).toLocaleString()} · 延迟 ${number(data.sync_lag_seconds)} 秒`
    : '同步状态暂不可用';
}

function healthLabel(level) {
  return ({ normal: '正常', attention: '关注', abnormal: '异常', critical: '严重' })[level] || '正常';
}

function statusLabel(status) {
  return ({ active: '正常', inactive: '停用', error: '错误' })[status] || status || '未知';
}

function renderAccountRow(row) {
  const reasons = row.health?.reasons || []; // health.reasons is the explainable anomaly contract.
  const healthReason = reasons.length ? reasons.join(' ') : '当前未触发异常规则';
  return `<tr data-account-id="${Number(row.account_id)}">
    <td><strong>${escapeHTML(row.account_name || `账号 ${row.account_id}`)}</strong><br><small>ID ${number(row.account_id)}${row.parent_account_id ? ` · 母账号 ${number(row.parent_account_id)}` : ''}</small></td>
    <td>${escapeHTML(row.platform || '—')}<br><small>${escapeHTML(statusLabel(row.status))}</small></td>
    <td>${number(row.attempts)}</td><td>${percent(row.successes, row.attempts)}</td><td>${number(row.failures)}</td>
    <td>${number(row.model_count)}</td><td>${number(row.user_count)}</td><td>${number(row.tokens)}</td>
    <td>${money(row.user_cost)}</td><td>${money(row.account_cost)}</td>
    <td>${row.average_duration_ms ? `${number(row.average_duration_ms)} ms` : '—'}</td>
    <td>${row.p95_duration_ms ? `${number(row.p95_duration_ms)} ms` : '—'}</td>
    <td>${dateTime(row.last_success_at)}</td><td>${dateTime(row.last_failure_at)}</td>
    <td class="health-cell"><span class="health-line"><i class="health-dot ${escapeHTML(row.health?.level || 'normal')}"></i>${healthLabel(row.health?.level)}</span><small title="${escapeHTML(healthReason)}">${escapeHTML(healthReason)}</small></td>
  </tr>`;
}

async function loadAccounts() {
  const data = await request(`/accounts?${query()}`);
  state.total = Number(data.total || 0);
  const pages = Math.max(1, Math.ceil(state.total / state.page_size));
  $('account-count').textContent = `${number(state.total)} 个账号`;
  $('page-label').textContent = `第 ${state.page} / ${pages} 页`;
  $('prev-page').disabled = state.page <= 1;
  $('next-page').disabled = state.page >= pages;
  const rows = data.items || [];
  $('accounts-body').innerHTML = rows.length
    ? rows.map(renderAccountRow).join('')
    : '<tr><td colspan="15" class="empty">当前范围没有符合条件的账号调用</td></tr>';
  document.querySelectorAll('[data-account-id]').forEach(row => row.addEventListener('click', () => {
    openDrawer(Number(row.dataset.accountId), row.querySelector('strong').textContent);
  }));
  document.querySelectorAll('[data-sort]').forEach(button => {
    const active = button.dataset.sort === state.sort_by;
    button.classList.toggle('active', active);
    button.dataset.direction = active ? state.sort_order : '';
  });
}

async function loadQuality() {
  const data = await request(`/data-quality?${query()}`);
  const rows = [
    ['主库连接', data.source_connected ? '正常' : '不可用'],
    ['错误归属率', data.error_attribution_rate == null ? '暂不可用' : `${(data.error_attribution_rate * 100).toFixed(1)}%`],
    ['未归属账号', number(data.unattributed_errors)], ['恢复型失败', number(data.recovered_failures)],
    ['精确 / 估算模型', `${number(data.exact_models)} / ${number(data.estimated_models)}`],
    ['缺失请求标识', number(data.fallback_identities)], ['数据来源', data.data_source || '暂不可用'],
  ];
  $('quality-content').innerHTML = `<div class="quality-list">${rows.map(([key, value]) => `<div class="quality-row"><span>${escapeHTML(key)}</span><strong>${escapeHTML(value)}</strong></div>`).join('')}</div>`;
}

async function refresh(silent = false) {
  try {
    await Promise.all([loadOverview(), loadAccounts(), loadQuality()]);
    if (state.accountId) await loadDrawer();
  } catch (error) {
    if (!silent) toast(error.message, true);
  }
}

async function openDrawer(id, name) {
  state.accountId = id;
  $('drawer-title').textContent = name;
  $('drawer-subtitle').textContent = `账号 ID ${id}`;
  $('account-drawer').classList.add('open');
  $('account-drawer').setAttribute('aria-hidden', 'false');
  $('drawer-backdrop').hidden = false;
  await loadDrawer();
}

function closeDrawer() {
  state.accountId = null;
  $('account-drawer').classList.remove('open');
  $('account-drawer').setAttribute('aria-hidden', 'true');
  $('drawer-backdrop').hidden = true;
}

function detailTable(columns, items) {
  if (!items.length) return '<p class="empty">当前范围没有明细</p>';
  return `<div class="table-scroll detail-scroll"><table><thead><tr>${columns.map(column => `<th>${escapeHTML(column.label)}</th>`).join('')}</tr></thead><tbody>${items.map(item => `<tr>${columns.map(column => `<td>${column.render ? column.render(item[column.key], item) : escapeHTML(item[column.key] ?? '—')}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`;
}

function renderModels(items) {
  return detailTable([
    { key: 'actual_model', label: '实际上游模型' }, { key: 'model_attribution', label: '归属' },
    { key: 'attempts', label: '调用', render: number }, { key: 'successes', label: '成功', render: number },
    { key: 'failures', label: '失败', render: number }, { key: 'attempts', label: '成功率', render: (_, item) => percent(item.successes, item.attempts) },
    { key: 'tokens', label: 'Token', render: number }, { key: 'user_cost', label: '用户计费', render: money },
    { key: 'account_cost', label: '账号成本', render: money }, { key: 'average_duration_ms', label: '平均延迟', render: value => `${number(value)} ms` },
    { key: 'p95_duration_ms', label: 'P95', render: value => `${number(value)} ms` },
  ], items);
}

function renderUsers(items) {
  if (!items.length) return '<p class="empty">当前范围没有用户调用</p>';
  const users = new Map();
  for (const item of items) {
    const id = Number(item.user_id || 0);
    if (!users.has(id)) users.set(id, { id, email: item.email, username: item.username, attempts: 0, successes: 0, failures: 0, tokens: 0, cost: 0, keys: [] });
    const user = users.get(id);
    user.attempts += Number(item.attempts || 0); user.successes += Number(item.successes || 0);
    user.failures += Number(item.failures || 0); user.tokens += Number(item.tokens || 0); user.cost += Number(item.user_cost || 0);
    user.keys.push(item);
  }
  return `<div class="user-list">${[...users.values()].map(user => `<article class="user-row">
    <div><strong>${escapeHTML(user.email || `用户 ${user.id}`)}</strong><small>${escapeHTML(user.username || '')} · ID ${number(user.id)}</small></div>
    <div class="user-metrics"><span>${number(user.attempts)} 调用</span><span>${percent(user.successes, user.attempts)}</span><span>${number(user.tokens)} Token</span><span>${money(user.cost)}</span></div>
    <details class="expand-api-keys"><summary>API Key ${number(user.keys.length)} 个</summary>
      <div class="key-list">${user.keys.map(key => `<div><strong>${escapeHTML(key.api_key_name || `API Key ${key.api_key_id}`)}</strong><span>ID ${number(key.api_key_id)} · ${escapeHTML(key.masked_prefix || '无脱敏前缀')}</span><span>${number(key.attempts)} 调用 · ${percent(key.successes, key.attempts)}</span></div>`).join('')}</div>
    </details>
  </article>`).join('')}</div>`;
}

function renderErrors(items) {
  return detailTable([
    { key: 'error_category', label: '失败原因' }, { key: 'upstream_status_code', label: '上游状态码' },
    { key: 'provider_error_code', label: '上游错误码' }, { key: 'failures', label: '失败', render: number },
    { key: 'recovered_failures', label: '恢复型失败', render: number }, { key: 'last_failure_at', label: '最近失败', render: dateTime },
  ], items);
}

function renderTrends(items) {
  if (!items.length) return '<p class="empty">当前范围没有趋势数据</p>';
  const max = Math.max(...items.map(item => Number(item.attempts || 0)), 1);
  return `<div class="trend-chart" role="img" aria-label="账号调用趋势">${items.map(item => {
    const successWidth = Number(item.successes || 0) * 100 / max;
    const failureWidth = Number(item.failures || 0) * 100 / max;
    return `<div class="trend-row"><time>${escapeHTML(dateTime(item.bucket))}</time><div class="trend-bars"><i class="success" style="width:${successWidth}%"></i><i class="failure" style="width:${failureWidth}%"></i></div><strong>${number(item.attempts)}</strong><small>${percent(item.successes, item.attempts)} · P95 ${number(item.p95_duration_ms)} ms</small></div>`;
  }).join('')}</div>`;
}

function renderAttempts(items) {
  return detailTable([
    { key: 'attempted_at', label: '时间', render: dateTime }, { key: 'actual_model', label: '实际上游模型' },
    { key: 'result', label: '结果' }, { key: 'recovered', label: '已恢复', render: value => value ? '是' : '否' },
    { key: 'error_category', label: '失败原因' }, { key: 'status_code', label: '状态码' },
    { key: 'user_id', label: '用户 ID', render: number }, { key: 'api_key_id', label: 'API Key ID', render: number },
    { key: 'tokens', label: 'Token', render: number }, { key: 'account_cost', label: '账号成本', render: money },
    { key: 'duration_ms', label: '延迟', render: value => `${number(value)} ms` },
  ], items);
}

function renderDetail(tab, data) {
  if (tab === 'media') {
    return `<div class="metric-strip detail-metrics">${metric('图片生成', number(data.image_count), '生成数量')}${metric('视频生成', number(data.video_count), `${number(data.video_duration_seconds)} 秒`)}</div>`;
  }
  if (tab === 'trends') return renderTrends(Array.isArray(data) ? data : []);
  const items = data.items || [];
  if (tab === 'models') return renderModels(items);
  if (tab === 'users') return renderUsers(items);
  if (tab === 'errors') return renderErrors(items);
  return renderAttempts(items);
}

async function loadDrawer() {
  if (!state.accountId) return;
  const paths = {
    models: `/accounts/${state.accountId}/models`, users: `/accounts/${state.accountId}/users`,
    errors: `/accounts/${state.accountId}/errors`, trends: `/accounts/${state.accountId}/trends`,
    attempts: '/attempts', media: `/accounts/${state.accountId}`,
  };
  try {
    const data = await request(`${paths[state.tab]}?${query({ account_id: String(state.accountId) })}`);
    $('drawer-content').innerHTML = renderDetail(state.tab, data);
  } catch (error) {
    $('drawer-content').innerHTML = `<p class="error-text">${escapeHTML(error.message)}</p>`;
  }
}

function toast(message, error = false) {
  const node = $('toast');
  node.textContent = message;
  node.classList.toggle('error', error);
  node.classList.add('show');
  setTimeout(() => node.classList.remove('show'), 2600);
}

document.querySelectorAll('#account-filters input:not(#custom-from):not(#custom-to),#account-filters select').forEach(control => {
  control.addEventListener('change', () => {
    const key = control.id.replaceAll('-', '_');
    state.filters[key] = control.value;
    state.page = 1;
    if (control.id === 'range') $('custom-range').hidden = control.value !== 'custom';
    if (control.id !== 'range' || control.value !== 'custom') refresh();
  });
});

$('apply-custom-range').addEventListener('click', () => { state.page = 1; refresh(); });
$('page-size').addEventListener('change', event => { state.page_size = Number(event.target.value); state.page = 1; loadAccounts(); });
$('prev-page').addEventListener('click', () => { if (state.page > 1) { state.page--; loadAccounts(); } });
$('next-page').addEventListener('click', () => { if (state.page * state.page_size < state.total) { state.page++; loadAccounts(); } });
$('refresh-button').addEventListener('click', () => refresh());
$('auto-refresh').addEventListener('change', event => { state.autoRefresh = event.target.checked; });
$('drawer-close').addEventListener('click', closeDrawer);
$('drawer-backdrop').addEventListener('click', closeDrawer);

document.querySelectorAll('[data-sort]').forEach(button => button.addEventListener('click', event => {
  const sort = event.currentTarget.dataset.sort;
  if (state.sort_by === sort) state.sort_order = state.sort_order === 'desc' ? 'asc' : 'desc';
  else { state.sort_by = sort; state.sort_order = 'desc'; }
  state.page = 1;
  loadAccounts();
}));

document.querySelectorAll('.tabs button').forEach(button => button.addEventListener('click', () => {
  document.querySelectorAll('.tabs button').forEach(node => node.classList.toggle('active', node === button));
  state.tab = button.dataset.tab;
  loadDrawer();
}));

$('thresholds-button').addEventListener('click', async () => {
  try {
    const data = await request('/thresholds');
    $('threshold-success-rate').value = Number(data.success_rate || 0.9) * 100;
    $('thresholds-dialog').showModal();
  } catch (error) { toast(error.message, true); }
});

$('save-thresholds').addEventListener('click', async event => {
  event.preventDefault();
  try {
    await request('/thresholds', { method: 'PUT', body: JSON.stringify({ scope: 'global', scope_id: 0, success_rate: Number($('threshold-success-rate').value) / 100 }) });
    $('thresholds-dialog').close(); toast('阈值已保存'); refresh(true);
  } catch (error) { toast(error.message, true); }
});

async function watchRebuildJob(id) {
  const job = await request(`/rebuild-jobs/${id}`);
  $('rebuild-status').textContent = `任务 ${id}：${job.status} · 已处理 ${number(job.processed_rows)} 行${job.error ? ` · ${job.error}` : ''}`;
  if (job.status === 'pending' || job.status === 'running') setTimeout(() => watchRebuildJob(id).catch(error => { $('rebuild-status').textContent = error.message; }), 1500);
}

$('rebuild-button').addEventListener('click', () => $('rebuild-dialog').showModal());
$('start-rebuild').addEventListener('click', async event => {
  event.preventDefault();
  try {
    const job = await request('/rebuild-jobs', { method: 'POST', body: JSON.stringify({ from: new Date($('rebuild-from').value).toISOString(), to: new Date($('rebuild-to').value).toISOString() }) });
    await watchRebuildJob(job.id);
  } catch (error) { $('rebuild-status').textContent = error.message; }
});

refresh();
setInterval(() => { if (state.autoRefresh) refresh(true); }, 60000);

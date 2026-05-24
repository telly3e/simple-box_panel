import React, { useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Activity, Database, Edit3, Eye, Play, Plus, RefreshCcw, Save, Server, Trash2, Users, X } from 'lucide-react';
import './styles.css';

const API = '/api';
const SS_METHODS = [
  '2022-blake3-aes-128-gcm',
  '2022-blake3-aes-256-gcm',
  '2022-blake3-chacha20-poly1305',
  'none',
  'aes-128-gcm',
  'aes-192-gcm',
  'aes-256-gcm',
  'chacha20-ietf-poly1305',
  'xchacha20-ietf-poly1305',
] as const;
const DEFAULT_ANYTLS_PADDING_SCHEME = [
  'stop=8',
  '0=30-30',
  '1=100-400',
  '2=400-500,c,500-1000,c,500-1000,c,500-1000,c,500-1000',
  '3=9-9,500-1000',
  '4=500-1000',
  '5=500-1000',
  '6=500-1000',
  '7=500-1000',
].join('\n');

type Summary = {
  user_count: number;
  enabled_users: number;
  exit_node_count: number;
  total_used_bytes: number;
  online_exit_nodes: number;
};

type User = {
  id: string;
  name: string;
  enabled: boolean;
  quota_bytes: number;
  used_bytes: number;
  anytls_password: string;
  ss_password: string;
  ss_2022_password_16: string;
  ss_2022_password_32: string;
  subscription_token: string;
};

type ExitNode = {
  id: string;
  name: string;
  hostname: string;
  enabled: boolean;
  anytls_enabled: boolean;
  ss_enabled: boolean;
  anytls_port: number;
  anytls_padding_scheme: string;
  ss_port: number;
  ss_method: string;
  ss_2022_server_password_16: string;
  ss_2022_server_password_32: string;
  relay_enabled: boolean;
  relay_host: string;
  relay_anytls_port: number;
  relay_ss_port: number;
  cert_mode: string;
  cert_domain: string;
  certificate_path: string;
  key_path: string;
  acme_email: string;
  cloudflare_api_token_env: string;
  agent_token: string;
  stats_mode: string;
  stats_api_listen: string;
  last_heartbeat_at?: string;
  applied_config_version: number;
  last_applied_at?: string;
  last_agent_error: string;
  last_agent_error_at?: string;
  expected_config_version: number;
};

type Tab = 'dashboard' | 'users' | 'nodes' | 'subscriptions';

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API}${path}`, {
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
    ...init,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || res.statusText);
  }
  return res.json() as Promise<T>;
}

function formatBytes(value: number) {
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = value;
  let idx = 0;
  while (size >= 1024 && idx < units.length - 1) {
    size /= 1024;
    idx += 1;
  }
  return `${size.toFixed(idx === 0 ? 0 : 1)} ${units[idx]}`;
}

function bytesToGB(value: number) {
  if (!value) return '0';
  return (value / 1024 / 1024 / 1024).toFixed(2).replace(/\.?0+$/, '');
}

function gbToBytes(value: string) {
  return Math.round(Number(value || 0) * 1024 * 1024 * 1024);
}

function App() {
  const [tab, setTab] = useState<Tab>('dashboard');
  const [summary, setSummary] = useState<Summary | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [exits, setExits] = useState<ExitNode[]>([]);
  const [error, setError] = useState('');

  async function loadAll() {
    setError('');
    try {
      const [nextSummary, nextUsers, nextExits] = await Promise.all([
        api<Summary>('/summary'),
        api<User[]>('/users'),
        api<ExitNode[]>('/exit-nodes'),
      ]);
      setSummary(nextSummary);
      setUsers(nextUsers ?? []);
      setExits(nextExits ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  useEffect(() => {
    loadAll();
    const timer = window.setInterval(loadAll, 7000);
    return () => window.clearInterval(timer);
  }, []);

  return (
    <main>
      <aside>
        <div className="brand">
          <Server size={22} />
          <span>sing-panel</span>
        </div>
        <NavButton active={tab === 'dashboard'} onClick={() => setTab('dashboard')} icon={<Activity size={18} />} label="仪表盘" />
        <NavButton active={tab === 'users'} onClick={() => setTab('users')} icon={<Users size={18} />} label="用户" />
        <NavButton active={tab === 'nodes'} onClick={() => setTab('nodes')} icon={<Server size={18} />} label="节点" />
        <NavButton active={tab === 'subscriptions'} onClick={() => setTab('subscriptions')} icon={<Database size={18} />} label="订阅" />
        <button className="ghost refresh" onClick={loadAll} title="刷新">
          <RefreshCcw size={18} />
        </button>
      </aside>
      <section className="content">
        <header>
          <div>
            <h1>{tabTitle(tab)}</h1>
            <p>{summary ? `${summary.enabled_users}/${summary.user_count} 用户启用 · ${summary.online_exit_nodes}/${summary.exit_node_count} Exit 在线` : '加载中'}</p>
          </div>
          {error && <div className="error">{error}</div>}
        </header>
        {tab === 'dashboard' && <Dashboard summary={summary} users={users} exits={exits} />}
        {tab === 'users' && <UsersView users={users} reload={loadAll} />}
        {tab === 'nodes' && <NodesView exits={exits} reload={loadAll} />}
        {tab === 'subscriptions' && <SubscriptionsView users={users} />}
      </section>
    </main>
  );
}

function NavButton({ active, icon, label, onClick }: { active: boolean; icon: React.ReactNode; label: string; onClick: () => void }) {
  return (
    <button className={active ? 'nav active' : 'nav'} onClick={onClick}>
      {icon}
      <span>{label}</span>
    </button>
  );
}

function tabTitle(tab: Tab) {
  return ({ dashboard: '仪表盘', users: '用户管理', nodes: '节点拓扑', subscriptions: '订阅预览' } as const)[tab];
}

function Dashboard({ summary, users, exits }: { summary: Summary | null; users: User[]; exits: ExitNode[] }) {
  return (
    <div className="stack">
      <div className="metrics">
        <Metric label="用户" value={summary?.user_count ?? 0} sub={`${summary?.enabled_users ?? 0} enabled`} />
        <Metric label="Exit" value={summary?.exit_node_count ?? 0} sub={`${summary?.online_exit_nodes ?? 0} online`} />
        <Metric label="中转" value={exits.filter((node) => node.relay_enabled).length} sub="relay endpoints" />
        <Metric label="总用量" value={formatBytes(summary?.total_used_bytes ?? 0)} sub="accounted traffic" />
      </div>
      <div className="grid two">
        <Panel title="最近用户">
          <Rows empty="暂无用户">
            {users.slice(0, 6).map((user) => (
              <div className="row" key={user.id}>
                <span>{user.name}</span>
                <strong>{formatBytes(user.used_bytes)} / {user.quota_bytes ? formatBytes(user.quota_bytes) : '不限'}</strong>
              </div>
            ))}
          </Rows>
        </Panel>
        <Panel title="节点状态">
          <Rows empty="暂无节点">
            {exits.map((node) => (
              <div className="row" key={node.id}>
                <span>{node.name}</span>
                <strong>{nodeStatus(node)}</strong>
              </div>
            ))}
          </Rows>
        </Panel>
      </div>
    </div>
  );
}

function Metric({ label, value, sub }: { label: string; value: React.ReactNode; sub: string }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{sub}</small>
    </div>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="panel">
      <h2>{title}</h2>
      {children}
    </section>
  );
}

function Rows({ children, empty }: { children: React.ReactNode; empty: string }) {
  const items = React.Children.toArray(children).filter(Boolean);
  return <div className="rows">{items.length ? items : <div className="empty">{empty}</div>}</div>;
}

function UsersView({ users, reload }: { users: User[]; reload: () => Promise<void> }) {
  const [name, setName] = useState('');
  const [quotaGB, setQuotaGB] = useState('100');
  const [editingID, setEditingID] = useState('');
  const [editName, setEditName] = useState('');
  const [editQuotaGB, setEditQuotaGB] = useState('');
  const [error, setError] = useState('');

  async function createUser(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    try {
      await api('/users', {
        method: 'POST',
        body: JSON.stringify({ name, quota_bytes: gbToBytes(quotaGB) }),
      });
      setName('');
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  async function toggle(user: User) {
    setError('');
    try {
      await api(`/users/${user.id}`, { method: 'PATCH', body: JSON.stringify({ enabled: !user.enabled }) });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  async function resetSubscription(user: User) {
    setError('');
    try {
      await api(`/users/${user.id}`, { method: 'PATCH', body: JSON.stringify({ reset_subscription_token: true }) });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  async function resetTraffic(user: User) {
    setError('');
    try {
      await api(`/users/${user.id}`, { method: 'PATCH', body: JSON.stringify({ reset_used_bytes: true }) });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  async function deleteUser(user: User) {
    if (!window.confirm(`删除用户 ${user.name}？订阅和历史流量记录也会一起删除。`)) {
      return;
    }
    setError('');
    try {
      await api(`/users/${user.id}`, { method: 'DELETE' });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  function startEdit(user: User) {
    setEditingID(user.id);
    setEditName(user.name);
    setEditQuotaGB(bytesToGB(user.quota_bytes));
  }

  async function saveUser(event: React.FormEvent, user: User) {
    event.preventDefault();
    setError('');
    try {
      await api(`/users/${user.id}`, {
        method: 'PATCH',
        body: JSON.stringify({ name: editName, quota_bytes: gbToBytes(editQuotaGB) }),
      });
      setEditingID('');
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  return (
    <div className="stack">
      <form className="toolbar" onSubmit={createUser}>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="用户名称" required />
        <input value={quotaGB} onChange={(e) => setQuotaGB(e.target.value)} type="number" min="0" step="1" placeholder="GB" />
        <button className="primary"><Plus size={17} />新增</button>
      </form>
      {error && <div className="error">{error}</div>}
      <Panel title="用户列表">
        <Rows empty="暂无用户">
          {users.map((user) => (
            <div className="row tall" key={user.id}>
              {editingID === user.id ? (
                <form className="inline-edit" onSubmit={(event) => saveUser(event, user)}>
                  <input value={editName} onChange={(e) => setEditName(e.target.value)} required />
                  <input value={editQuotaGB} onChange={(e) => setEditQuotaGB(e.target.value)} type="number" min="0" step="0.01" />
                  <button className="icon primary" title="保存"><Save size={17} /></button>
                  <button className="icon ghost-light" type="button" onClick={() => setEditingID('')} title="取消"><X size={17} /></button>
                </form>
              ) : (
                <>
                  <div>
                    <span>{user.name}</span>
                    <small>{user.id}</small>
                  </div>
                  <strong>{formatBytes(user.used_bytes)} / {user.quota_bytes ? formatBytes(user.quota_bytes) : '不限'}</strong>
                  <div className="actions">
                    <button className="icon ghost-light" onClick={() => startEdit(user)} title="编辑用户"><Edit3 size={17} /></button>
                    <button className="icon ghost-light" onClick={() => resetTraffic(user)} title="重置流量"><RefreshCcw size={17} /></button>
                    <button className="ghost-light text-button" onClick={() => resetSubscription(user)} title="重置订阅链接">Token</button>
                    <button className={user.enabled ? 'toggle on' : 'toggle'} onClick={() => toggle(user)}>{user.enabled ? '启用' : '停用'}</button>
                    <button className="icon ghost-light danger" onClick={() => deleteUser(user)} title="删除用户"><Trash2 size={17} /></button>
                  </div>
                </>
              )}
            </div>
          ))}
        </Rows>
      </Panel>
    </div>
  );
}

function NodesView({ exits, reload }: { exits: ExitNode[]; reload: () => Promise<void> }) {
  const [exitName, setExitName] = useState('JP Node');
  const [exitHost, setExitHost] = useState('jp.example.com');
  const [preview, setPreview] = useState('');
  const [previewTitle, setPreviewTitle] = useState('desired config');
  const [error, setError] = useState('');

  async function createExit(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    try {
      await api('/exit-nodes', {
        method: 'POST',
        body: JSON.stringify({
          name: exitName,
          hostname: exitHost,
          enabled: true,
          anytls_enabled: true,
          ss_enabled: true,
          anytls_port: 2443,
          ss_port: 8388,
          ss_method: '2022-blake3-aes-128-gcm',
          relay_enabled: false,
          cert_mode: 'manual',
          certificate_path: '/etc/sing-box/cert.pem',
          key_path: '/etc/sing-box/key.pem',
        }),
      });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  async function loadDesiredConfig(node: ExitNode) {
    setError('');
    try {
      const data = await api<Record<string, unknown>>(`/agent/${node.id}/desired-config`, {
        headers: { 'X-Sing-Panel-Agent-Token': node.agent_token },
      });
      setPreviewTitle(`${node.name} desired config`);
      setPreview(JSON.stringify(data, null, 2));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  return (
    <div className="stack">
      {error && <div className="error">{error}</div>}
      <div className="grid two">
        <Panel title="节点">
          <form className="form" onSubmit={createExit}>
            <input value={exitName} onChange={(e) => setExitName(e.target.value)} required />
            <input value={exitHost} onChange={(e) => setExitHost(e.target.value)} required />
            <button className="primary"><Plus size={17} />新增节点</button>
          </form>
          <Rows empty="暂无节点">
            {exits.map((node) => (
              <ExitNodeEditor key={node.id} node={node} reload={reload} onPreview={() => loadDesiredConfig(node)} />
            ))}
          </Rows>
        </Panel>
        <Panel title="订阅出口">
          <Rows empty="暂无可用节点">
            {exits.filter((node) => node.enabled).map((node) => (
              <div className="row tall" key={node.id}>
                <div>
                  <span>{node.name}</span>
                  <small>{node.relay_enabled ? node.relay_host : node.hostname}</small>
                </div>
                <strong>{protocolSummary(node)}</strong>
              </div>
            ))}
          </Rows>
        </Panel>
      </div>
      <Panel title={previewTitle}>
        <pre className="compact">{preview || '选择一个 Exit 预览 agent desired config'}</pre>
      </Panel>
    </div>
  );
}

function ExitNodeEditor({ node, reload, onPreview }: { node: ExitNode; reload: () => Promise<void>; onPreview: () => void }) {
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState(() => exitFormFromNode(node));
  const [error, setError] = useState('');

  useEffect(() => {
    setForm(exitFormFromNode(node));
  }, [node]);

  function update(key: keyof typeof form, value: string | boolean) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    try {
      await api(`/exit-nodes/${node.id}`, {
        method: 'PATCH',
        body: JSON.stringify({
          name: form.name,
          hostname: form.hostname,
          anytls_enabled: form.anytls_enabled,
          ss_enabled: form.ss_enabled,
          anytls_port: Number(form.anytls_port),
          anytls_padding_scheme: form.anytls_padding_scheme,
          ss_port: Number(form.ss_port),
          ss_method: form.ss_method,
          relay_enabled: form.relay_enabled,
          relay_host: form.relay_host,
          relay_anytls_port: Number(form.relay_anytls_port || 0),
          relay_ss_port: Number(form.relay_ss_port || 0),
          cert_mode: form.cert_mode,
          cert_domain: form.cert_domain,
          certificate_path: form.certificate_path,
          key_path: form.key_path,
          acme_email: form.acme_email,
          cloudflare_api_token_env: form.cloudflare_api_token_env,
          stats_mode: form.stats_mode,
          stats_api_listen: form.stats_api_listen,
        }),
      });
      setOpen(false);
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  async function resetAgentToken() {
    setError('');
    try {
      await api(`/exit-nodes/${node.id}`, { method: 'PATCH', body: JSON.stringify({ reset_agent_token: true }) });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  async function toggleNode() {
    setError('');
    try {
      await api(`/exit-nodes/${node.id}`, { method: 'PATCH', body: JSON.stringify({ enabled: !node.enabled }) });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  async function deleteNode() {
    if (!window.confirm(`删除节点 ${node.name}？面板会停止管理它，并删除该节点的历史流量记录。`)) {
      return;
    }
    setError('');
    try {
      await api(`/exit-nodes/${node.id}`, { method: 'DELETE' });
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'request failed');
    }
  }

  return (
    <div className="node-editor">
      <div className="node-summary">
        <div>
          <span>{node.name}</span>
          <small>{node.hostname} · {node.enabled ? '运行' : '暂停'} · {protocolSummary(node)} · {node.relay_enabled ? '中转' : '直连'} · {versionSummary(node)}</small>
          <small>agent token: {node.agent_token}</small>
          {node.last_agent_error && <small className="danger-text">{node.last_agent_error}</small>}
        </div>
        <strong>{node.anytls_port}/{node.ss_port}</strong>
        <div className="actions">
          <button className="icon ghost-light" onClick={onPreview} title="预览 desired config"><Eye size={17} /></button>
          <button className="ghost-light text-button" onClick={resetAgentToken} title="重置 Agent Token">Token</button>
          <button className={node.enabled ? 'toggle on' : 'toggle'} onClick={toggleNode} title={node.enabled ? '暂停节点' : '恢复节点'}>{node.enabled ? '运行' : '暂停'}</button>
          <button className="icon ghost-light" onClick={() => setOpen((value) => !value)} title="编辑 Exit"><Edit3 size={17} /></button>
          <button className="icon ghost-light danger" onClick={deleteNode} title="删除节点"><Trash2 size={17} /></button>
        </div>
      </div>
      {error && !open && <div className="error slim">{error}</div>}
      {open && (
        <form className="node-form" onSubmit={save}>
          <div className="form-grid">
            <label>名称<input value={form.name} onChange={(e) => update('name', e.target.value)} required /></label>
            <label>Hostname<input value={form.hostname} onChange={(e) => update('hostname', e.target.value)} required /></label>
            <label className="check"><input checked={form.anytls_enabled} onChange={(e) => update('anytls_enabled', e.target.checked)} type="checkbox" />启用 AnyTLS</label>
            <label className="check"><input checked={form.ss_enabled} onChange={(e) => update('ss_enabled', e.target.checked)} type="checkbox" />启用 Shadowsocks</label>
            <label>AnyTLS 端口<input value={form.anytls_port} onChange={(e) => update('anytls_port', e.target.value)} type="number" min="1" max="65535" required /></label>
            <label>SS 端口<input value={form.ss_port} onChange={(e) => update('ss_port', e.target.value)} type="number" min="1" max="65535" required /></label>
            <label className="wide">AnyTLS padding_scheme<textarea value={form.anytls_padding_scheme} onChange={(e) => update('anytls_padding_scheme', e.target.value)} placeholder={DEFAULT_ANYTLS_PADDING_SCHEME} rows={6} disabled={!form.anytls_enabled} /></label>
            <label>SS method
              <select value={form.ss_method} onChange={(e) => update('ss_method', e.target.value)} disabled={!form.ss_enabled}>
                {SS_METHODS.map((method) => <option key={method} value={method}>{method}</option>)}
              </select>
            </label>
            <label className="check"><input checked={form.relay_enabled} onChange={(e) => update('relay_enabled', e.target.checked)} type="checkbox" />订阅使用中转</label>
            <label>中转 Host<input value={form.relay_host} onChange={(e) => update('relay_host', e.target.value)} placeholder="relay.example.com" disabled={!form.relay_enabled} /></label>
            <label>中转 AnyTLS 端口<input value={form.relay_anytls_port} onChange={(e) => update('relay_anytls_port', e.target.value)} type="number" min="0" max="65535" disabled={!form.relay_enabled} /></label>
            <label>中转 SS 端口<input value={form.relay_ss_port} onChange={(e) => update('relay_ss_port', e.target.value)} type="number" min="0" max="65535" disabled={!form.relay_enabled} /></label>
            <label>证书模式
              <select value={form.cert_mode} onChange={(e) => update('cert_mode', e.target.value)}>
                <option value="manual">manual</option>
                <option value="acme">acme</option>
              </select>
            </label>
            <label>证书域名<input value={form.cert_domain} onChange={(e) => update('cert_domain', e.target.value)} placeholder="example.com" /></label>
            {form.cert_mode === 'manual' ? (
              <>
                <label>证书路径<input value={form.certificate_path} onChange={(e) => update('certificate_path', e.target.value)} placeholder="/etc/sing-box/cert.pem" /></label>
                <label>私钥路径<input value={form.key_path} onChange={(e) => update('key_path', e.target.value)} placeholder="/etc/sing-box/key.pem" /></label>
              </>
            ) : (
              <>
                <label>ACME 邮箱<input value={form.acme_email} onChange={(e) => update('acme_email', e.target.value)} placeholder="admin@example.com" /></label>
                <label>Cloudflare Token 环境变量<input value={form.cloudflare_api_token_env} onChange={(e) => update('cloudflare_api_token_env', e.target.value)} placeholder="CLOUDFLARE_API_TOKEN" /></label>
              </>
            )}
            <label>统计模式
              <select value={form.stats_mode} onChange={(e) => update('stats_mode', e.target.value)}>
                <option value="mock">mock</option>
                <option value="v2ray-api">v2ray-api</option>
              </select>
            </label>
            <label>Stats API Listen<input value={form.stats_api_listen} onChange={(e) => update('stats_api_listen', e.target.value)} placeholder="127.0.0.1:10085" /></label>
          </div>
          {error && <div className="error slim">{error}</div>}
          <div className="form-actions">
            <button className="primary"><Save size={17} />保存</button>
            <button className="ghost-light text-button" type="button" onClick={() => setOpen(false)}><X size={17} />取消</button>
          </div>
        </form>
      )}
    </div>
  );
}

function exitFormFromNode(node: ExitNode) {
  return {
    name: node.name,
    hostname: node.hostname,
    anytls_enabled: node.anytls_enabled ?? true,
    ss_enabled: node.ss_enabled ?? true,
    anytls_port: String(node.anytls_port),
    anytls_padding_scheme: node.anytls_padding_scheme || '',
    ss_port: String(node.ss_port),
    ss_method: node.ss_method || 'aes-128-gcm',
    relay_enabled: node.relay_enabled ?? false,
    relay_host: node.relay_host || '',
    relay_anytls_port: String(node.relay_anytls_port || ''),
    relay_ss_port: String(node.relay_ss_port || ''),
    cert_mode: node.cert_mode || 'manual',
    cert_domain: node.cert_domain || '',
    certificate_path: node.certificate_path || '',
    key_path: node.key_path || '',
    acme_email: node.acme_email || '',
    cloudflare_api_token_env: node.cloudflare_api_token_env || '',
    stats_mode: node.stats_mode || 'mock',
    stats_api_listen: node.stats_api_listen || '127.0.0.1:10085',
  };
}

function protocolSummary(node: ExitNode) {
  const protocols = [];
  if (node.anytls_enabled) protocols.push(`AnyTLS:${node.relay_enabled && node.relay_anytls_port ? node.relay_anytls_port : node.anytls_port}`);
  if (node.ss_enabled) protocols.push(`SS:${node.relay_enabled && node.relay_ss_port ? node.relay_ss_port : node.ss_port} ${node.ss_method || 'aes-128-gcm'}`);
  return protocols.join(' / ') || '未启用';
}

function versionSummary(node: ExitNode) {
  const applied = node.applied_config_version || 0;
  const desired = node.expected_config_version || 0;
  return `applied v${applied} / desired v${desired}`;
}

function nodeStatus(node: ExitNode) {
  const pending = (node.applied_config_version || 0) < (node.expected_config_version || 0);
  if (!node.enabled) {
    if (!node.last_heartbeat_at) return '暂停/离线';
    if (node.last_agent_error) return '暂停错误';
    return pending ? '暂停待应用' : '已暂停';
  }
  if (!node.last_heartbeat_at) return '离线';
  if (node.last_agent_error) return '错误';
  if (pending) return '待应用';
  return '在线';
}

function SubscriptionsView({ users }: { users: User[] }) {
  const enabledUsers = users.filter((user) => user.enabled);
  const [userID, setUserID] = useState('');
  const selected = userID || enabledUsers[0]?.id || '';
  const [preview, setPreview] = useState('');
  const selectedUser = useMemo(() => enabledUsers.find((user) => user.id === selected), [enabledUsers, selected]);
  const subscriptionPath = selectedUser?.subscription_token ? `/sub/${selectedUser.subscription_token}` : '';
  const url = useMemo(() => subscriptionPath ? `${window.location.origin}${subscriptionPath}` : '', [subscriptionPath]);

  async function loadPreview() {
    if (!subscriptionPath) return;
    const res = await fetch(subscriptionPath);
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || res.statusText);
    }
    const data = await res.json() as Record<string, unknown>;
    setPreview(JSON.stringify(data, null, 2));
  }

  useEffect(() => {
    loadPreview().catch((err) => setPreview(err instanceof Error ? err.message : 'request failed'));
  }, [subscriptionPath]);

  return (
    <div className="stack">
      <div className="toolbar">
        <select value={selected} onChange={(e) => setUserID(e.target.value)}>
          {enabledUsers.map((user) => <option key={user.id} value={user.id}>{user.name}</option>)}
        </select>
        <button className="primary" onClick={loadPreview} disabled={!selected}><Play size={17} />预览</button>
      </div>
      <Panel title="订阅 URL">
        <code className="url">{url || '暂无启用用户'}</code>
      </Panel>
      <pre>{preview}</pre>
    </div>
  );
}

createRoot(document.getElementById('root')!).render(<App />);

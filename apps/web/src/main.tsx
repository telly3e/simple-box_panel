import React, { useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Activity, Database, Play, Plus, RefreshCcw, Server, Users } from 'lucide-react';
import './styles.css';

const API = 'http://localhost:8080/api';

type Summary = {
  user_count: number;
  enabled_users: number;
  exit_node_count: number;
  entry_node_count: number;
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
};

type ExitNode = {
  id: string;
  name: string;
  hostname: string;
  anytls_port: number;
  ss_port: number;
  cert_mode: string;
  cert_domain: string;
  certificate_path: string;
  key_path: string;
  acme_email: string;
  cloudflare_api_token_env: string;
  last_heartbeat_at?: string;
  expected_config_version: number;
};

type EntryNode = {
  id: string;
  name: string;
  public_host: string;
  public_anytls_port: number;
  public_ss_port: number;
  exit_node_id: string;
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

function App() {
  const [tab, setTab] = useState<Tab>('dashboard');
  const [summary, setSummary] = useState<Summary | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [exits, setExits] = useState<ExitNode[]>([]);
  const [entries, setEntries] = useState<EntryNode[]>([]);
  const [error, setError] = useState('');

  async function loadAll() {
    setError('');
    try {
      const [nextSummary, nextUsers, nextExits, nextEntries] = await Promise.all([
        api<Summary>('/summary'),
        api<User[]>('/users'),
        api<ExitNode[]>('/exit-nodes'),
        api<EntryNode[]>('/entry-nodes'),
      ]);
      setSummary(nextSummary);
      setUsers(nextUsers);
      setExits(nextExits);
      setEntries(nextEntries);
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
        {tab === 'dashboard' && <Dashboard summary={summary} users={users} exits={exits} entries={entries} />}
        {tab === 'users' && <UsersView users={users} reload={loadAll} />}
        {tab === 'nodes' && <NodesView exits={exits} entries={entries} reload={loadAll} />}
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

function Dashboard({ summary, users, exits, entries }: { summary: Summary | null; users: User[]; exits: ExitNode[]; entries: EntryNode[] }) {
  return (
    <div className="stack">
      <div className="metrics">
        <Metric label="用户" value={summary?.user_count ?? 0} sub={`${summary?.enabled_users ?? 0} enabled`} />
        <Metric label="Exit" value={summary?.exit_node_count ?? 0} sub={`${summary?.online_exit_nodes ?? 0} online`} />
        <Metric label="Entry" value={summary?.entry_node_count ?? 0} sub="public nodes" />
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
                <strong>{node.last_heartbeat_at ? 'online' : 'offline'}</strong>
              </div>
            ))}
            {entries.map((node) => (
              <div className="row muted" key={node.id}>
                <span>{node.name}</span>
                <strong>{node.public_host}</strong>
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

  async function createUser(event: React.FormEvent) {
    event.preventDefault();
    await api('/users', {
      method: 'POST',
      body: JSON.stringify({ name, quota_bytes: Math.round(Number(quotaGB || 0) * 1024 * 1024 * 1024) }),
    });
    setName('');
    await reload();
  }

  async function toggle(user: User) {
    await api(`/users/${user.id}`, { method: 'PATCH', body: JSON.stringify({ enabled: !user.enabled }) });
    await reload();
  }

  return (
    <div className="stack">
      <form className="toolbar" onSubmit={createUser}>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="用户名称" required />
        <input value={quotaGB} onChange={(e) => setQuotaGB(e.target.value)} type="number" min="0" step="1" placeholder="GB" />
        <button className="primary"><Plus size={17} />新增</button>
      </form>
      <Panel title="用户列表">
        <Rows empty="暂无用户">
          {users.map((user) => (
            <div className="row tall" key={user.id}>
              <div>
                <span>{user.name}</span>
                <small>{user.id}</small>
              </div>
              <strong>{formatBytes(user.used_bytes)} / {user.quota_bytes ? formatBytes(user.quota_bytes) : '不限'}</strong>
              <button className={user.enabled ? 'toggle on' : 'toggle'} onClick={() => toggle(user)}>{user.enabled ? '启用' : '停用'}</button>
            </div>
          ))}
        </Rows>
      </Panel>
    </div>
  );
}

function NodesView({ exits, entries, reload }: { exits: ExitNode[]; entries: EntryNode[]; reload: () => Promise<void> }) {
  const [exitName, setExitName] = useState('HK Exit');
  const [exitHost, setExitHost] = useState('exit.local');
  const [entryName, setEntryName] = useState('HK Entry');
  const [entryHost, setEntryHost] = useState('hk.example.com');
  const [selectedExit, setSelectedExit] = useState('');

  const exitID = selectedExit || exits[0]?.id || '';

  async function createExit(event: React.FormEvent) {
    event.preventDefault();
    await api('/exit-nodes', {
      method: 'POST',
      body: JSON.stringify({
        name: exitName,
        hostname: exitHost,
        anytls_port: 2443,
        ss_port: 8388,
        cert_mode: 'manual',
        certificate_path: '/etc/sing-box/cert.pem',
        key_path: '/etc/sing-box/key.pem',
      }),
    });
    await reload();
  }

  async function createEntry(event: React.FormEvent) {
    event.preventDefault();
    await api('/entry-nodes', {
      method: 'POST',
      body: JSON.stringify({
        name: entryName,
        public_host: entryHost,
        public_anytls_port: 443,
        public_ss_port: 8443,
        exit_node_id: exitID,
      }),
    });
    await reload();
  }

  return (
    <div className="grid two">
      <Panel title="Exit">
        <form className="form" onSubmit={createExit}>
          <input value={exitName} onChange={(e) => setExitName(e.target.value)} required />
          <input value={exitHost} onChange={(e) => setExitHost(e.target.value)} required />
          <button className="primary"><Plus size={17} />新增 Exit</button>
        </form>
        <Rows empty="暂无 Exit">
          {exits.map((node) => (
            <div className="row tall" key={node.id}>
              <div>
                <span>{node.name}</span>
                <small>{node.hostname}</small>
              </div>
              <strong>{node.anytls_port}/{node.ss_port}</strong>
            </div>
          ))}
        </Rows>
      </Panel>
      <Panel title="Entry">
        <form className="form" onSubmit={createEntry}>
          <input value={entryName} onChange={(e) => setEntryName(e.target.value)} required />
          <input value={entryHost} onChange={(e) => setEntryHost(e.target.value)} required />
          <select value={exitID} onChange={(e) => setSelectedExit(e.target.value)} required>
            {exits.map((node) => <option key={node.id} value={node.id}>{node.name}</option>)}
          </select>
          <button className="primary" disabled={!exitID}><Plus size={17} />新增 Entry</button>
        </form>
        <Rows empty="暂无 Entry">
          {entries.map((node) => (
            <div className="row tall" key={node.id}>
              <div>
                <span>{node.name}</span>
                <small>{node.public_host}</small>
              </div>
              <strong>{node.public_anytls_port}/{node.public_ss_port}</strong>
            </div>
          ))}
        </Rows>
      </Panel>
    </div>
  );
}

function SubscriptionsView({ users }: { users: User[] }) {
  const enabledUsers = users.filter((user) => user.enabled);
  const [userID, setUserID] = useState('');
  const selected = userID || enabledUsers[0]?.id || '';
  const [preview, setPreview] = useState('');
  const url = useMemo(() => selected ? `${API}/subscriptions/${selected}/sing-box.json` : '', [selected]);

  async function loadPreview() {
    if (!selected) return;
    const data = await api<Record<string, unknown>>(`/subscriptions/${selected}/sing-box.json`);
    setPreview(JSON.stringify(data, null, 2));
  }

  useEffect(() => {
    loadPreview().catch((err) => setPreview(err instanceof Error ? err.message : 'request failed'));
  }, [selected]);

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


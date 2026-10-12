import React, { useEffect, useState, useCallback } from 'react';
import { Layout, Menu, message, Button, Spin } from 'antd';
import {
  DashboardOutlined, ApiOutlined, KeyOutlined, FileTextOutlined,
  TagsOutlined, TeamOutlined, LogoutOutlined,
} from '@ant-design/icons';
import { api } from './api.js';
import LoginPage from './pages/Login.jsx';
import DashboardPage from './pages/Dashboard.jsx';
import ChannelsPage from './pages/Channels.jsx';
import TokensPage from './pages/Tokens.jsx';
import LogsPage from './pages/Logs.jsx';
import PricesPage from './pages/Prices.jsx';
import UsersPage from './pages/Users.jsx';

const { Header, Sider, Content } = Layout;

export default function App() {
  const [ready, setReady] = useState(false);
  const [logged, setLogged] = useState(false);
  const [page, setPage] = useState('dashboard');

  useEffect(() => {
    api.get('/api/status').then(() => setReady(true)).catch(() => setReady(true));
    api.get('/api/token').then(() => { setLogged(true); setReady(true); })
      .catch(() => { setLogged(false); setReady(true); });
  }, []);

  const logout = useCallback(async () => {
    try { await api.post('/api/user/logout'); } catch (e) { /* ignore */ }
    window.location.hash = '#/login';
    window.location.reload();
  }, []);

  if (!ready) {
    return <div style={{ display: 'flex', justifyContent: 'center', paddingTop: '20vh' }}><Spin size="large" /></div>;
  }

  window.api = api; // handy in console
  if (!logged) {
    return <LoginPage onLogin={() => { setLogged(true); setPage('dashboard'); }} />;
  }

  const pages = {
    dashboard: <DashboardPage />,
    channels: <ChannelsPage />,
    tokens: <TokensPage />,
    logs: <LogsPage />,
    prices: <PricesPage />,
    users: <UsersPage />,
  };

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider breakpoint="lg" collapsedWidth="0">
        <div style={{ color: '#fff', padding: '16px', fontSize: 18, fontWeight: 600, letterSpacing: 1 }}>
          htokenmeter
        </div>
        <Menu theme="dark" mode="inline" selectedKeys={[page]} onClick={(e) => setPage(e.key)}
          items={[
            { key: 'dashboard', icon: <DashboardOutlined />, label: '用量看板' },
            { key: 'channels', icon: <ApiOutlined />, label: '渠道' },
            { key: 'tokens', icon: <KeyOutlined />, label: '令牌' },
            { key: 'logs', icon: <FileTextOutlined />, label: '日志' },
            { key: 'prices', icon: <TagsOutlined />, label: '价格表' },
            { key: 'users', icon: <TeamOutlined />, label: '用户' },
          ]} />
        <div style={{ position: 'absolute', bottom: 12, left: 0, right: 0, textAlign: 'center' }}>
          <Button type="text" icon={<LogoutOutlined />} onClick={logout}
            style={{ color: 'rgba(255,255,255,0.65)' }}>退出</Button>
        </div>
      </Sider>
      <Layout>
        <Content style={{ padding: 16, overflow: 'auto' }}>
          <ErrorBoundary>{pages[page]}</ErrorBoundary>
        </Content>
      </Layout>
    </Layout>
  );
}

class ErrorBoundary extends React.Component {
  constructor(props) { super(props); this.state = {}; }
  static getDerivedStateFromError(err) { return { err }; }
  componentDidCatch(err) { message.error(String(err)); }
  render() {
    if (this.state.err) {
      return <div>页面出错：{String(this.state.err)}</div>;
    }
    return this.props.children;
  }
}

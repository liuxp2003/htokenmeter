import React, { useState } from 'react';
import { Card, Form, Input, Button, message } from 'antd';
import { api } from '../api.js';

export default function LoginPage({ onLogin }) {
  const [loading, setLoading] = useState(false);
  const onFinish = async ({ username, password }) => {
    setLoading(true);
    try {
      await api.post('/api/user/login', { username, password });
      message.success('登录成功');
      onLogin();
    } catch (e) {
      message.error(e.message);
    } finally {
      setLoading(false);
    }
  };
  return (
    <div style={{ display: 'flex', justifyContent: 'center', paddingTop: '12vh' }}>
      <Card title="htokenmeter 登录" style={{ width: 360 }}>
        <Form layout="vertical" onFinish={onFinish}>
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input autoFocus />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password />
          </Form.Item>
          <Button type="primary" htmlType="submit" block loading={loading}>登录</Button>
        </Form>
        <p style={{ color: '#999', fontSize: 12, marginTop: 12 }}>
          首次启动的管理员密码在服务日志中（htokenmeter 日志的 "first boot" 行）。
        </p>
      </Card>
    </div>
  );
}

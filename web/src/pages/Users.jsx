import React, { useEffect, useState } from 'react';
import { Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, message } from 'antd';
import { api } from '../api.js';

export default function UsersPage() {
  const [rows, setRows] = useState([]);
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm();

  const load = () => api.get('/api/user').then((r) => setRows(r.data)).catch((e) => message.error(e.message));
  useEffect(() => { load(); }, []);

  const createUser = async () => {
    const v = await form.validateFields();
    try {
      await api.post('/api/user', { username: v.username, password: v.password, display_name: v.display_name || '', role: v.role });
      message.success('已创建');
      setOpen(false);
      load();
    } catch (e) { message.error(e.message); }
  };

  const update = async (record, patch) => {
    try {
      await api.put('/api/user', {
        id: record.id, display_name: record.display_name, role: record.role,
        status: record.status, group: record.group, ...patch,
      });
      load();
    } catch (e) { message.error(e.message); }
  };

  const del = async (id) => {
    try { await api.del(`/api/user/${id}`); message.success('已删除'); load(); }
    catch (e) { message.error(e.message); }
  };

  const cols = [
    { title: 'ID', dataIndex: 'id', width: 50 },
    { title: '用户名', dataIndex: 'username' },
    { title: '显示名', dataIndex: 'display_name' },
    { title: '角色', dataIndex: 'role', render: (r) => r >= 10 ? <Tag color="blue">管理员</Tag> : <Tag>普通用户</Tag> },
    { title: '状态', dataIndex: 'status', render: (s) => s === 1 ? <Tag color="green">启用</Tag> : <Tag color="red">禁用</Tag> },
    {
      title: '操作', render: (_, r) => (
        <Space>
          <a onClick={() => update(r, { role: r.role === 10 ? 1 : 10 })}>{r.role === 10 ? '降为普通' : '升为管理员'}</a>
          <a onClick={() => update(r, { status: r.status === 1 ? 2 : 1 })}>{r.status === 1 ? '禁用' : '启用'}</a>
          <Popconfirm title="删除用户及其令牌？" onConfirm={() => del(r.id)}><a style={{ color: 'red' }}>删除</a></Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Card title="用户管理" extra={<Button type="primary" onClick={() => { form.resetFields(); setOpen(true); }}>新增用户</Button>}>
      <Table rowKey="id" size="small" columns={cols} dataSource={rows} pagination={false} />
      <Modal title="新增用户" open={open} onOk={createUser} onCancel={() => setOpen(false)}>
        <Form form={form} layout="vertical">
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="password" label="密码（至少 8 位）" rules={[{ required: true, min: 8, message: '至少 8 位' }]}><Input.Password /></Form.Item>
          <Form.Item name="display_name" label="显示名"><Input /></Form.Item>
          <Form.Item name="role" label="角色" initialValue={1}><Select options={[{ value: 1, label: '普通用户' }, { value: 10, label: '管理员' }]} /></Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}

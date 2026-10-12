import React, { useEffect, useState } from 'react';
import { Button, Card, Form, Input, InputNumber, Modal, Popconfirm, Space, Table, Tag, Typography, message } from 'antd';
import { api, fmtUsd } from '../api.js';

export default function TokensPage() {
  const [rows, setRows] = useState([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(null);
  const [created, setCreated] = useState(null);
  const [form] = Form.useForm();

  const load = () => api.get('/api/token').then((r) => setRows(r.data)).catch((e) => message.error(e.message));
  useEffect(() => { load(); }, []);

  const openForm = (record) => {
    setEditing(record || null);
    form.resetFields();
    if (record) form.setFieldsValue(record);
    setOpen(true);
  };

  const save = async () => {
    const v = await form.validateFields();
    const payload = {
      id: editing?.id || 0, name: v.name, status: editing?.status ?? 1,
      expired_time: v.expired_days
        ? Math.floor(Date.now() / 1000) + v.expired_days * 86400
        : (editing?.expired_time ?? -1),
      quota_usd: v.quota_usd ?? -1, models: v.models || '', subnet: v.subnet || '',
    };
    try {
      if (editing) { await api.put('/api/token', payload); message.success('已保存'); }
      else {
        const r = await api.post('/api/token', payload);
        setCreated(r.data);
      }
      setOpen(false);
      load();
    } catch (e) { message.error(e.message); }
  };

  const del = async (id) => {
    try { await api.del(`/api/token/${id}`); message.success('已删除'); load(); }
    catch (e) { message.error(e.message); }
  };

  const cols = [
    { title: 'ID', dataIndex: 'id', width: 50 },
    { title: '名称', dataIndex: 'name' },
    { title: '状态', dataIndex: 'status', render: (s) => s === 1 ? <Tag color="green">启用</Tag> : <Tag>停用</Tag> },
    {
      title: 'Key', dataIndex: 'key', render: (k) => (
        <Typography.Text copyable={{ text: 'sk-' + k }} code>sk-{k.slice(0, 12)}…</Typography.Text>
      ),
    },
    { title: '预算', render: (_, r) => r.quota_usd < 0 ? '不限' : `${fmtUsd(r.used_usd * 1e6)} / $${r.quota_usd}` },
    { title: '模型限制', dataIndex: 'models', render: (m) => m || '全部' },
    { title: '过期', dataIndex: 'expired_time', render: (t) => t < 0 ? '永不过期' : new Date(t * 1000).toLocaleDateString() },
    {
      title: '操作', render: (_, r) => (
        <Space>
          <a onClick={() => openForm(r)}>编辑</a>
          <Popconfirm title="删除令牌？" onConfirm={() => del(r.id)}><a style={{ color: 'red' }}>删除</a></Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Card title="访问令牌（发给内网客户端的 sk- key）" extra={<Button type="primary" onClick={() => openForm(null)}>新增令牌</Button>}>
      <Table rowKey="id" size="small" columns={cols} dataSource={rows} pagination={{ pageSize: 15 }} />
      <Modal title={editing ? `编辑令牌 #${editing.id}` : '新增令牌'} open={open} onOk={save} onCancel={() => setOpen(false)}>
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="models" label="允许的模型（逗号分隔，留空=全部）"><Input placeholder="gpt-4o,claude-sonnet-4-5" /></Form.Item>
          <Form.Item name="quota_usd" label="预算上限 USD（-1 或留空=不限）"><InputNumber style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="expired_days" label="有效天数（留空=永不过期）"><InputNumber style={{ width: '100%' }} min={1} /></Form.Item>
          <Form.Item name="subnet" label="IP 限制（CIDR 逗号分隔，留空=不限）"><Input placeholder="192.168.1.0/24" /></Form.Item>
        </Form>
      </Modal>
      <Modal title="令牌已创建" open={!!created} footer={<Button type="primary" onClick={() => setCreated(null)}>我已保存</Button>} onCancel={() => setCreated(null)}>
        {created && (
          <div>
            <p>请立即复制，密钥只显示一次：</p>
            <Typography.Paragraph copyable={{ text: 'sk-' + created.key }} code>sk-{created.key}</Typography.Paragraph>
          </div>
        )}
      </Modal>
    </Card>
  );
}

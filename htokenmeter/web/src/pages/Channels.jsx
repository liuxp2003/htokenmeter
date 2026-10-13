import React, { useEffect, useState } from 'react';
import { Button, Card, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, message } from 'antd';
import { api } from '../api.js';

const TYPES = [
  { value: 1, label: 'OpenAI 兼容（OpenAI/DeepSeek/Ollama/vLLM/聚合站…）' },
  { value: 2, label: 'Anthropic（Claude）' },
  { value: 3, label: 'Google Gemini' },
  { value: 4, label: 'Azure OpenAI' },
];
const STATUS = { 1: ['启用', 'green'], 2: ['手动禁用', 'orange'], 3: ['自动禁用', 'red'] };

export default function ChannelsPage() {
  const [rows, setRows] = useState([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(null);
  const [form] = Form.useForm();

  const load = () => api.get('/api/channel').then((r) => setRows(r.data)).catch((e) => message.error(e.message));
  useEffect(() => { load(); }, []);

  const openForm = (record) => {
    setEditing(record || null);
    form.resetFields();
    if (record) {
      form.setFieldsValue({ ...record, keys: '' });
    }
    setOpen(true);
  };

  const save = async () => {
    const v = await form.validateFields();
    const payload = {
      id: editing?.id || 0, type: v.type, name: v.name, status: editing?.status ?? 1,
      base_url: v.base_url || '', keys: v.keys || '', priority: v.priority || 0,
      weight: v.weight || 0, models: v.models, group: v.group || 'default',
      model_mapping: v.model_mapping || '', proxy: v.proxy || '',
      config: v.config || '',
    };
    try {
      if (editing) await api.put('/api/channel', payload);
      else await api.post('/api/channel', payload);
      message.success('已保存');
      setOpen(false);
      load();
    } catch (e) { message.error(e.message); }
  };

  const del = async (id) => {
    try { await api.del(`/api/channel/${id}`); message.success('已删除'); load(); }
    catch (e) { message.error(e.message); }
  };

  const toggle = async (record) => {
    const next = record.status === 1 ? 2 : 1;
    try {
      await api.post(`/api/channel/${record.id}/status`, { status: next });
      load();
    } catch (e) { message.error(e.message); }
  };

  const test = async (id) => {
    const hide = message.loading('测试中…', 0);
    try {
      const r = await api.get(`/api/channel/${id}/test`);
      if (r.success) message.success(`OK (${r.time_ms}ms): ${r.message}`);
      else message.error(`失败: ${r.message}`);
    } catch (e) { message.error(e.message); }
    finally { hide(); }
  };

  const cols = [
    { title: 'ID', dataIndex: 'id', width: 50 },
    { title: '名称', dataIndex: 'name' },
    { title: '类型', dataIndex: 'type', render: (t) => TYPES.find((x) => x.value === t)?.label.split('（')[0] || t },
    { title: '状态', dataIndex: 'status', render: (s) => <Tag color={STATUS[s]?.[1]}>{STATUS[s]?.[0] || s}</Tag> },
    { title: '模型', dataIndex: 'models', ellipsis: true },
    { title: '分组', dataIndex: 'group' },
    { title: '优先级/权重', render: (_, r) => `${r.priority}/${r.weight}` },
    { title: '累计成本', dataIndex: 'used_usd', render: (v) => '$' + (v || 0).toFixed(4) },
    { title: '延迟', dataIndex: 'response_ms', render: (v) => (v ? `${v}ms` : '-') },
    {
      title: '操作', width: 240, render: (_, r) => (
        <Space>
          <a onClick={() => test(r.id)}>测试</a>
          <a onClick={() => toggle(r)}>{r.status === 1 ? '禁用' : '启用'}</a>
          <a onClick={() => openForm(r)}>编辑</a>
          <Popconfirm title="删除渠道？" onConfirm={() => del(r.id)}><a style={{ color: 'red' }}>删除</a></Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Card title="渠道管理" extra={<Button type="primary" onClick={() => openForm(null)}>新增渠道</Button>}>
      <Table rowKey="id" size="small" columns={cols} dataSource={rows} pagination={{ pageSize: 15 }} />
      <Modal title={editing ? `编辑渠道 #${editing.id}` : '新增渠道'} open={open} onOk={save} onCancel={() => setOpen(false)}
        width={640} destroyOnClose>
        <Form form={form} layout="vertical" initialValues={{ type: 1, priority: 0, weight: 0, group: 'default' }}>
          <Form.Item name="type" label="类型" rules={[{ required: true }]}><Select options={TYPES} /></Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}><Input /></Form.Item>
          <Form.Item name="base_url" label="Base URL（留空用官方默认；Ollama 填 http://IP:11434）"><Input placeholder="https://api.openai.com" /></Form.Item>
          <Form.Item name="keys" label={editing ? '密钥（留空保持不变；多 key 换行分隔）' : '密钥（多 key 换行分隔）'}>
            <Input.TextArea rows={2} placeholder="sk-..." />
          </Form.Item>
          <Form.Item name="models" label="支持的模型（逗号分隔）" rules={[{ required: true }]}><Input placeholder="gpt-4o,claude-sonnet-4-5" /></Form.Item>
          <Form.Item name="group" label="分组（逗号分隔）"><Input /></Form.Item>
          <Space size="large">
            <Form.Item name="priority" label="优先级"><InputNumber /></Form.Item>
            <Form.Item name="weight" label="权重"><InputNumber /></Form.Item>
          </Space>
          <Form.Item name="model_mapping" label='模型映射 JSON（如 {"claude":"claude-3-5-sonnet-latest"}）'><Input /></Form.Item>
          <Form.Item name="proxy" label="代理（http/socks5 URL，可选）"><Input placeholder="http://192.168.1.2:7890" /></Form.Item>
          <Form.Item name="config" label='附加配置 JSON（Azure: {"api_version":"2024-10-21"}）'><Input /></Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}

import React, { useEffect, useState } from 'react';
import { Button, Card, Form, Input, InputNumber, Modal, Popconfirm, Space, Table, message } from 'antd';
import { api } from '../api.js';

export default function PricesPage() {
  const [rows, setRows] = useState([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(null);
  const [form] = Form.useForm();

  const load = () => api.get('/api/price').then((r) => setRows(r.data)).catch((e) => message.error(e.message));
  useEffect(() => { load(); }, []);

  const openForm = (record) => {
    setEditing(record || null);
    form.resetFields();
    if (record) form.setFieldsValue(record);
    setOpen(true);
  };

  const save = async () => {
    const v = await form.validateFields();
    try {
      await api.post('/api/price', {
        model: v.model, prompt_usd: v.prompt_usd || 0, completion_usd: v.completion_usd || 0,
        cache_read_usd: v.cache_read_usd || 0, cache_write_usd: v.cache_write_usd || 0,
        image_usd: v.image_usd || 0, per_request_usd: v.per_request_usd || 0, enabled: true,
      });
      message.success('已保存');
      setOpen(false);
      load();
    } catch (e) { message.error(e.message); }
  };

  const del = async (model) => {
    try { await api.del(`/api/price/${encodeURIComponent(model)}`); message.success('已删除'); load(); }
    catch (e) { message.error(e.message); }
  };

  const cols = [
    { title: '模型', dataIndex: 'model' },
    { title: '输入 $/1M', dataIndex: 'prompt_usd' },
    { title: '输出 $/1M', dataIndex: 'completion_usd' },
    { title: '缓存读 $/1M', dataIndex: 'cache_read_usd' },
    { title: '缓存写 $/1M', dataIndex: 'cache_write_usd' },
    { title: '图像 $/张', dataIndex: 'image_usd' },
    { title: '按次 $', dataIndex: 'per_request_usd' },
    {
      title: '操作', render: (_, r) => (
        <Space>
          <a onClick={() => openForm(r)}>编辑</a>
          <Popconfirm title="删除价格？" onConfirm={() => del(r.model)}><a style={{ color: 'red' }}>删除</a></Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Card title="模型单价表（USD / 1M tokens；成本 = usage × 单价）"
      extra={<Button type="primary" onClick={() => openForm(null)}>新增价格</Button>}>
      <Table rowKey="model" size="small" columns={cols} dataSource={rows} pagination={{ pageSize: 20 }} />
      <Modal title={editing ? `编辑 ${editing.model}` : '新增价格'} open={open} onOk={save} onCancel={() => setOpen(false)}>
        <Form form={form} layout="vertical">
          <Form.Item name="model" label="模型名" rules={[{ required: true }]}><Input disabled={!!editing} /></Form.Item>
          <Form.Item name="prompt_usd" label="输入 $/1M tokens"><InputNumber style={{ width: '100%' }} min={0} step={0.01} /></Form.Item>
          <Form.Item name="completion_usd" label="输出 $/1M tokens"><InputNumber style={{ width: '100%' }} min={0} step={0.01} /></Form.Item>
          <Form.Item name="cache_read_usd" label="缓存读 $/1M"><InputNumber style={{ width: '100%' }} min={0} step={0.01} /></Form.Item>
          <Form.Item name="cache_write_usd" label="缓存写 $/1M"><InputNumber style={{ width: '100%' }} min={0} step={0.01} /></Form.Item>
          <Form.Item name="image_usd" label="图像 $/张"><InputNumber style={{ width: '100%' }} min={0} step={0.01} /></Form.Item>
          <Form.Item name="per_request_usd" label="按次 $/请求"><InputNumber style={{ width: '100%' }} min={0} step={0.001} /></Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}

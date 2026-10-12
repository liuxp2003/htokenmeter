import React, { useEffect, useState } from 'react';
import { Card, Input, Table } from 'antd';
import { api, fmtUsd } from '../api.js';

export default function LogsPage() {
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [model, setModel] = useState('');

  const load = () => {
    const q = new URLSearchParams({ limit: 20, offset: (page - 1) * 20 });
    if (model) q.set('model', model);
    api.get(`/api/log?${q}`).then((r) => { setRows(r.data); setTotal(r.total); }).catch(() => {});
  };
  useEffect(() => { load(); }, [page]); // eslint-disable-line

  const cols = [
    { title: '时间', dataIndex: 'created_time', width: 170, render: (t) => new Date(t * 1000).toLocaleString() },
    { title: '模型', dataIndex: 'model_name' },
    { title: '渠道', dataIndex: 'channel_name' },
    { title: '令牌', dataIndex: 'token_name' },
    { title: '输入', dataIndex: 'prompt_tokens' },
    { title: '输出', dataIndex: 'completion_tokens' },
    { title: '缓存读', dataIndex: 'cache_read_tokens' },
    { title: '流式', dataIndex: 'is_stream', render: (v) => (v ? '是' : '-') },
    { title: '成本', dataIndex: 'cost_micro_usd', render: fmtUsd },
  ];

  return (
    <Card title="用量日志">
      <Input.Search placeholder="按模型过滤" allowClear style={{ width: 260, marginBottom: 12 }}
        onSearch={(v) => { setModel(v); setPage(1); setTimeout(load, 0); }} />
      <Table rowKey="id" size="small" columns={cols} dataSource={rows}
        pagination={{ pageSize: 20, current: page, total, onChange: setPage }} />
    </Card>
  );
}

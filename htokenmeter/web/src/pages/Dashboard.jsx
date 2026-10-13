import React, { useEffect, useState } from 'react';
import { Card, Col, Row, Statistic, Table, Segmented } from 'antd';
import { api, fmtUsd } from '../api.js';

export default function DashboardPage() {
  const [stats, setStats] = useState(null);
  const [days, setDays] = useState(14);

  useEffect(() => {
    api.get(`/api/stats?days=${days}`).then((r) => setStats(r.data)).catch(() => {});
  }, [days]);

  if (!stats) return null;
  const total = (stats.by_day || []).reduce((s, r) => s + r.cost_micro_usd, 0);
  const tokens = (stats.by_day || []).reduce((s, r) => s + r.prompt_tokens + r.completion_tokens, 0);
  const requests = (stats.by_day || []).reduce((s, r) => s + r.requests, 0);

  const dimCols = [
    { title: '名称', dataIndex: 'bucket' },
    { title: '请求数', dataIndex: 'requests' },
    { title: '输入 tokens', dataIndex: 'prompt_tokens' },
    { title: '输出 tokens', dataIndex: 'completion_tokens' },
    { title: '成本', dataIndex: 'cost_micro_usd', render: fmtUsd },
  ];

  return (
    <div>
      <Segmented options={[{ label: '7 天', value: 7 }, { label: '14 天', value: 14 }, { label: '30 天', value: 30 }]}
        value={days} onChange={setDays} style={{ marginBottom: 16 }} />
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={8}><Card><Statistic title={`近 ${days} 天成本`} value={fmtUsd(total)} /></Card></Col>
        <Col span={8}><Card><Statistic title="tokens 用量" value={tokens.toLocaleString()} /></Card></Col>
        <Col span={8}><Card><Statistic title="请求数" value={requests} /></Card></Col>
      </Row>
      <Row gutter={16}>
        <Col span={12}>
          <Card title="按日" size="small">
            <Table size="small" rowKey="bucket" pagination={{ pageSize: 10 }}
              columns={[{ title: '日期', dataIndex: 'bucket' }, ...dimCols.slice(1)]}
              dataSource={stats.by_day || []} />
          </Card>
        </Col>
        <Col span={12}>
          <Card title="按模型" size="small">
            <Table size="small" rowKey="bucket" pagination={{ pageSize: 10 }}
              columns={dimCols} dataSource={stats.by_model || []} />
          </Card>
        </Col>
      </Row>
      <Row gutter={16} style={{ marginTop: 16 }}>
        <Col span={12}>
          <Card title="按渠道" size="small">
            <Table size="small" rowKey="bucket" pagination={{ pageSize: 10 }}
              columns={dimCols} dataSource={stats.by_channel || []} />
          </Card>
        </Col>
        <Col span={12}>
          <Card title="按令牌" size="small">
            <Table size="small" rowKey="bucket" pagination={{ pageSize: 10 }}
              columns={dimCols} dataSource={stats.by_token || []} />
          </Card>
        </Col>
      </Row>
    </div>
  );
}

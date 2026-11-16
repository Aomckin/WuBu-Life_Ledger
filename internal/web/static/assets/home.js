(() => {
 'use strict';
 const currency = new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY' });
 const summaries = {
  items: ['/summary', data => data.tracked + ' 项物品 · ' + data.low_stock + ' 项低库存'],
  finance: ['/money/balance', data => '自由资金 ' + currency.format(data.liquid_balance)],
  ai: ['/intake/settings', data => data.ai_available ? 'AI 已配置' : 'AI 未配置，可手动处理']
 };
 document.querySelectorAll('[data-module]').forEach(async card => {
  const [path, format] = summaries[card.dataset.module];
  const label = card.querySelector('.workspace-status-text');
  try {
   const response = await fetch('/api/v1' + path);
   if (!response.ok) throw new Error(String(response.status));
   label.textContent = format(await response.json());
   card.dataset.state = 'ready';
  } catch {
   label.textContent = '概览暂不可用';
   card.dataset.state = 'unavailable';
  }
 });
})();

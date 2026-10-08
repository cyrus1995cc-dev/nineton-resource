<script setup lang="ts">
import { ref } from 'vue';
import type { Asset } from '../types';

const props = defineProps<{ asset: Asset | null; busy?: boolean }>();
const emit = defineEmits<{ close: []; event: [payload: { type: string; toAssignee?: string; toDepartment?: string; note?: string }] }>();
const showClaim = ref(false);
const assignee = ref('');
const department = ref('');
const note = ref('');

function submitClaim() {
  emit('event', { type: '领用', toAssignee: assignee.value, toDepartment: department.value, note: note.value });
  showClaim.value = false;
}
</script>

<template>
  <div v-if="asset" class="drawer-backdrop" @click.self="emit('close')">
    <section class="drawer">
      <header class="drawer-header"><div><span class="eyebrow">资产详情</span><h2>{{ asset.asset_code }}</h2></div><button class="icon-button" @click="emit('close')">×</button></header>
      <div class="drawer-status"><span class="status-pill" :class="`status-${asset.status}`">{{ asset.status }}</span><span>{{ asset.name || '未命名资产' }}</span></div>
      <div class="detail-grid">
        <div><label>资产分类</label><strong>{{ asset.category_path || '—' }}</strong></div><div><label>所在位置</label><strong>{{ asset.location || '—' }}</strong></div>
        <div><label>使用人</label><strong>{{ asset.assignee || '—' }}</strong></div><div><label>使用部门</label><strong>{{ asset.department || '—' }}</strong></div>
        <div><label>所属公司</label><strong>{{ asset.company || '—' }}</strong></div><div><label>型号</label><strong>{{ asset.model || '—' }}</strong></div>
        <div><label>序列号</label><strong>{{ asset.serial_number || '—' }}</strong></div><div><label>含税金额</label><strong>{{ Number(asset.amount_taxed || 0).toLocaleString('zh-CN', { style: 'currency', currency: 'CNY' }) }}</strong></div>
      </div>
      <div class="qr-raw"><label>二维码原文</label><code>{{ asset.qr_raw || '暂未记录' }}</code></div>
      <div class="drawer-actions">
        <button v-if="asset.status === '空闲'" class="primary-button" @click="showClaim = true">领用资产</button>
        <button v-if="asset.status === '在用' || asset.status === '借出'" class="secondary-button" :disabled="busy" @click="emit('event', { type: asset.status === '借出' ? '借出归还' : '归还' })">归还资产</button>
      </div>
      <div class="timeline"><div class="section-title"><h3>流转记录</h3><span>{{ asset.events?.length ?? 0 }} 条</span></div><div v-if="!asset.events?.length" class="muted">暂无流转记录</div><div v-for="event in asset.events" :key="event.id" class="timeline-item"><span class="timeline-dot"></span><div><strong>{{ event.type }}</strong><span>{{ event.operator_name || '系统' }} · {{ new Date(event.created_at).toLocaleString('zh-CN') }}</span><small v-if="event.note">{{ event.note }}</small></div></div></div>
      <div v-if="showClaim" class="claim-form"><h3>确认领用</h3><input v-model="assignee" placeholder="使用人" /><input v-model="department" placeholder="使用部门" /><textarea v-model="note" placeholder="备注"></textarea><div class="form-actions"><button class="secondary-button" @click="showClaim = false">取消</button><button class="primary-button" @click="submitClaim">确认领用</button></div></div>
    </section>
  </div>
</template>

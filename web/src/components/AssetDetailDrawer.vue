<script setup lang="ts">
import { formatSystemTime } from '../dateTime';
import { reactive, ref, watch } from 'vue';
import type { Asset, AssetUpdateInput } from '../types';

const props = defineProps<{ asset: Asset | null; busy?: boolean; editBusy?: boolean; canWrite?: boolean }>();
const emit = defineEmits<{
  close: [];
  event: [payload: { type: string; toAssignee?: string; toDepartment?: string; note?: string }];
  update: [payload: AssetUpdateInput];
}>();

type Tab = 'basic' | 'usage' | 'events' | 'maintenance' | 'inventory';
const tabs: Array<{ id: Tab; label: string }> = [
  { id: 'basic', label: '基本信息' },
  { id: 'usage', label: '使用信息' },
  { id: 'events', label: '操作记录' },
  { id: 'maintenance', label: '维修记录' },
  { id: 'inventory', label: '盘点记录' },
];
const activeTab = ref<Tab>('basic');
const editing = ref(false);
const showClaim = ref(false);
const assignee = ref('');
const department = ref('');
const note = ref('');

function emptyForm(): AssetUpdateInput {
  return {
    category_level1: '', category_path: '', name: '', location: '', company: '', model: '', cpu: '',
    discrete_gpu: '', memory: '', gpu_model: '', serial_number: '', remark: '', photo_url: '', assignee: '',
    department: '', received_at: '', amount_taxed: 0, amount_untaxed: 0, tax_amount: 0,
  };
}
const form = reactive<AssetUpdateInput>(emptyForm());

function syncForm(asset: Asset | null) {
  Object.assign(form, emptyForm(), asset ? {
    category_level1: asset.category_level1,
    category_path: asset.category_path,
    name: asset.name,
    location: asset.location,
    company: asset.company,
    model: asset.model,
    cpu: asset.cpu,
    discrete_gpu: asset.discrete_gpu,
    memory: asset.memory,
    gpu_model: asset.gpu_model,
    serial_number: asset.serial_number,
    remark: asset.remark,
    photo_url: asset.photo_url,
    assignee: asset.assignee,
    department: asset.department,
    received_at: asset.received_at,
    amount_taxed: Number(asset.amount_taxed || 0),
    amount_untaxed: Number(asset.amount_untaxed || 0),
    tax_amount: Number(asset.tax_amount || 0),
  } : {});
}

watch(() => props.asset, (asset) => {
  syncForm(asset);
  editing.value = false;
  activeTab.value = 'basic';
  showClaim.value = false;
}, { immediate: true });

function startEditing() {
  if (!props.asset) return;
  syncForm(props.asset);
  editing.value = true;
  activeTab.value = 'basic';
}

function cancelEditing() {
  syncForm(props.asset);
  editing.value = false;
}

function saveEditing() {
  if (!props.asset || props.canWrite === false || !form.name.trim()) return;
  emit('update', { ...form, operatorName: '当前管理员' });
}

function submitClaim() {
  emit('event', { type: '领用', toAssignee: assignee.value, toDepartment: department.value, note: note.value });
  showClaim.value = false;
}

function tabEvents() {
  if (!props.asset?.events) return [];
  if (activeTab.value === 'maintenance') return props.asset.events.filter((event) => event.type === '维修');
  if (activeTab.value === 'inventory') return props.asset.events.filter((event) => event.type === '盘点');
  return props.asset.events;
}
</script>

<template>
  <div v-if="asset" class="drawer-backdrop" @click.self="emit('close')">
    <section class="drawer asset-info-drawer">
      <header class="drawer-header">
        <div><span class="eyebrow">资产信息</span><h2>{{ asset.asset_code }}</h2></div>
        <div class="drawer-header-actions"><button v-if="canWrite !== false" class="secondary-button edit-trigger" @click="editing ? cancelEditing() : startEditing()">{{ editing ? '退出编辑' : '编辑资料' }}</button><button class="icon-button" aria-label="关闭" @click="emit('close')">×</button></div>
      </header>

      <div class="drawer-status"><span class="status-pill" :class="`status-${asset.status}`">{{ asset.status }}</span><span>{{ asset.name || '未命名资产' }}</span><span class="readonly-hint">资产编码只读</span></div>

      <div v-if="editing" class="editor-layout">
        <nav class="editor-tabs" aria-label="资产信息页签">
          <button v-for="tab in tabs" :key="tab.id" :class="{ active: activeTab === tab.id }" @click="activeTab = tab.id">{{ tab.label }}</button>
        </nav>
        <div class="editor-content">
          <div v-if="activeTab === 'basic'" class="form-section">
            <div class="section-title"><div><span class="eyebrow">资产属性</span><h3>基本信息</h3></div><span class="form-required">* 必填</span></div>
            <div class="form-grid">
              <label class="field"><span>资产编码</span><input :value="asset.asset_code" disabled /></label>
              <label class="field required"><span>资产名称</span><input v-model="form.name" required /></label>
              <label class="field"><span>资产分类</span><input v-model="form.category_path" placeholder="如：计算机和网络设备/手机" /></label>
              <label class="field"><span>一级分类</span><input v-model="form.category_level1" /></label>
              <label class="field"><span>型号</span><input v-model="form.model" /></label>
              <label class="field"><span>设备序列号</span><input v-model="form.serial_number" /></label>
              <label class="field"><span>CPU</span><input v-model="form.cpu" /></label>
              <label class="field"><span>内存</span><input v-model="form.memory" /></label>
              <label class="field"><span>显卡型号</span><input v-model="form.gpu_model" /></label>
              <label class="field"><span>独立显卡</span><input v-model="form.discrete_gpu" /></label>
            </div>
          </div>

          <div v-else-if="activeTab === 'usage'" class="form-section">
            <div class="section-title"><div><span class="eyebrow">资产归属</span><h3>使用信息</h3></div></div>
            <div class="form-grid">
              <label class="field"><span>所属公司</span><input v-model="form.company" /></label>
              <label class="field"><span>所在位置</span><input v-model="form.location" placeholder="如：12楼" /></label>
              <label class="field"><span>使用人</span><input v-model="form.assignee" /></label>
              <label class="field"><span>使用部门</span><input v-model="form.department" /></label>
              <label class="field"><span>领用日期</span><input v-model="form.received_at" placeholder="YYYY-MM-DD" /></label>
              <label class="field readonly-field"><span>当前状态</span><input :value="asset.status" disabled /></label>
            </div>
          </div>

          <div v-else-if="activeTab === 'events' || activeTab === 'maintenance' || activeTab === 'inventory'" class="form-section history-section">
            <div class="section-title"><div><span class="eyebrow">资产履历</span><h3>{{ tabs.find((tab) => tab.id === activeTab)?.label }}</h3></div><span>{{ tabEvents().length }} 条</span></div>
            <div v-if="!tabEvents().length" class="empty-history">暂无{{ tabs.find((tab) => tab.id === activeTab)?.label }}</div>
            <div v-for="event in tabEvents()" :key="event.id" class="timeline-item"><span class="timeline-dot"></span><div><strong>{{ event.type }}</strong><span>{{ event.operator_name || '系统' }} · {{ formatSystemTime(event.created_at) }}</span><small v-if="event.note">{{ event.note }}</small></div></div>
          </div>

          <div class="form-section finance-section">
            <div class="section-title"><div><span class="eyebrow">财务信息</span><h3>金额与备注</h3></div></div>
            <div class="form-grid">
              <label class="field"><span>含税金额</span><input v-model.number="form.amount_taxed" type="number" min="0" step="0.01" /></label>
              <label class="field"><span>不含税金额</span><input v-model.number="form.amount_untaxed" type="number" min="0" step="0.01" /></label>
              <label class="field"><span>税额</span><input v-model.number="form.tax_amount" type="number" min="0" step="0.01" /></label>
              <label class="field field-wide"><span>备注</span><textarea v-model="form.remark" rows="3"></textarea></label>
            </div>
          </div>

          <div class="editor-footer"><span class="muted">保存后会记录一条“编辑”操作记录</span><div class="form-actions"><button class="secondary-button" @click="cancelEditing">取消</button><button class="primary-button" :disabled="editBusy || !form.name.trim()" @click="saveEditing">{{ editBusy ? '保存中…' : '保存资料' }}</button></div></div>
        </div>
      </div>

      <template v-else>
        <div class="detail-grid">
          <div><label>资产分类</label><strong>{{ asset.category_path || '—' }}</strong></div><div><label>所在位置</label><strong>{{ asset.location || '—' }}</strong></div>
          <div><label>使用人</label><strong>{{ asset.assignee || '—' }}</strong></div><div><label>使用部门</label><strong>{{ asset.department || '—' }}</strong></div>
          <div><label>所属公司</label><strong>{{ asset.company || '—' }}</strong></div><div><label>型号</label><strong>{{ asset.model || '—' }}</strong></div>
          <div><label>序列号</label><strong>{{ asset.serial_number || '—' }}</strong></div><div><label>含税金额</label><strong>{{ Number(asset.amount_taxed || 0).toLocaleString('zh-CN', { style: 'currency', currency: 'CNY' }) }}</strong></div>
        </div>
        <div class="qr-raw"><label>二维码原文</label><code>{{ asset.qr_raw || '暂未记录' }}</code></div>
        <div class="drawer-actions">
          <button v-if="canWrite !== false && asset.status === '空闲'" class="primary-button" @click="showClaim = true">领用资产</button>
          <button v-if="canWrite !== false && (asset.status === '在用' || asset.status === '借出')" class="secondary-button" :disabled="busy" @click="emit('event', { type: asset.status === '借出' ? '借出归还' : '归还' })">归还资产</button>
        </div>
        <div class="timeline"><div class="section-title"><h3>流转记录</h3><span>{{ asset.events?.length ?? 0 }} 条</span></div><div v-if="!asset.events?.length" class="muted">暂无流转记录</div><div v-for="event in asset.events" :key="event.id" class="timeline-item"><span class="timeline-dot"></span><div><strong>{{ event.type }}</strong><span>{{ event.operator_name || '系统' }} · {{ formatSystemTime(event.created_at) }}</span><small v-if="event.note">{{ event.note }}</small></div></div></div>
      </template>

      <div v-if="showClaim" class="claim-form"><h3>确认领用</h3><input v-model="assignee" placeholder="使用人" /><input v-model="department" placeholder="使用部门" /><textarea v-model="note" placeholder="备注"></textarea><div class="form-actions"><button class="secondary-button" @click="showClaim = false">取消</button><button class="primary-button" @click="submitClaim">确认领用</button></div></div>
    </section>
  </div>
</template>

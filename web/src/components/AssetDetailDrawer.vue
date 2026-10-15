<script setup lang="ts">
import { formatSystemTime } from '../dateTime';
import { computed, nextTick, reactive, ref, watch } from 'vue';
import { availableFlowTypes, flowTitles, type FlowType } from '../../../shared/flows';
import type { Asset, AssetEvent, AssetUpdateInput } from '../types';
import { assetEditFields, assetOwnershipFields } from '../../../shared/assets';

const props = defineProps<{ asset: Asset | null; busy?: boolean; editBusy?: boolean; canWrite?: boolean; canApply?: boolean; employeeOnly?: boolean }>();
const emit = defineEmits<{
  close: [];
  event: [payload: { type: FlowType }];
  flow: [id: number];
  update: [payload: Partial<AssetUpdateInput>];
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
const expanded = ref(true);
const recordTab = ref('操作记录');
const moreDialog = ref<HTMLDialogElement | null>(null);
const showMore = ref(false);
const actions = computed(() => props.asset ? availableFlowTypes(props.asset.status).filter((type) => !props.employeeOnly || ['领用', '归还', '交接', '维修', '报失'].includes(type)) : []);
function eventChanges(event: AssetEvent) {
  if (event.changes?.length) return event.changes;
  return (['status', 'assignee', 'department', 'location'] as const).map((field) => ({ field, before: event[`from_${field}`] ?? '', after: event[`to_${field}`] ?? '' })).filter((c) => c.before !== c.after);
}
const fieldLabels: Record<string, string> = { status: '资产状态', name: '资产名称', category_level1: '一级分类', category_path: '资产分类', model: '型号', serial_number: '设备序列号', location: '所在位置', assignee: '使用人', department: '使用部门', company: '所属公司', cpu: 'CPU', memory: '内存', discrete_gpu: '独立显卡', gpu_model: '显卡型号', received_at: '领用日期', remark: '备注', photo_url: '照片', amount_taxed: '含税金额', amount_untaxed: '不含税金额', tax_amount: '税额' };
const records = computed(() => (props.asset?.events ?? []).filter((event) => recordTab.value === '维修记录' ? event.type === '维修' : recordTab.value === '盘点记录' ? event.type === '盘点' : recordTab.value === '其他记录' ? ['编辑', '导入'].includes(event.type) : !['维修', '盘点', '编辑', '导入'].includes(event.type)));
async function openMore() { showMore.value = true; await nextTick(); moreDialog.value?.showModal(); }
function launch(type: FlowType) { moreDialog.value?.close(); showMore.value = false; emit('event', { type }); }

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
  expanded.value = true; recordTab.value = '操作记录'; showMore.value = false;
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
  const changes = Object.fromEntries(assetEditFields.filter((field) => !(assetOwnershipFields as readonly string[]).includes(field) && String(form[field]) !== String(props.asset?.[field] ?? '')).map((field) => [field, form[field]]));
  emit('update', { ...changes, expectedVersion: props.asset.version });
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

      <div v-if="editing" class="drawer-status"><span class="status-pill" :class="`status-${asset.status}`">{{ asset.status }}</span><span>{{ asset.name || '未命名资产' }}</span><span class="readonly-hint">资产编码只读</span></div>

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
              <label class="field"><span>所在位置</span><input v-model="form.location" disabled /></label>
              <label class="field"><span>使用人</span><input v-model="form.assignee" disabled /></label>
              <label class="field"><span>使用部门</span><input v-model="form.department" disabled /></label>
              <label class="field"><span>领用日期</span><input disabled v-model="form.received_at" placeholder="YYYY-MM-DD" /></label>
              <label class="field readonly-field"><span>当前状态</span><input :value="asset.status" disabled /></label>
            </div>
            <p class="flow-state-message">使用人、部门和位置通过交接或调拨申请变更，审批通过后生效。</p>
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
        <article class="scan-info-card"><header><h3>{{ asset.name || '未命名资产' }}</h3><span class="status-pill" :class="`status-${asset.status}`">{{ asset.status }}</span></header>
          <dl class="scan-info-fields"><div><dt>资产编码</dt><dd>{{ asset.asset_code }}</dd></div><template v-if="expanded"><div><dt>资产分类</dt><dd>{{ asset.category_path || '—' }}</dd></div><div><dt>型号</dt><dd>{{ asset.model || '—' }}</dd></div><div><dt>设备序列号</dt><dd>{{ asset.serial_number || '—' }}</dd></div><div><dt>所在位置</dt><dd>{{ asset.location || '—' }}</dd></div><div><dt>使用部门</dt><dd>{{ asset.department || '—' }}</dd></div><div><dt>使用人</dt><dd>{{ asset.assignee || '—' }}</dd></div><div><dt>所属公司</dt><dd>{{ asset.company || '—' }}</dd></div></template></dl>
          <button class="text-button scan-expand" :aria-expanded="expanded" @click="expanded = !expanded">{{ expanded ? '收起 ∧' : '展开基本信息 ∨' }}</button>
        </article>
        <details class="scan-extra"><summary>更多资料与二维码</summary><dl class="scan-info-fields"><div><dt>含税金额</dt><dd>{{ Number(asset.amount_taxed || 0).toLocaleString('zh-CN', { style: 'currency', currency: 'CNY' }) }}</dd></div><div><dt>CPU / 内存</dt><dd>{{ asset.cpu || '—' }} / {{ asset.memory || '—' }}</dd></div><div><dt>领用日期</dt><dd>{{ asset.received_at || '—' }}</dd></div><div><dt>备注</dt><dd>{{ asset.remark || '—' }}</dd></div></dl><div class="qr-raw"><label>二维码原文</label><code>{{ asset.qr_raw || '暂未记录' }}</code></div></details>
        <article class="scan-history-card"><nav class="scan-record-tabs" aria-label="资产记录分类"><button v-for="label in ['操作记录', '维修记录', '盘点记录', '其他记录']" :key="label" :class="{ active: recordTab === label }" @click="recordTab = label">{{ label }}</button></nav>
          <p v-if="!records.length" class="empty-history">暂无{{ recordTab }}</p><section v-for="(event, index) in records" :key="event.id" class="scan-record"><span class="scan-record-number">第 {{ records.length - index }} 条</span><dl><div><dt>操作人</dt><dd>{{ event.operator_name || '系统' }}</dd></div><div><dt>操作类型</dt><dd>{{ event.type }}</dd></div><div><dt>操作时间</dt><dd>{{ formatSystemTime(event.created_at) }}</dd></div><div v-if="event.flow_id && event.document_no"><dt>关联单据</dt><dd><button class="text-button" @click="emit('flow', event.flow_id)">{{ event.document_no }} →</button></dd></div></dl>
            <div class="scan-changes"><p v-for="change in eventChanges(event)" :key="change.field">{{ fieldLabels[change.field] || change.field }}由“{{ change.before === '' ? '空' : change.before }}”变更为“{{ change.after === '' ? '空' : change.after }}”</p><p v-if="event.note" class="preserve-lines">{{ event.note }}</p></div>
          </section>
        </article>
        <footer v-if="canApply && actions.length" class="scan-bottom-actions"><button v-if="actions.includes('维修')" @click="launch('维修')"><span class="scan-action-symbol purple"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M14 6a5 5 0 0 0-6 6L3 17l4 4 5-5a5 5 0 0 0 6-6l-3 3-4-4 3-3Z" /></svg></span>故障报修</button><button v-if="actions.includes('交接')" @click="launch('交接')"><span class="scan-action-symbol orange"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M4 7h15m-4-4 4 4-4 4M20 17H5m4-4-4 4 4 4" /></svg></span>交接他人</button><button v-if="actions.includes('报失')" @click="launch('报失')"><span class="scan-action-symbol blue"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="10" cy="10" r="6" /><path d="m14 14 6 6M10 7v4m0 2v1" /></svg></span>资产报失</button><button @click="openMore"><span class="scan-action-symbol gray"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" /></svg></span>更多</button></footer>
        <p v-else-if="!canApply" class="scan-readonly">当前账号为只读，可查看资产信息和记录。</p>
      </template>
      <dialog v-if="showMore" ref="moreDialog" class="scan-more-dialog" aria-labelledby="scan-more-title" @cancel="showMore = false"><h3 id="scan-more-title">资产操作</h3><p class="muted">以下操作提交审批后生效。</p><div class="scan-more-grid"><button v-for="type in actions" :key="type" class="secondary-button" @click="launch(type)">{{ flowTitles[type] }}</button><button v-if="canWrite" class="secondary-button" @click="moreDialog?.close(); showMore = false; startEditing()">编辑资料</button></div><button class="secondary-button scan-more-cancel" @click="moreDialog?.close(); showMore = false">取消</button></dialog>
    </section>
  </div>
</template>

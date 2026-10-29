<script setup lang="ts">
import { formatSystemTime } from '../dateTime';
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { availableFlowTypes, type FlowType } from '../../shared/flows';
import type { Asset, AssetEvent, AssetUpdateInput } from '../types';
import { assetEditFields, assetOwnershipFields } from '../../shared/assets';
import { assetDepartmentLabel } from '../assetWorkspace';
import AppIcon from './AppIcon.vue';
import { defaultScanActions, normalizeScanActions, scanActionIcons, scanActionLabels, visibleScanActions } from '../assetScan';

const props = defineProps<{ asset: Asset | null; busy?: boolean; editBusy?: boolean; canWrite?: boolean; canApply?: boolean; employeeOnly?: boolean; allowedFlows?: FlowType[]; preferenceKey?: string }>();
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
const expanded = ref(false);
const recordTab = ref('操作记录');
const recordTabs = ['操作记录', '维修记录', '配件记录', '其他记录'];
const moreDialog = ref<HTMLDialogElement | null>(null);
const showMore = ref(false);
const configuring = ref(false);
const shortcuts = ref<FlowType[]>([...defaultScanActions]);
const shortcutDraft = ref<FlowType[]>([]);
const drawer = ref<HTMLElement | null>(null);
let previousFocus: HTMLElement | null = null;
let previousOverflow = '';
const actions = computed(() => props.allowedFlows ?? (props.asset ? availableFlowTypes(props.asset.status).filter((type) => !props.employeeOnly || ['领用', '归还', '交接', '维修', '报失'].includes(type)) : []));
const primaryActions = computed(() => visibleScanActions(shortcuts.value, actions.value));
const primaryActionSummary = computed(() => primaryActions.value.map(type => scanActionLabels[type]).join('、') || '未设置');
const employeeActions = computed(() => actions.value.filter(type => (props.employeeOnly ? ['领用', '归还', '交接', '维修', '报失'] : ['领用', '归还']).includes(type)));
const managerActions = computed(() => actions.value.filter(type => !employeeActions.value.includes(type)));
const managerActionLabels = { ...scanActionLabels, 交接: '变更领用人', 维修: '资产维修' };
watch(() => props.preferenceKey, () => {
  try { shortcuts.value = normalizeScanActions(JSON.parse(localStorage.getItem(`asset_scan_actions_${props.preferenceKey || 'local'}`) || 'null')); }
  catch { shortcuts.value = [...defaultScanActions]; }
}, { immediate: true });
function eventChanges(event: AssetEvent) {
  if (event.changes?.length) return event.changes;
  return (['status', 'assignee', 'department', 'location'] as const).map((field) => ({ field, before: event[`from_${field}`] ?? '', after: event[`to_${field}`] ?? '' })).filter((c) => c.before !== c.after);
}
const fieldLabels: Record<string, string> = { status: '资产状态', name: '资产名称', category_level1: '一级分类', category_path: '资产分类', model: '型号', serial_number: '设备序列号', location: '所在位置', assignee: '使用人', department: '使用部门', company: '所属公司', cpu: 'CPU', memory: '内存', discrete_gpu: '独立显卡', gpu_model: '显卡型号', received_at: '领用日期', remark: '备注', photo_url: '照片', amount_taxed: '含税金额', amount_untaxed: '不含税金额', tax_amount: '税额' };
const records = computed(() => (props.asset?.events ?? []).filter((event) => recordTab.value === '配件记录' ? false : recordTab.value === '维修记录' ? event.type === '维修' : recordTab.value === '其他记录' ? ['盘点', '编辑', '导入'].includes(event.type) : !['维修', '盘点', '编辑', '导入'].includes(event.type)));
function onRecordTabKey(event: KeyboardEvent, index: number) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
  event.preventDefault();
  const target = event.key === 'Home' ? 0 : event.key === 'End' ? recordTabs.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + recordTabs.length) % recordTabs.length;
  recordTab.value = recordTabs[target]!;
  document.getElementById(`record-tab-${target}`)?.focus();
}
async function openMore() { configuring.value = false; showMore.value = true; await nextTick(); if (showMore.value && !moreDialog.value?.open) moreDialog.value?.showModal(); }
function closeMore() { moreDialog.value?.close(); showMore.value = false; configuring.value = false; }
function launch(type: FlowType) {
  if (!props.canApply || !actions.value.includes(type)) return;
  closeMore(); emit('event', { type });
}
function configureShortcuts() { shortcutDraft.value = shortcuts.value.filter(type => actions.value.includes(type)); configuring.value = true; }
function toggleShortcut(type: FlowType) {
  if (shortcutDraft.value.includes(type)) shortcutDraft.value = shortcutDraft.value.filter(t => t !== type);
  else if (shortcutDraft.value.length < 3) shortcutDraft.value.push(type);
}
function saveShortcuts() {
  shortcuts.value = [...shortcutDraft.value];
  try { localStorage.setItem(`asset_scan_actions_${props.preferenceKey || 'local'}`, JSON.stringify(shortcuts.value)); } catch { /* Keep the preference for this session if storage is unavailable. */ }
  configuring.value = false;
}
function closeDrawer() { if (!props.editBusy) { closeMore(); emit('close'); } }
function onDrawerKeydown(event: KeyboardEvent) {
  if (showMore.value) return;
  if (event.key === 'Escape') { event.preventDefault(); closeDrawer(); }
  if (event.key !== 'Tab') return;
  const focusable = [...(drawer.value?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, [tabindex="0"]') ?? [])].filter(el => el.getClientRects().length && el.tabIndex !== -1);
  const first = focusable[0], last = focusable.at(-1);
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
}
function restorePage() { document.body.style.overflow = previousOverflow; previousFocus?.focus(); previousFocus = null; }
watch(() => !!props.asset, async (open) => {
  if (!open) { closeMore(); restorePage(); return; }
  previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  previousOverflow = document.body.style.overflow; document.body.style.overflow = 'hidden';
  await nextTick(); drawer.value?.querySelector<HTMLElement>('button')?.focus();
});
onBeforeUnmount(() => { if (props.asset) restorePage(); });

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
  expanded.value = false; recordTab.value = '操作记录'; closeMore();
}, { immediate: true });

function startEditing() {
  if (!props.asset || props.canWrite === false) return;
  closeMore();
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
  <div v-if="asset" class="drawer-backdrop" @click.self="closeDrawer">
    <section ref="drawer" class="drawer asset-info-drawer" :class="{ 'asset-detail-editing': editing }" role="dialog" aria-modal="true" aria-labelledby="asset-detail-title" @keydown="onDrawerKeydown">
      <header class="drawer-header asset-detail-header">
        <button class="icon-button asset-detail-back" aria-label="返回上一页" title="返回上一页" :disabled="editBusy" @click="closeDrawer"><AppIcon name="chevron" /></button>
        <h2 id="asset-detail-title">{{ editing ? '编辑资产资料' : '资产详情' }}</h2>
        <div class="drawer-header-actions"><button v-if="canWrite !== false" class="text-button edit-trigger" :disabled="editBusy" @click="editing ? cancelEditing() : startEditing()">{{ editing ? '退出编辑' : '编辑' }}</button></div>
      </header>

      <div class="asset-detail-scroll">
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
              <label class="field"><span>使用部门</span><input :value="assetDepartmentLabel(asset)" disabled /></label>
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
        <article class="scan-info-card">
          <header><h3>{{ asset.name || '未命名资产' }}</h3><span class="status-pill" :class="`status-${asset.status}`">{{ asset.status }}</span></header>
          <dl class="scan-info-fields">
            <div><dt>资产编码</dt><dd>{{ asset.asset_code }}</dd></div>
            <div><dt>资产分类</dt><dd>{{ asset.category_path || '—' }}</dd></div>
            <div><dt>型号</dt><dd>{{ asset.model || '—' }}</dd></div>
            <template v-if="expanded">
              <div><dt>设备序列号</dt><dd>{{ asset.serial_number || '—' }}</dd></div>
              <div><dt>所在位置</dt><dd>{{ asset.location || '—' }}</dd></div>
              <div><dt>使用部门</dt><dd>{{ assetDepartmentLabel(asset) || '—' }}</dd></div>
              <div><dt>使用人</dt><dd>{{ asset.assignee || '—' }}</dd></div>
              <div><dt>所属公司</dt><dd>{{ asset.company || '—' }}</dd></div>
              <div><dt>领用日期</dt><dd>{{ asset.received_at || '—' }}</dd></div>
            </template>
          </dl>
          <button class="text-button scan-expand" :aria-expanded="expanded" @click="expanded = !expanded">{{ expanded ? '收起' : '展开' }}<AppIcon name="chevron" :class="{ expanded }" :size="16" /></button>
        </article>
        <article class="scan-history-card">
          <nav class="scan-record-tabs" aria-label="资产记录分类" role="tablist">
            <button v-for="(label, index) in recordTabs" :id="`record-tab-${index}`" :key="label" role="tab" :tabindex="recordTab === label ? 0 : -1" :aria-selected="recordTab === label" aria-controls="scan-record-panel" :class="{ active: recordTab === label }" @keydown="onRecordTabKey($event, index)" @click="recordTab = label">{{ label }}</button>
          </nav>
          <div id="scan-record-panel" role="tabpanel" :aria-labelledby="`record-tab-${recordTabs.indexOf(recordTab)}`">
            <p v-if="recordTab === '其他记录'" class="scan-record-hint">包含盘点、资料编辑及导入记录</p>
            <p v-if="!records.length" class="empty-history">暂无{{ recordTab }}</p>
            <section v-for="(event, index) in records" :key="event.id" class="scan-record">
              <span class="scan-record-number">第 {{ records.length - index }} 条</span>
              <dl><div><dt>操作人</dt><dd>{{ event.operator_name || '系统' }}</dd></div><div><dt>操作类型</dt><dd>{{ event.type }}</dd></div><div><dt>操作时间</dt><dd>{{ formatSystemTime(event.created_at) }}</dd></div><div v-if="event.flow_id && event.document_no"><dt>关联单据</dt><dd><button class="text-button" @click="emit('flow', event.flow_id)">{{ event.document_no }} →</button></dd></div></dl>
              <div class="scan-changes"><p v-for="change in eventChanges(event)" :key="change.field">{{ fieldLabels[change.field] || change.field }}由“{{ change.before === '' ? '空' : change.before }}”变更为“{{ change.after === '' ? '空' : change.after }}”</p><p v-if="event.note" class="preserve-lines">{{ event.note }}</p></div>
            </section>
          </div>
        </article>
        <details class="scan-extra"><summary>更多资料与二维码</summary><dl class="scan-info-fields"><div><dt>含税金额</dt><dd>{{ Number(asset.amount_taxed || 0).toLocaleString('zh-CN', { style: 'currency', currency: 'CNY' }) }}</dd></div><div><dt>CPU / 内存</dt><dd>{{ asset.cpu || '—' }} / {{ asset.memory || '—' }}</dd></div><div><dt>备注</dt><dd>{{ asset.remark || '—' }}</dd></div></dl><div class="qr-raw"><label>二维码原文</label><code>{{ asset.qr_raw || '暂未记录' }}</code></div></details>
      </template>
      </div>
      <footer v-if="!editing && canApply && actions.length" class="scan-bottom-actions" aria-label="资产常用操作">
        <button v-for="type in primaryActions" :key="type" @click="launch(type)"><span class="scan-action-symbol" :class="scanActionIcons[type].tone"><AppIcon :name="scanActionIcons[type].name" :size="24" /></span>{{ scanActionLabels[type] }}</button>
        <button aria-haspopup="dialog" @click="openMore"><span class="scan-action-symbol gray"><AppIcon name="grid" :size="24" /></span>更多</button>
      </footer>
      <p v-else-if="!editing" class="scan-readonly">当前资产可查看信息和记录，暂无可发起的流转操作。</p>
      <dialog v-if="showMore" ref="moreDialog" class="scan-more-dialog" aria-labelledby="scan-more-title" @cancel.prevent="closeMore" @click="($event.target === moreDialog) && closeMore()">
        <template v-if="configuring">
          <header class="scan-sheet-heading"><button class="text-button" @click="configuring = false">返回</button><h3 id="scan-more-title">设置外显操作项</h3><button class="text-button" @click="saveShortcuts">完成</button></header>
          <p class="scan-sheet-note">最多选择 3 项，保存在当前设备。其他操作可从“更多”进入。</p>
          <div class="scan-shortcut-options"><label v-for="type in actions" :key="type"><input type="checkbox" :checked="shortcutDraft.includes(type)" :disabled="!shortcutDraft.includes(type) && shortcutDraft.length >= 3" @change="toggleShortcut(type)" /><span>{{ scanActionLabels[type] }}</span></label></div>
        </template>
        <template v-else>
          <h3 id="scan-more-title" class="sr-only">资产操作</h3>
          <button class="scan-shortcut-setting" @click="configureShortcuts"><span>设置外显操作项</span><span>{{ primaryActionSummary }}<AppIcon name="chevron" :size="16" /></span></button>
          <section v-if="employeeActions.length" class="scan-action-group"><h4>员工操作</h4><div class="scan-more-grid"><button v-for="type in employeeActions" :key="type" @click="launch(type)"><span class="scan-action-symbol" :class="scanActionIcons[type].tone"><AppIcon :name="scanActionIcons[type].name" :size="24" /></span>{{ scanActionLabels[type] }}</button></div></section>
          <section v-if="canWrite || managerActions.length" class="scan-action-group"><h4>管理员操作</h4><div class="scan-more-grid"><button v-if="canWrite" @click="startEditing"><span class="scan-action-symbol coral"><AppIcon name="edit" :size="24" /></span>编辑资料</button><button v-for="type in managerActions" :key="type" @click="launch(type)"><span class="scan-action-symbol" :class="scanActionIcons[type].tone"><AppIcon :name="scanActionIcons[type].name" :size="24" /></span>{{ managerActionLabels[type] }}</button></div></section>
          <p class="scan-sheet-note">可用操作随资产状态和账号权限变化，流转申请经审批后生效。</p>
        </template>
        <button class="scan-more-cancel" @click="closeMore">取消</button>
      </dialog>
    </section>
  </div>
</template>

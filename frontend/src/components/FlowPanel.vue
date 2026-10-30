<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue';
import { api } from '../api';
import type { Asset, AuthUser } from '../types';
import { availableFlowTypes, flowStatusLabels, flowTitles, flowTypes, type Flow, type FlowDetail, type FlowInput, type FlowType } from '../../shared/flows';
import type { Employee } from '../directory';
import { formatSystemTime } from '../dateTime';
import EmployeePicker from './EmployeePicker.vue';
import RequisitionAllocation from './RequisitionAllocation.vue';
import ExportButton from './ExportButton.vue';
import { allowedAssetFlows, type AssetFlowDraft } from '../assetWorkspace';

const props = defineProps<{ user: AuthUser; canWrite: boolean; draft?: AssetFlowDraft | null; initialId?: number | null }>();
const emit = defineEmits<{ changed: []; success: [message: string]; settings: [];requisition:[] }>();
const employeeOnly = computed(() => ['employee', 'approver'].includes(props.user.role));
const employees = ref<Employee[]>([]);
const scope = ref('all'); const status = ref(''); const search = ref(''); const page = ref(1);
const exportPath = computed(() => `/api/exports/flows?${new URLSearchParams({ scope: scope.value, status: status.value, search: search.value })}`);
const flows = ref<Flow[]>([]); const total = ref(0); const loading = ref(false); const busy = ref(false); const error = ref('');
const fixedApprover=ref<AuthUser|null>(null); const recoveryInstance=ref('');
const detail = ref<FlowDetail | null>(null); const approvers = ref<AuthUser[]>([]); const opinion = ref(''); const comment = ref('');
const dialog = ref<HTMLDialogElement | null>(null); const creating = ref(false); const draftAssets = ref<Asset[]>([]);
const assetSearch = ref(''); const candidates = ref<Asset[]>([]); const assetPage = ref(1); const assetTotal = ref(0); const assetLoading = ref(false);
let listSequence = 0; let assetSequence = 0; let detailSequence = 0;
const form = reactive<FlowInput>({ type: '领用', assetCodes: [], applicantDepartment: '', approverIds: [0], toAssignee: '', toDepartment: '', toLocation: '', note: '', requestId: '' });
const selectableApprovers = computed(() => approvers.value.filter((a) => a.id !== props.user.id || !!fixedApprover.value));
const allowedTypes = computed(() => draftAssets.value.length ? allowedAssetFlows(draftAssets.value, props.user) : flowTypes.filter(type => !employeeOnly.value || ['归还', '交接', '维修', '报失'].includes(type)));
const currentStep = computed(() => detail.value?.steps.find((s) => s.status === 'pending'));
const canApprove = computed(() => ['admin', 'asset_manager', 'approver'].includes(props.user.role) && !detail.value?.oa && detail.value?.flow.status === 'pending' && currentStep.value?.approver_id === props.user.id);
const canWithdraw = computed(() => props.canWrite && ['pending','approved'].includes(detail.value?.flow.status||'') && detail.value?.flow.applicant_id === props.user.id);
const logLabels = { submitted: '提交申请', approved: '审批通过', rejected: '驳回申请', withdrawn: '撤回申请', completed: '流程完结',allocated:'分配设备' };
const stepLabels = { waiting: '等待前序审批', pending: '待审批', approved: '已通过', rejected: '已驳回', cancelled: '已取消' };
const notificationLabels = { approval_pending: '审批待办',approved:'批准结果', completed: '完结结果', rejected: '驳回结果', withdrawn: '撤回结果' };
async function bindOA(){if(!detail.value||!recoveryInstance.value.trim())return;busy.value=true;error.value='';try{detail.value=await api.bindOA(detail.value.flow.id,recoveryInstance.value.trim());recoveryInstance.value='';emit('changed')}catch(e){showError(e)}finally{busy.value=false}}
const notificationStatuses = { pending: '待推送',accepted:'钉钉已受理', sent: '已发送', failed: '发送失败', cancelled: '已取消' };
function showError(err: unknown) { error.value = err instanceof Error ? err.message : '操作失败，请重试'; }
async function loadList() {
  const sequence = ++listSequence; loading.value = true;
  try { const result = await api.listFlows({ scope: scope.value, status: status.value, search: search.value, page: page.value }); if (sequence === listSequence) { flows.value = result.items; total.value = result.total; } }
  catch (err) { if (sequence === listSequence) showError(err); } finally { if (sequence === listSequence) loading.value = false; }
}
function filterList() { page.value = 1; void loadList(); }
async function openFlow(id: number) {
  const sequence = ++detailSequence; error.value = ''; loading.value = true;
  try { const result = await api.getFlow(id); if (sequence !== detailSequence) return; detail.value = result; opinion.value = ''; comment.value = ''; updateUrl(id); }
  catch (err) { if (sequence === detailSequence) showError(err); } finally { if (sequence === detailSequence) loading.value = false; }
}
function updateUrl(id?: number) { const url = new URL(window.location.href); if (id) url.searchParams.set('flow', String(id)); else url.searchParams.delete('flow'); window.history.replaceState(null, '', url); }
function closeDetail() { detailSequence++; detail.value = null; loading.value = false; error.value = ''; updateUrl(); void loadList(); }
async function loadCandidates() {
  const sequence = ++assetSequence; assetLoading.value = true;
  try { const result = await api.listAssets({ search: assetSearch.value, page: assetPage.value, pageSize: 10, scope: employeeOnly.value ? 'mine' : '' }); if (sequence === assetSequence) { candidates.value = result.items; assetTotal.value = result.total; } }
  catch (err) { if (sequence === assetSequence) showError(err); } finally { if (sequence === assetSequence) assetLoading.value = false; }
}
async function startDraft(assets: Asset[] = [], type?: FlowType) {
  const asset = assets[0];
  error.value = ''; draftAssets.value = [...assets];
  Object.assign(form, { type: type ?? (asset ? (allowedAssetFlows([asset], props.user)[0] ?? '归还') : employeeOnly.value ? '归还' : '领用'), applicantDepartment: props.user.department || asset?.department || '', approverIds: [0], toAssignee: '', toEmployeeId: null, toDepartment: '', toLocation: '', note: '', requestId: crypto.randomUUID() });
  assetSearch.value = ''; assetPage.value = 1; creating.value = true; await nextTick(); dialog.value?.showModal();
  try { const options = await api.flowOptions(); approvers.value = options.approvers; fixedApprover.value=options.fixedApprover||null; if(fixedApprover.value)form.approverIds=[fixedApprover.value.id]; employees.value = options.employees; if (employeeOnly.value && form.type === '领用') { form.toEmployeeId = props.user.employee_id; selectEmployee(); } } catch (err) { showError(err); }
  void loadCandidates();
}
function addAsset(asset: Asset) {
  if (draftAssets.value.some((a) => a.asset_code === asset.asset_code) || draftAssets.value.length >= 100) return;
  draftAssets.value.push(asset);
  if (!allowedTypes.value.includes(form.type)) form.type = allowedTypes.value[0] ?? '领用';
}
function cancelDraft(event?: Event) { if (busy.value) { event?.preventDefault(); return; } dialog.value?.close(); creating.value = false; error.value = ''; }
async function submit() {
  if (!draftAssets.value.length || !allowedTypes.value.includes(form.type)) { error.value = '请选择状态适合本次操作的资产'; return; }
  if (['领用','借出','交接'].includes(form.type) && !form.toEmployeeId) { error.value = '请从公司通讯录选择使用人'; return; }
  if (form.approverIds.some((id) => !id) || new Set(form.approverIds).size !== form.approverIds.length) { error.value = '请按顺序指定审批人，审批人不能重复'; return; }
  busy.value = true; error.value = '';
  try {
    const result = await api.createFlow({ ...form, assetCodes: draftAssets.value.map((a) => a.asset_code) }); detail.value = result; updateUrl(result.flow.id);
    dialog.value?.close(); creating.value = false; emit('success', '申请已提交，审批通过后生效'); await loadList();
  } catch (err) { showError(err); } finally { busy.value = false; }
}
async function act(action: 'approve' | 'reject' | 'withdraw') {
  if (!detail.value || busy.value) return; busy.value = true; error.value = '';
  try { detail.value = await api.actFlow(detail.value.flow.id, action, opinion.value); opinion.value = ''; emit('changed'); emit('success', action === 'approve' ? (detail.value.flow.status === 'completed' ? '审批完成，资产台账已更新' : detail.value.flow.status==='approved'?'需求已批准，等待分配设备':'已通过，等待下一位审批人') : action === 'reject' ? '申请已驳回' : '申请已撤回'); await loadList(); }
  catch (err) { showError(err); } finally { busy.value = false; }
}
async function addComment() {
  if (!detail.value || !comment.value.trim() || busy.value) return; busy.value = true; error.value = '';
  try { detail.value = await api.commentFlow(detail.value.flow.id, comment.value); comment.value = ''; } catch (err) { showError(err); } finally { busy.value = false; }
}
async function syncOA(){if(!detail.value||busy.value)return;busy.value=true;try{detail.value=await api.syncOA(detail.value.flow.id);emit('changed')}catch(e){showError(e)}finally{busy.value=false}}
async function retryOA(){if(!detail.value||busy.value)return;busy.value=true;try{await api.retryOA(detail.value.flow.id);detail.value=await api.getFlow(detail.value.flow.id)}catch(e){showError(e)}finally{busy.value=false}}
function allocationSaved(value:FlowDetail){detail.value=value;emit('changed');emit('success',value.flow.status==='completed'?'设备已分配，资产台账已更新':'设备分配已保存')}
function selectEmployee() { const e = employees.value.find((e) => e.id === form.toEmployeeId); form.toAssignee = e?.name ?? ''; form.toDepartment = e?.department ?? ''; }
watch(() => form.type, () => { if (!creating.value) return; if (employeeOnly.value && form.type === '领用') { form.toEmployeeId = props.user.employee_id; selectEmployee(); } assetPage.value = 1; void loadCandidates(); });
watch(() => props.draft, (draft) => { if (draft) void startDraft(draft.assets, draft.type); });
watch(() => props.initialId, (id) => { if (id) void openFlow(id); });
onMounted(() => { void loadList(); if (props.initialId) void openFlow(props.initialId); if (props.draft) void startDraft(props.draft.assets, props.draft.type); });
</script>

<template>
  <div class="flow-workspace">
    <div v-if="error && !creating" class="scan-error" role="alert">{{ error }}</div>
    <article v-if="!detail" class="panel flow-list-panel">
      <div class="panel-header"><div><span class="eyebrow">申请与审批</span><h2>资产流转记录</h2></div><div class="export-actions"><ExportButton :path="exportPath" :disabled="loading" @success="emit('success', $event)" @error="(message) => { error = message; }" /><button v-if="canWrite" class="primary-button" @click="emit('requisition')">申请领用</button><button v-if="canWrite" class="secondary-button" @click="startDraft()">其他资产申请</button></div></div>
      <p class="export-note">导出包含筛选后的全部单据、领用需求、资产明细、审批步骤和处理记录。</p><nav class="flow-tabs" aria-label="流转范围"><button v-for="item in [{ id: 'all', label: employeeOnly ? '与我相关' : '全部单据' }, { id: 'todo', label: '我的待办' }, { id: 'mine', label: '我发起的' }]" :key="item.id" :class="{ active: scope === item.id }" @click="scope = item.id; filterList()">{{ item.label }}</button></nav>
      <form class="flow-filters" @submit.prevent="filterList"><label class="field"><span>搜索单据或资产</span><input v-model="search" placeholder="单号、申请人、资产编码或名称" /></label><label class="field"><span>审批状态</span><select v-model="status" @change="filterList"><option value="">全部状态</option><option v-for="(label, key) in flowStatusLabels" :key="key" :value="key">{{ label }}</option></select></label><button class="secondary-button" :disabled="loading">{{ loading ? '加载中…' : '搜索' }}</button></form>
      <p v-if="loading" class="muted" role="status">正在加载单据…</p>
      <div v-else-if="!flows.length" class="empty-history">暂无单据{{ scope === 'todo' ? '待你审批' : '' }}<p>发起申请后，可在这里查看审批进度和处理结果。</p></div>
      <button v-for="flow in flows" v-else :key="flow.id" class="flow-list-row" @click="openFlow(flow.id)"><div class="flow-row-main"><strong>{{ flow.applicant_name }}的{{ flowTitles[flow.type] }}</strong><span>{{ flow.document_no }}</span></div><div class="flow-row-meta"><span>{{ flow.asset_count ? flow.asset_count+' 件资产':'待分配设备' }} · {{ flow.applicant_department }}</span><span>{{ formatSystemTime(flow.created_at) }}</span></div><span class="flow-status" :class="flow.status">{{ flowStatusLabels[flow.status] }}</span></button>
      <div class="pager"><span>共 {{ total }} 张单据</span><div><button :disabled="loading || page <= 1" @click="page--; loadList()">上一页</button><span>{{ page }} / {{ Math.max(1, Math.ceil(total / 20)) }}</span><button :disabled="loading || page * 20 >= total" @click="page++; loadList()">下一页</button></div></div>
    </article>
    <template v-else>
      <div class="flow-detail-toolbar"><button class="secondary-button" :disabled="busy" @click="closeDetail">← 返回流转记录</button><button class="text-button" :disabled="busy || loading" @click="openFlow(detail.flow.id)">刷新单据</button></div>
      <article class="panel flow-document">
        <header class="flow-document-header"><div><span class="eyebrow">{{ detail.flow.document_no }}</span><h2>{{ detail.flow.applicant_name }}的{{ flowTitles[detail.flow.type] }}</h2><p>{{ detail.flow.applicant_department }} · {{ formatSystemTime(detail.flow.created_at) }}</p></div><span class="flow-status large" :class="detail.flow.status">{{ flowStatusLabels[detail.flow.status] }}</span></header>
        <p class="flow-state-message">{{ detail.flow.status === 'pending' ? `等待 ${currentStep?.approver_name ?? '指定审批人'} 审批，资产台账尚未变更` : detail.flow.status==='approved'?'需求已批准，等待管理员分配具体设备':detail.flow.status === 'completed' ? '全部审批已通过，资产台账已更新' : '流程已结束，资产台账未因本次申请变更' }}</p>
        <div v-if="detail.oa" class="oa-status"><strong>钉钉原生审批</strong><p>{{detail.oa.state==='queued'?'正在准备发送钉钉审批':detail.oa.state==='active'?'请在钉钉审批中处理，结果约30秒同步':detail.oa.state==='done'?'钉钉结果已同步':detail.oa.state==='creating'?'正在创建审批':detail.oa.state==='uncertain'?'创建结果待核对，请勿重复发起':'钉钉审批发送失败'}}</p><p v-if="detail.oa.last_error" class="scan-error">{{detail.oa.last_error}}</p><form v-if="user.role==='admin' && detail.oa.state==='uncertain'" class="oa-recovery" @submit.prevent="bindOA"><label class="field"><span>核对已创建的钉钉审批实例 ID</span><input v-model="recoveryInstance" maxlength="200" :disabled="busy" required placeholder="从钉钉审批链接中的 procInsId 获取"/></label><button class="secondary-button" :disabled="busy||!recoveryInstance.trim()">核对并关联</button></form><div class="export-actions"><button class="secondary-button" :disabled="busy" @click="syncOA">同步钉钉结果</button><button v-if="user.role==='admin' && detail.oa.state==='failed'" class="secondary-button" :disabled="busy" @click="retryOA">重试发送</button></div></div>
        <div class="flow-document-grid">
          <section class="flow-document-main"><h3>申请信息</h3><dl class="flow-fields"><div><dt>申请人</dt><dd>{{ detail.flow.applicant_name }}</dd></div><div><dt>申请部门</dt><dd>{{ detail.flow.applicant_department }}</dd></div><div v-if="detail.flow.to_assignee"><dt>目标使用人</dt><dd>{{ detail.flow.to_assignee }}</dd></div><div v-if="detail.flow.to_department"><dt>目标部门</dt><dd>{{ detail.flow.to_department }}</dd></div><div v-if="detail.flow.to_location"><dt>目标位置</dt><dd>{{ detail.flow.to_location }}</dd></div><div v-if="detail.flow.note" class="wide"><dt>申请说明</dt><dd class="preserve-lines">{{ detail.flow.note }}</dd></div></dl>
            <template v-if="detail.requisition"><h3>领用需求</h3><dl class="flow-fields"><div><dt>设备类别</dt><dd>{{detail.requisition.category_path}}</dd></div><div><dt>申请数量</dt><dd>{{detail.requisition.quantity}} 件</dd></div><div><dt>需求日期</dt><dd>{{detail.requisition.needed_date}}</dd></div></dl><RequisitionAllocation v-if="user.role==='admin' && detail.steps.some(s=>s.approver_id===user.id) && ['pending','approved'].includes(detail.flow.status)" :key="detail.requisition.version" :detail="detail" @saved="allocationSaved"/></template>
            <h3>资产列表 <small>{{ detail.items.length }} 件</small></h3><p v-if="detail.requisition && !detail.items.length" class="muted">尚未分配设备，审批和分配完成后才会显示在本人名下。</p><div v-for="item in detail.items" :key="item.id" class="flow-asset-card"><strong>{{ item.asset_name }} <code>{{ item.asset_code }}</code></strong><span>{{ item.category_path || '未分类' }} · {{ item.model || '无型号' }}</span><span>{{ item.expected_status }} · {{ item.expected_location || '未设置位置' }} · {{ item.expected_assignee || '无使用人' }} · {{ item.expected_department || '未设置部门' }}</span></div>
            <h3>处理记录</h3><div v-for="entry in detail.logs" :key="entry.id" class="timeline-item"><span class="timeline-dot"></span><div><strong>{{ entry.actor_name }} · {{ logLabels[entry.action] }}</strong><span>{{ formatSystemTime(entry.created_at) }}</span><small class="preserve-lines" v-if="entry.note">{{ entry.note }}</small></div></div>
          </section>
          <aside class="flow-document-side"><h3>审批进度</h3><div class="flow-step submitted"><span class="flow-step-number">始</span><div><strong>{{ detail.flow.applicant_name }}</strong><span>发起申请</span><small>{{ formatSystemTime(detail.flow.created_at) }}</small></div></div><div v-for="step in detail.steps" :key="step.id" class="flow-step" :class="step.status"><span class="flow-step-number">{{ step.sequence }}</span><div><strong>{{ step.approver_name }}</strong><span>{{ stepLabels[step.status] }}</span><small v-if="step.acted_at">{{ formatSystemTime(step.acted_at) }}</small><small v-if="step.comment" class="preserve-lines">{{ step.comment }}</small></div></div><div v-if="detail.flow.completed_at" class="flow-step"><span class="flow-step-number">终</span><div><strong>{{ flowStatusLabels[detail.flow.status] }}</strong><small>{{ formatSystemTime(detail.flow.completed_at) }}</small></div></div></aside>
        </div>
        <section v-if="canApprove || canWithdraw" class="flow-action-section"><label class="field"><span>{{ canApprove ? '审批意见（驳回时必填）' : '撤回说明（可选）' }}</span><textarea v-model="opinion" rows="3" maxlength="2000" :disabled="busy" /></label><div class="form-actions"><button v-if="canWithdraw" class="secondary-button" :disabled="busy" @click="act('withdraw')">撤回申请</button><template v-if="canApprove"><button class="secondary-button danger-button" :disabled="busy || !opinion.trim()" @click="act('reject')">驳回</button><button class="primary-button" :disabled="busy" @click="act('approve')">{{ busy ? '处理中…' : '同意申请' }}</button></template></div><p class="muted" v-if="canApprove">同意后进入下一审批节点；最后一位审批人通过后更新全部资产。</p></section>
        <section class="flow-comments"><h3>评论 <small>{{ detail.comments.length }} 条</small></h3><p v-if="!detail.comments.length" class="muted">暂无评论</p><div v-for="entry in detail.comments" :key="entry.id" class="flow-comment"><strong>{{ entry.user_name }}</strong><time>{{ formatSystemTime(entry.created_at) }}</time><p class="preserve-lines">{{ entry.content }}</p></div><form v-if="canWrite" @submit.prevent="addComment"><label class="field"><span>添加评论</span><textarea v-model="comment" rows="2" maxlength="2000" :disabled="busy" placeholder="补充说明或沟通处理情况" /></label><button class="secondary-button" :disabled="busy || !comment.trim()">发表评论</button></form></section>
        <details class="flow-notifications"><summary>钉钉通知记录 · {{ detail.notifications.filter((n) => n.status === 'pending').length }} 条待推送</summary><p>新申请通过钉钉原生审批处理；结果通知仅发送给申请人。“钉钉已受理”表示仍在确认投递，刷新可查看发送结果。</p><div v-for="entry in detail.notifications" :key="entry.id"><span>{{ notificationLabels[entry.kind] }} → {{ entry.recipient_name }}</span><span>{{ notificationStatuses[entry.status] }}</span></div></details>
      </article>
    </template>
    <dialog v-if="creating" ref="dialog" class="flow-create-dialog" aria-labelledby="flow-create-title" @cancel="cancelDraft">
      <header class="panel-header"><div><span class="eyebrow">资产申请与办理</span><h2 id="flow-create-title">发起资产流转申请</h2></div><button class="icon-button" aria-label="关闭申请" :disabled="busy" @click="cancelDraft()">×</button></header>
      <p class="flow-state-message">员工申请由固定资产管理员审批后生效；管理员本人发起的资产管理操作直接办理并留痕。</p>
      <div v-if="error" class="scan-error" role="alert">{{ error }}</div>
      <form @submit.prevent="submit"><fieldset :disabled="busy" class="flow-fieldset">
        <div class="form-grid"><label class="field"><span>流转类型 *</span><select v-model="form.type" required><option v-for="type in allowedTypes" :key="type" :value="type">{{ flowTitles[type] }}</option></select></label><label class="field"><span>申请部门 *</span><input v-model="form.applicantDepartment" :readonly="employeeOnly" required maxlength="200" /></label><template v-if="['领用', '借出', '交接'].includes(form.type)"><div class="field"><span>目标使用人 *</span><EmployeePicker v-model="form.toEmployeeId" :employees="employees" required :disabled="employeeOnly && form.type === '领用'" @update:model-value="selectEmployee" /><p v-if="!employees.length" class="directory-notice">尚未同步公司人员，请联系管理员同步钉钉通讯录。</p></div><label class="field"><span>目标使用部门 *</span><input v-model="form.toDepartment" readonly required maxlength="200" /></label></template><label v-if="form.type === '调拨'" class="field"><span>目标部门</span><input v-model="form.toDepartment" maxlength="200" /></label><label class="field"><span>{{ ['归还', '借出归还'].includes(form.type) ? '归还位置（可选）' : '目标位置（可选）' }}</span><input v-model="form.toLocation" maxlength="200" /></label><label class="field field-wide"><span>申请说明{{ ['维修', '报失', '报废'].includes(form.type) ? ' *' : '（可选）' }}</span><textarea v-model="form.note" rows="3" maxlength="2000" :required="['维修', '报失', '报废'].includes(form.type)" /></label></div>
        <section class="flow-picker"><h3>申请资产 <small>已选 {{ draftAssets.length }} / 100 件</small></h3><div class="flow-selected-assets"><div v-for="asset in draftAssets" :key="asset.id"><span>{{ asset.asset_code }} · {{ asset.name }} · {{ asset.status }}</span><button type="button" class="text-button" :aria-label="`移除 ${asset.asset_code}`" @click="draftAssets = draftAssets.filter((a) => a.id !== asset.id)">移除</button></div></div><p v-if="draftAssets.length && !allowedTypes.length" class="scan-error">所选资产状态不兼容，请分开申请。</p><div class="flow-picker-search"><label class="field"><span>搜索添加资产</span><input v-model="assetSearch" placeholder="资产编码或名称" @keydown.enter.prevent="assetPage = 1; loadCandidates()" /></label><button type="button" class="secondary-button" :disabled="assetLoading" @click="assetPage = 1; loadCandidates()">搜索</button></div><p v-if="assetLoading" class="muted">正在查询资产…</p><p v-else-if="!candidates.length" class="muted">未找到资产</p><div v-for="asset in candidates" :key="asset.id" class="flow-candidate"><span><strong>{{ asset.asset_code }} · {{ asset.name }}</strong><small>{{ asset.status }} · {{ asset.location || '未设置位置' }}</small></span><button type="button" class="secondary-button" :disabled="draftAssets.some((a) => a.id === asset.id) || !availableFlowTypes(asset.status).includes(form.type) || draftAssets.length >= 100" @click="addAsset(asset)">{{ draftAssets.some((a) => a.id === asset.id) ? '已添加' : '添加' }}</button></div><div class="pager"><span>共 {{ assetTotal }} 件</span><div><button type="button" :disabled="assetLoading || assetPage <= 1" @click="assetPage--; loadCandidates()">上一页</button><span>{{ assetPage }}</span><button type="button" :disabled="assetLoading || assetPage * 10 >= assetTotal" @click="assetPage++; loadCandidates()">下一页</button></div></div></section>
        <div v-if="fixedApprover" class="requisition-summary"><span>固定审批人</span><strong>{{fixedApprover.display_name}} · 资产管理员</strong><p>提交后由资产管理员处理，无需选择部门领导。</p></div><section v-else class="flow-approvers"><h3>顺序审批人</h3><p class="muted">按下列顺序逐人审批；每位审批人都通过后生效。</p><div v-if="!selectableApprovers.length" class="scan-error">还没有其他审批账号，请联系管理员配置审批人。<button type="button" class="text-button" @click="cancelDraft(); emit('settings')">前往系统设置</button></div><div v-for="(_, index) in form.approverIds" :key="index" class="flow-approver-row"><label class="field"><span>第 {{ index + 1 }} 位审批人 *</span><select v-model.number="form.approverIds[index]" required><option :value="0" disabled>请选择审批人</option><option v-for="a in selectableApprovers" :key="a.id" :value="a.id" :disabled="form.approverIds.includes(a.id) && form.approverIds[index] !== a.id">{{ a.display_name }}（{{ a.username }}）</option></select></label><button v-if="form.approverIds.length > 1" type="button" class="text-button" @click="form.approverIds.splice(index, 1)">移除</button></div><button type="button" class="secondary-button" :disabled="form.approverIds.length >= Math.min(10, selectableApprovers.length)" @click="form.approverIds.push(0)">添加下一位审批人</button></section>
      </fieldset><footer class="form-actions flow-create-footer"><button type="button" class="secondary-button" :disabled="busy" @click="cancelDraft()">取消</button><button class="primary-button" :disabled="busy || !draftAssets.length || !selectableApprovers.length || !allowedTypes.length">{{ busy ? '提交中…' : '提交申请' }}</button></footer></form>
    </dialog>
  </div>
</template>

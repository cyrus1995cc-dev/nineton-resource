<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import LegacyAssetMappings from './LegacyAssetMappings.vue';
import { api } from '../api';
import type { Company, DirectoryStatus, Employee, EmployeeMapping } from '../directory';
import { roleLabels } from '../directory';
import { formatSystemTime } from '../dateTime';
import { descendantIds, treeRows } from '../organization';
const emit = defineEmits<{ changed: [] }>();
const status = ref<DirectoryStatus | null>(null); const employees = ref<Employee[]>([]); const mappings = ref<EmployeeMapping[]>([]);
const busy = ref(false); const error = ref(''); const message = ref(''); const search = ref(''); const tab = ref('employees');
const roots = ref('1'); const interval = ref(0); const page = ref(1);
const companies = ref<Company[]>([]); const companyFilter = ref(0); const departmentFilter = ref(0); const defaultCompany = ref(0);
let pollTimer: ReturnType<typeof setTimeout> | undefined; let disposed = false; let observedRun = 0;
const syncing = computed(() => !!status.value?.syncing);
const departments = computed(() => status.value?.departments.filter(d => d.corp_id === status.value?.corpId) ?? []);
const departmentTree = computed(() => treeRows(departments.value, d => d.dingtalk_id, d => d.parent_id));
const departmentScope = computed(() => departmentFilter.value ? descendantIds(departments.value, departmentFilter.value, d => d.dingtalk_id, d => d.parent_id) : null);
const statusReason = ref('');
const filtered = computed(() => employees.value.filter(e => `${e.name} ${e.company} ${e.department} ${e.job_number} ${e.title}`.toLowerCase().includes(search.value.trim().toLowerCase()) && (!companyFilter.value || e.memberships?.some(m => m.company_id === companyFilter.value)) && (!departmentScope.value || e.memberships?.some(m => departmentScope.value!.has(m.dingtalk_id)))));
const shown = computed(() => filtered.value.slice((page.value - 1) * 20, page.value * 20));
async function load() {
  const [s, e, m, c] = await Promise.all([api.directoryStatus(), api.listEmployees(true), api.employeeMappings(), api.listCompanies()]);
  if (disposed) return;
  status.value = s; employees.value = e.employees; mappings.value = m.assets; companies.value = c.companies; roots.value = s.rootDepartmentIds.join(','); interval.value = s.intervalMinutes; defaultCompany.value = s.defaultCompanyId;
  page.value = Math.min(page.value, Math.max(1, Math.ceil(filtered.value.length / 20)));
  schedulePoll();
}
async function action(fn: () => Promise<void>) { busy.value = true; error.value = ''; message.value = ''; try { await fn(); } catch (err) { error.value = err instanceof Error ? err.message : '操作失败'; } finally { busy.value = false; } }
function schedulePoll() {
  if (pollTimer) clearTimeout(pollTimer);
  if (!syncing.value || disposed) return;
  observedRun = status.value?.runs[0]?.id ?? 0;
  pollTimer = setTimeout(async () => {
    try {
      const s = await api.directoryStatus(); if (disposed) return; status.value = s;
      if (!s.syncing) {
        const run = s.runs.find(r => r.id === observedRun);
        await load(); emit('changed');
        if (run?.status === 'completed') message.value = `同步完成：${run.employees_count} 位员工，${run.departments_count} 个部门`;
        else error.value = run?.message || '同步未完成，请查看同步记录';
      } else schedulePoll();
    } catch (e) { error.value = e instanceof Error ? e.message : '同步状态查询失败'; schedulePoll(); }
  }, 2500);
}
async function sync() { await action(async () => {
  const job = await api.startDirectorySync(); await load();
  const run = status.value?.runs.find(r => r.id === job.runId);
  if (run?.status === 'completed') { message.value = `同步完成：${run.employees_count} 位员工，${run.departments_count} 个部门`; emit('changed'); }
  else if (run?.status === 'failed') error.value = run.message || '同步失败，请查看同步记录';
  else message.value = '同步任务已开始，可以离开本页，后台会继续同步。';
}); }
async function save() { await action(async () => { const ids = roots.value.split(/[,，\s]+/).filter(Boolean).map(Number); await api.directorySettings({ rootDepartmentIds: ids, intervalMinutes: Number(interval.value), defaultCompanyId: defaultCompany.value || null }); await load(); message.value = '同步设置已保存'; }); }
async function mappingsChanged() { await action(load); emit('changed'); }
async function setActive(e: Employee) { await action(async () => { await api.setEmployeeActive(e.id, !e.active, statusReason.value); await load(); emit('changed'); message.value = `${e.name}已${e.active ? '停用' : '启用'}，历史记录保留`; }); }
onMounted(() => { void action(load); });
watch([companyFilter, departmentFilter], () => { page.value = 1; });
onBeforeUnmount(() => { disposed = true; if (pollTimer) clearTimeout(pollTimer); });
</script>

<template>
  <article class="panel directory-panel">
    <div class="panel-header"><div><span class="eyebrow">钉钉企业通讯录</span><h2>员工与部门</h2></div><button class="primary-button" :disabled="busy || syncing || !status?.configured" @click="sync">{{ syncing ? '正在同步…' : busy ? '处理中…' : '立即同步' }}</button></div>
    <p v-if="error" class="scan-error" role="alert">{{ error }}</p><p v-if="message" class="directory-message" role="status">{{ message }}</p>
    <div v-if="status && !status.configured" class="directory-notice"><p>尚未配置钉钉企业应用。由服务端配置后，即可同步授权范围内的部门和人员。</p><ul><li>企业 CorpId：{{ status.configuration.corpId ? '已配置' : '待配置 DINGTALK_CORP_ID' }}</li><li>应用 AppKey：{{ status.configuration.clientId ? '已配置' : '待配置 DINGTALK_CLIENT_ID' }}</li><li>应用 AppSecret：{{ status.configuration.clientSecret ? '已配置' : '待配置 DINGTALK_CLIENT_SECRET' }}</li></ul><p>钉钉开放平台还需授予部门、人员详情读取权限和通讯录可见范围。密钥仅保存在服务端。</p></div>
    <div class="directory-summary"><span>员工 <strong>{{ status?.employeesCount ?? 0 }}</strong></span><span>部门 <strong>{{ status?.departments.length ?? 0 }}</strong></span><span>资产待关联 <strong>{{ status?.unmappedCount ?? 0 }}</strong></span></div>
    <nav class="flow-tabs" aria-label="通讯录管理"><button v-for="item in [{ id: 'employees', label: '员工列表' }, { id: 'departments', label: '组织架构' }, { id: 'mappings', label: '旧台账关联' }, { id: 'sync', label: '同步设置与记录' }]" :key="item.id" :class="{ active: tab === item.id }" @click="tab = item.id">{{ item.label }}</button></nav>
    <section v-if="tab === 'employees'">
      <form class="directory-search" @submit.prevent="page = 1"><label class="field"><span>搜索员工、部门或工号</span><input v-model="search" @input="page = 1" /></label><button type="button" class="secondary-button" :disabled="busy" @click="action(load)">刷新</button></form>
      <div class="organization-filters"><label class="field"><span>所属公司</span><select v-model.number="companyFilter"><option :value="0">全部公司</option><option v-for="c in companies" :key="c.id" :value="c.id">{{ c.name }}</option></select></label><label class="field"><span>所属部门（含下级）</span><select v-model.number="departmentFilter"><option :value="0">全部部门</option><option v-for="row in departmentTree" :key="row.item.id" :value="row.item.dingtalk_id">{{ '　'.repeat(Math.min(row.depth, 6)) }}{{ row.item.name }}{{ row.item.missing ? '（待核对）' : '' }}</option></select></label></div>
      <label class="field directory-status-reason"><span>启用 / 停用原因</span><input v-model="statusReason" maxlength="500" placeholder="调整人员状态前填写原因" /></label>
      <div v-for="e in shown" :key="e.id" class="directory-row"><div class="directory-person"><strong>{{ e.name }}</strong><span>{{ e.company || '未分配公司' }}</span><span>{{ e.department || '未设置部门' }} · {{ e.job_number || `员工编号 ${e.id}` }}</span><small>{{ e.title }} {{ e.dingtalk_user_id }}</small><details v-if="e.memberships?.length"><summary>查看部门与公司关系</summary><div v-for="m in e.memberships" :key="m.department_id">{{ m.department_name }} → {{ m.company_name || '未绑定公司' }}</div></details></div><span class="role-pill">{{ roleLabels[e.role] || '未关联账号' }}</span><span class="directory-state" :class="{ warning: !e.active || e.missing_since }">{{ !e.active ? '已停用' : e.missing_since ? '授权范围待核对' : '已同步' }}</span><button class="secondary-button" :disabled="busy || syncing || !statusReason.trim()" @click="setActive(e)">{{ e.active ? '停用' : '启用' }}</button></div>
      <div v-if="!shown.length" class="empty-history">暂无员工，请同步钉钉通讯录或调整搜索条件。</div>
      <div class="pager"><span>共 {{ filtered.length }} 位员工</span><div><button :disabled="page <= 1" @click="page--">上一页</button><span>{{ page }} / {{ Math.max(1, Math.ceil(filtered.length / 20)) }}</span><button :disabled="page * 20 >= filtered.length" @click="page++">下一页</button></div></div>
    </section>
    <section v-else-if="tab === 'departments'">
      <p class="organization-help">展示已授权范围内的钉钉部门。点击部门查看对应人员，人员保留兼职部门关系。</p>
      <div class="department-list"><div v-for="row in departmentTree" :key="row.item.id" class="department-row" :style="{ paddingLeft: `${12 + Math.min(row.depth, 6) * 20}px` }"><button class="text-button" @click="departmentFilter = row.item.dingtalk_id; tab = 'employees'"><strong>{{ row.item.name }}</strong></button><span>部门编号 {{ row.item.dingtalk_id }}</span><span v-if="row.item.missing" class="directory-state warning">授权范围待核对</span><span v-else class="directory-state">已同步</span></div></div>
      <p v-if="!departmentTree.length" class="empty-history">尚无组织架构，配置钉钉应用后点击“立即同步”。</p>
    </section>
    <LegacyAssetMappings v-else-if="tab === 'mappings'" :assets="mappings" :disabled="busy || syncing" @changed="mappingsChanged" @refresh="action(load)" />
    <section v-else>
      <form class="directory-config" @submit.prevent="save"><label class="field"><span>授权起始部门编号</span><input v-model="roots" required /><small>根部门为 1；部分授权时填写可访问的部门编号，多个用逗号分隔。</small></label><label class="field"><span>定时同步间隔</span><select v-model.number="interval"><option :value="0">关闭，仅手动同步</option><option :value="30">每 30 分钟</option><option :value="60">每小时</option><option :value="360">每 6 小时</option><option :value="1440">每天</option></select></label><label class="field"><span>未绑定部门的默认公司</span><select v-model.number="defaultCompany"><option :value="0">不自动分配</option><option v-for="c in companies" :key="c.id" :value="c.id">{{ c.name }}</option></select><small>按公司绑定的部门就近归属；未命中时才使用此设置。</small></label><button class="primary-button" :disabled="busy || syncing">保存同步设置</button></form>
      <p class="muted">接口失败时保留原通讯录；未再读到的人员标为待核对，并暂停关联账号访问。核实离职后再停用，历史和名下资产保留。待审批人员须先处理单据再停用。</p>
      <div v-for="run in status?.runs" :key="run.id" class="directory-run"><strong>{{ run.status === 'completed' ? '同步成功' : run.status === 'failed' ? '同步失败' : '同步中' }}</strong><span>{{ formatSystemTime(run.created_at) }}</span><span>{{ run.employees_count }} 位员工 · {{ run.departments_count }} 个部门</span><p v-if="run.message">{{ run.message }}</p></div><p v-if="!status?.runs.length" class="empty-history">尚无同步记录。</p>
    </section>
  </article>
</template>

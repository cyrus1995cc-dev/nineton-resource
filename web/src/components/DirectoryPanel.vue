<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { api } from '../api';
import type { DirectoryStatus, Employee, EmployeeMapping } from '../directory';
import { roleLabels } from '../directory';
import { formatSystemTime } from '../dateTime';
const emit = defineEmits<{ changed: [] }>();
const status = ref<DirectoryStatus | null>(null); const employees = ref<Employee[]>([]); const mappings = ref<EmployeeMapping[]>([]);
const busy = ref(false); const error = ref(''); const message = ref(''); const search = ref(''); const tab = ref('employees');
const roots = ref('1'); const interval = ref(0); const page = ref(1); const mappingPage = ref(1);
const picks = reactive<Record<number, number>>({}); const reason = ref('已核对旧台账使用人与钉钉员工身份'); const statusReason = ref('');
const filtered = computed(() => employees.value.filter((e) => `${e.name} ${e.department} ${e.job_number}`.includes(search.value.trim())));
const shown = computed(() => filtered.value.slice((page.value - 1) * 20, page.value * 20));
const shownMappings = computed(() => mappings.value.slice((mappingPage.value - 1) * 20, mappingPage.value * 20));
async function load() {
  const [s, e, m] = await Promise.all([api.directoryStatus(), api.listEmployees(true), api.employeeMappings()]);
  status.value = s; employees.value = e.employees; mappings.value = m.assets; roots.value = s.rootDepartmentIds.join(','); interval.value = s.intervalMinutes;
}
async function action(fn: () => Promise<void>) { busy.value = true; error.value = ''; message.value = ''; try { await fn(); } catch (err) { error.value = err instanceof Error ? err.message : '操作失败'; } finally { busy.value = false; } }
async function sync() { await action(async () => { const result = await api.syncDirectory(); await load(); emit('changed'); message.value = `同步完成：${result.employees} 位员工，${result.departments} 个部门`; }); }
async function save() { await action(async () => { const ids = roots.value.split(/[,，\s]+/).filter(Boolean).map(Number); await api.directorySettings({ rootDepartmentIds: ids, intervalMinutes: Number(interval.value) }); await load(); message.value = '同步设置已保存'; }); }
async function bind(asset: EmployeeMapping) { await action(async () => { await api.bindAssetEmployee(asset.asset_code, picks[asset.id], asset.version, reason.value); await load(); emit('changed'); message.value = `${asset.asset_code} 已关联员工，台账姓名保持原记录`; }); }
async function setActive(e: Employee) { await action(async () => { await api.setEmployeeActive(e.id, !e.active, statusReason.value); await load(); emit('changed'); message.value = `${e.name}已${e.active ? '停用' : '启用'}，历史记录保留`; }); }
onMounted(() => { void action(load); });
</script>

<template>
  <article class="panel directory-panel">
    <div class="panel-header"><div><span class="eyebrow">钉钉企业通讯录</span><h2>员工与部门</h2></div><button class="primary-button" :disabled="busy || !status?.configured" @click="sync">{{ busy ? '处理中…' : '立即同步' }}</button></div>
    <p v-if="error" class="scan-error" role="alert">{{ error }}</p><p v-if="message" class="directory-message" role="status">{{ message }}</p>
    <p v-if="status && !status.configured" class="directory-notice">尚未配置钉钉企业应用。配置后可同步员工和部门；当前人员不会自动生成。</p>
    <div class="directory-summary"><span>员工 <strong>{{ status?.employeesCount ?? 0 }}</strong></span><span>部门 <strong>{{ status?.departments.length ?? 0 }}</strong></span><span>资产待关联 <strong>{{ status?.unmappedCount ?? 0 }}</strong></span></div>
    <nav class="flow-tabs" aria-label="通讯录管理"><button v-for="item in [{ id: 'employees', label: '员工列表' }, { id: 'mappings', label: '旧台账关联' }, { id: 'sync', label: '同步设置与记录' }]" :key="item.id" :class="{ active: tab === item.id }" @click="tab = item.id">{{ item.label }}</button></nav>
    <section v-if="tab === 'employees'">
      <form class="directory-search" @submit.prevent="page = 1"><label class="field"><span>搜索员工、部门或工号</span><input v-model="search" @input="page = 1" /></label><button type="button" class="secondary-button" :disabled="busy" @click="action(load)">刷新</button></form>
      <label class="field directory-status-reason"><span>启用 / 停用原因</span><input v-model="statusReason" maxlength="500" placeholder="调整人员状态前填写原因" /></label>
      <div v-for="e in shown" :key="e.id" class="directory-row"><div class="directory-person"><strong>{{ e.name }}</strong><span>{{ e.department || '未设置部门' }} · {{ e.job_number || `员工编号 ${e.id}` }}</span><small>{{ e.title }} {{ e.dingtalk_user_id }}</small></div><span class="role-pill">{{ roleLabels[e.role] || '未关联账号' }}</span><span class="directory-state" :class="{ warning: !e.active || e.missing_since }">{{ !e.active ? '已停用' : e.missing_since ? '授权范围待核对' : '已同步' }}</span><button class="secondary-button" :disabled="busy || !statusReason.trim()" @click="setActive(e)">{{ e.active ? '停用' : '启用' }}</button></div>
      <div v-if="!shown.length" class="empty-history">暂无员工，请同步钉钉通讯录或调整搜索条件。</div>
      <div class="pager"><span>共 {{ filtered.length }} 位员工</span><div><button :disabled="page <= 1" @click="page--">上一页</button><span>{{ page }} / {{ Math.max(1, Math.ceil(filtered.length / 20)) }}</span><button :disabled="page * 20 >= filtered.length" @click="page++">下一页</button></div></div>
    </section>
    <section v-else-if="tab === 'mappings'">
      <p class="muted">核对旧台账中的使用人后，选择对应的钉钉员工。重名人员请结合部门与工号确认；每次关联都有记录。</p>
      <label class="field"><span>关联原因</span><input v-model="reason" maxlength="500" /></label>
      <div v-for="asset in shownMappings" :key="asset.id" class="directory-mapping"><div><strong>{{ asset.asset_code }} · {{ asset.name }}</strong><span>{{ asset.assignee }} · {{ asset.department || '未设置部门' }}</span></div><label class="field"><span>对应员工{{ asset.candidates.length > 1 ? '（有重名，请核对）' : '' }}</span><select v-model.number="picks[asset.id]" :disabled="busy || !asset.candidates.length"><option :value="undefined" disabled>{{ asset.candidates.length ? '请选择对应员工' : '无同名员工，请核对通讯录或走交接审批' }}</option><option v-for="c in asset.candidates" :key="c.id" :value="c.id">{{ c.name }} · {{ c.department }} · {{ c.job_number || `编号 ${c.id}` }}</option></select></label><button class="secondary-button" :disabled="busy || !picks[asset.id] || !reason.trim()" @click="bind(asset)">核对并关联</button></div>
      <p v-if="!mappings.length" class="empty-history">没有待关联的旧台账记录。</p>
      <div class="pager"><span>共 {{ mappings.length }} 项待关联</span><div><button :disabled="mappingPage <= 1" @click="mappingPage--">上一页</button><span>{{ mappingPage }} / {{ Math.max(1, Math.ceil(mappings.length / 20)) }}</span><button :disabled="mappingPage * 20 >= mappings.length" @click="mappingPage++">下一页</button></div></div>
    </section>
    <section v-else>
      <form class="directory-config" @submit.prevent="save"><label class="field"><span>授权起始部门编号</span><input v-model="roots" required /><small>根部门为 1；部分授权时填写可访问的部门编号，多个用逗号分隔。</small></label><label class="field"><span>定时同步间隔</span><select v-model.number="interval"><option :value="0">关闭，仅手动同步</option><option :value="30">每 30 分钟</option><option :value="60">每小时</option><option :value="360">每 6 小时</option><option :value="1440">每天</option></select></label><button class="primary-button" :disabled="busy">保存同步设置</button></form>
      <p class="muted">接口失败时保留原通讯录；未再读到的人员标为待核对，并暂停关联账号访问。核实离职后再停用，历史和名下资产保留。待审批人员须先处理单据再停用。</p>
      <div v-for="run in status?.runs" :key="run.id" class="directory-run"><strong>{{ run.status === 'completed' ? '同步成功' : run.status === 'failed' ? '同步失败' : '同步中' }}</strong><span>{{ formatSystemTime(run.created_at) }}</span><span>{{ run.employees_count }} 位员工 · {{ run.departments_count }} 个部门</span><p v-if="run.message">{{ run.message }}</p></div><p v-if="!status?.runs.length" class="empty-history">尚无同步记录。</p>
    </section>
  </article>
</template>

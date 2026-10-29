<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue';
import { api } from '../api';
import type { EmployeeMapping } from '../directory';
import { groupEmployeeMappings, mappingBatch, type MappingGroup } from '../employeeMappings';
import AppIcon from './AppIcon.vue';

const props = defineProps<{ assets: EmployeeMapping[]; disabled?: boolean }>();
const emit = defineEmits<{ changed: []; refresh: [] }>();
const groups = computed(() => groupEmployeeMappings(props.assets));
const picks = reactive<Record<string, number>>({});
const selected = ref(new Set<string>());
const search = ref(''); const filter = ref('all'); const page = ref(1);
const busy = ref(false); const error = ref(''); const message = ref('');
const reason = ref('已核对旧台账使用人与当前钉钉员工身份');
const dialog = ref<HTMLDialogElement | null>(null);
type Review = { assignee: string; department: string; company: string; employee: EmployeeMapping['candidates'][number]; assets: EmployeeMapping[] };
const review = ref<Review[]>([]); const reviewReason = ref('');
const reviewCount = computed(() => review.value.reduce((n, group) => n + group.assets.length, 0));
const locked = computed(() => props.disabled || busy.value);
const counts = computed(() => Object.fromEntries(['unique','ambiguous','departed','inactive'].map(state => [state, groups.value.filter(group => group.state === state).reduce((n, group) => n + group.assets.length, 0)])));
const filtered = computed(() => groups.value.filter(group => (filter.value === 'all' || group.state === filter.value) && `${group.assignee} ${group.department} ${group.company} ${group.assets.map(a => `${a.asset_code} ${a.name}`).join(' ')}`.toLowerCase().includes(search.value.trim().toLowerCase())));
const shown = computed(() => filtered.value.slice((page.value - 1) * 15, page.value * 15));
const selectedGroups = computed(() => groups.value.filter(group => selected.value.has(group.key)));
const selectedCount = computed(() => selectedGroups.value.reduce((n, group) => n + group.assets.length, 0));
const stateLabels = { unique: '唯一同名候选', ambiguous: '重名，需核对', departed: '已离职', inactive: '员工已停用' };

watch(() => props.assets, () => {
  for (const key of Object.keys(picks)) delete picks[key];
  for (const group of groups.value) if (group.state === 'unique') picks[group.key] = group.candidates[0]!.id;
  selected.value = new Set(); page.value = 1;
  if (!busy.value) { dialog.value?.close(); review.value = []; }
}, { immediate: true });
watch([search, filter], () => { page.value = 1; });
function toggle(group: MappingGroup, checked: boolean) { const next = new Set(selected.value); if (checked) next.add(group.key); else next.delete(group.key); selected.value = next; }
function chooseUnique() { selected.value = new Set(filtered.value.filter(group => group.state === 'unique').map(group => group.key)); }
function cancel(event?: Event) { if (busy.value) event?.preventDefault(); else { dialog.value?.close(); review.value = []; } }
async function preview(chosen: MappingGroup[]) {
  if (locked.value) return;
  error.value = ''; message.value = '';
  try {
    if (!chosen.length) throw new Error('请先勾选需要关联的人员分组');
    if (!reason.value.trim()) throw new Error('请填写关联原因');
    const items = mappingBatch(chosen, picks);
    if (items.length > 2000) throw new Error('每次最多关联 2000 件资产，请减少选择后分批处理');
    review.value = chosen.map(group => ({ assignee: group.assignee, department: group.department, company: group.company, employee: { ...group.candidates.find(candidate => candidate.id === picks[group.key])! }, assets: group.assets.map(asset => ({ ...asset })) }));
    reviewReason.value = reason.value.trim(); await nextTick(); dialog.value?.showModal();
  } catch (err) { error.value = err instanceof Error ? err.message : '请核对关联信息'; }
}
async function submit() {
  if (locked.value || !review.value.length) return;
  busy.value = true; error.value = '';
  try {
    const items = review.value.flatMap(group => group.assets.map(asset => ({ assetCode: asset.asset_code, employeeId: group.employee.id, expectedVersion: asset.version })));
    const result = await api.bindAssetEmployees(items, reviewReason.value);
    message.value = `已关联 ${result.bound} 件资产${result.skipped ? `，${result.skipped} 件已关联，自动跳过` : ''}。离职人员资产保留原记录。`;
    dialog.value?.close(); review.value = []; selected.value = new Set(); emit('changed');
  } catch (err) { error.value = err instanceof Error ? err.message : '关联失败，请重试'; }
  finally { busy.value = false; }
}
</script>

<template>
  <section class="legacy-mappings">
    <div class="mapping-intro"><h3>按使用人批量关联</h3><p>同一使用人、原部门和公司的资产合并为一组，核对一次即可关联整组。唯一同名候选已预填，提交前请核对部门与工号。</p></div>
    <div class="mapping-counts"><button v-for="item in [{id:'all',label:'全部待关联',count:assets.length},{id:'unique',label:'唯一同名候选',count:counts.unique},{id:'ambiguous',label:'重名待核对',count:counts.ambiguous},{id:'departed',label:'离职人员资产',count:counts.departed},...(counts.inactive ? [{id:'inactive',label:'员工已停用',count:counts.inactive}] : [])]" :key="item.id" :class="{active:filter===item.id}" :aria-pressed="filter===item.id" @click="filter=item.id"><span>{{ item.label }}</span><strong>{{ item.count }}</strong></button></div>
    <p v-if="error" class="scan-error" role="alert">{{ error }}</p><p v-if="message" class="directory-message" role="status">{{ message }}</p>
    <div class="mapping-tools"><label class="field"><span>搜索使用人、部门或资产编码</span><input v-model="search" type="search" :disabled="locked" /></label><button class="secondary-button" :disabled="locked || !filtered.some(group=>group.state==='unique')" @click="chooseUnique">选择筛选内唯一匹配</button><button class="text-button" :disabled="locked || !selected.size" @click="selected=new Set()">清空选择</button><button class="secondary-button" :disabled="locked" @click="emit('refresh')">刷新待关联</button></div>
    <div class="mapping-batch-bar"><div><strong>已选 {{ selectedGroups.length }} 组 · {{ selectedCount }} 件资产</strong><small>选择覆盖当前筛选结果的所有分页</small></div><button class="primary-button" :disabled="locked || !selectedCount || !reason.trim()" @click="preview(selectedGroups)"><AppIcon name="check" :size="17" />批量关联已选</button></div>
    <label class="field mapping-reason"><span>关联原因</span><input v-model="reason" maxlength="500" :disabled="locked" /></label>
    <p v-if="filter==='departed'" class="mapping-departed-note">按管理员确认口径，当前通讯录中没有同名人员的旧台账归为离职人员资产。保留原使用人和资产状态，后续由管理员处理回收或交接。</p>
    <p v-if="!shown.length" class="empty-history">{{ assets.length ? '没有符合筛选条件的人员分组。' : '旧台账已全部关联。' }}</p>
    <article v-for="group in shown" :key="group.key" class="mapping-group">
      <div class="mapping-group-heading"><label class="mapping-check"><input type="checkbox" :aria-label="`选择${group.assignee}的${group.assets.length}件资产`" :checked="selected.has(group.key)" :disabled="locked || !group.candidates.length || !picks[group.key]" @change="toggle(group,($event.target as HTMLInputElement).checked)" /><strong>{{ group.assignee }}</strong></label><span :class="['mapping-state',group.state]">{{ stateLabels[group.state] }}</span><span class="mapping-asset-count">{{ group.assets.length }} 件资产</span></div>
      <div class="mapping-group-body"><div class="mapping-legacy-info"><span>原部门：{{ group.department || '未填写' }}</span><span v-if="group.company">原公司：{{ group.company }}</span><details><summary>查看该组 {{ group.assets.length }} 件资产</summary><ul><li v-for="asset in group.assets" :key="asset.id"><strong>{{ asset.asset_code }}</strong><span>{{ asset.name }}</span></li></ul></details></div><label v-if="group.candidates.length" class="field"><span>对应钉钉员工{{ group.state==='ambiguous' ? '（有重名，请核对）' : '' }}</span><select v-model.number="picks[group.key]" :disabled="locked"><option :value="undefined" disabled>请选择对应员工</option><option v-for="candidate in group.candidates" :key="candidate.id" :value="candidate.id">{{ candidate.name }} · {{ candidate.department || '未设置部门' }} · {{ candidate.job_number || `员工编号 ${candidate.id}` }}</option></select></label><p v-else class="mapping-group-notice">{{ group.state==='departed' ? '已离职，保留原资产归属，不参与在职员工关联。' : '通讯录中员工已停用，暂不关联。' }}</p><button v-if="group.candidates.length" class="secondary-button" :disabled="locked || !picks[group.key] || !reason.trim()" @click="preview([group])">关联该组 {{ group.assets.length }} 件</button></div>
    </article>
    <div class="pager mapping-pager"><span>共 {{ filtered.length }} 组 · {{ filtered.reduce((n,group)=>n+group.assets.length,0) }} 件资产</span><div><button :disabled="locked || page<=1" @click="page--">上一页</button><span>{{ page }} / {{ Math.max(1,Math.ceil(filtered.length/15)) }}</span><button :disabled="locked || page*15>=filtered.length" @click="page++">下一页</button></div></div>
    <dialog ref="dialog" class="mapping-review-dialog" aria-labelledby="mapping-review-title" @cancel="cancel"><form @submit.prevent="submit"><header class="mapping-review-header"><div><span class="eyebrow">提交前核对</span><h2 id="mapping-review-title">确认批量关联</h2></div><button type="button" class="icon-button" aria-label="关闭关联确认" :disabled="busy" @click="cancel()"><AppIcon name="close" /></button></header><div class="mapping-review-scroll"><p class="mapping-review-summary">将 {{ review.length }} 个分组的 <strong>{{ reviewCount }} 件资产</strong> 关联到下列员工。请核实人员身份；离职、未选择的分组保持原记录。</p><p class="mapping-review-reason">关联原因：{{ reviewReason }}</p><p v-if="error" class="scan-error" role="alert">{{ error }} 本批次未完成，请取消后刷新核对；若上次请求结果未知，重试会跳过已关联到同一员工的资产。</p><div v-for="(group,index) in review" :key="index" class="mapping-review-row"><strong>{{ group.assignee }} · {{ group.assets.length }} 件</strong><span>原部门：{{ group.department || '未填写' }}</span><span v-if="group.company">原公司：{{ group.company }}</span><span class="mapping-review-target">关联到：{{ group.employee.name }} · {{ group.employee.department || '未设置部门' }} · {{ group.employee.job_number || `员工编号 ${group.employee.id}` }}</span><details><summary>资产编码明细</summary><p>{{ group.assets.map(asset=>asset.asset_code).join('、') }}</p></details></div></div><footer class="mapping-review-footer"><span>每件资产均保留关联记录。</span><div class="form-actions"><button type="button" class="secondary-button" :disabled="busy" @click="cancel()">取消</button><button class="primary-button" :disabled="locked">{{ busy ? '正在关联，请稍候…' : `确认关联 ${reviewCount} 件资产` }}</button></div></footer></form></dialog>
  </section>
</template>

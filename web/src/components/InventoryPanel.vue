<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api } from '../api';
import { getAssetCodeFromScan } from '../qrLookup';
import type { InventoryRecord, InventoryTask, InventoryTaskDetail } from '../types';
import QrScanner from './QrScanner.vue';
import { formatSystemTime } from '../dateTime';
import ExportButton from './ExportButton.vue';

const props = defineProps<{ locations: Array<{ name: string; count: number }>; canWrite: boolean }>();
const emit = defineEmits<{ error: [message: string]; success: [message: string] }>();
const tasks = ref<InventoryTask[]>([]);
const detail = ref<InventoryTaskDetail | null>(null);
const name = ref(''); const location = ref('');
const scanText = ref(''); const actualLocation = ref(''); const note = ref('');
const busy = ref(false); const error = ref('');
const search = ref(''); const resultFilter = ref<InventoryRecord['result'] | ''>('');
const page = ref(1); const pageSize = 20;
const confirmation = ref<HTMLDialogElement | null>(null);
const activeTasks = computed(() => tasks.value.filter((task) => task.status === 'in_progress'));
const pendingCount = computed(() => detail.value?.records.filter((record) => record.result === 'pending').length ?? 0);
const progress = computed(() => detail.value?.task.total_count ? Math.round(detail.value.task.checked_count / detail.value.task.total_count * 100) : 0);
const labels: Record<InventoryRecord['result'], string> = { pending: '待盘', found: '正常', different: '位置差异', missing: '未盘到' };
const filtered = computed(() => {
  const query = search.value.trim().toLowerCase();
  return detail.value?.records.filter((record) => (!resultFilter.value || record.result === resultFilter.value)
    && (!query || `${record.asset_code} ${record.asset_name}`.toLowerCase().includes(query))) ?? [];
});
const pages = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)));
const records = computed(() => filtered.value.slice((page.value - 1) * pageSize, page.value * pageSize));
watch([search, resultFilter], () => { page.value = 1; });
watch(pages, (value) => { page.value = Math.min(page.value, value); });

function report(err: unknown, fallback: string) { error.value = err instanceof Error ? err.message : fallback; emit('error', error.value); }
function applyDetail(value: InventoryTaskDetail, reset = false) {
  detail.value = value;
  const index = tasks.value.findIndex((task) => task.id === value.task.id);
  if (index >= 0) tasks.value[index] = value.task;
  else tasks.value.unshift(value.task);
  if (reset) { actualLocation.value = value.task.location_filter; scanText.value = ''; note.value = ''; search.value = ''; resultFilter.value = ''; page.value = 1; }
}
async function loadTasks() {
  if (busy.value) return;
  busy.value = true; error.value = '';
  try {
    tasks.value = (await api.listInventoryTasks()).tasks;
    const selected = tasks.value.find((task) => task.id === detail.value?.task.id) ?? activeTasks.value[0] ?? tasks.value[0];
    if (selected) applyDetail(await api.getInventoryTask(selected.id), !detail.value);
  } catch (err) { report(err, '盘点任务加载失败'); }
  finally { busy.value = false; }
}
async function openTask(task: InventoryTask) {
  if (busy.value || task.id === detail.value?.task.id) return;
  busy.value = true; error.value = '';
  try { applyDetail(await api.getInventoryTask(task.id), true); }
  catch (err) { report(err, '盘点任务详情加载失败'); }
  finally { busy.value = false; }
}
async function createTask() {
  if (busy.value || !props.canWrite || !name.value.trim()) return;
  busy.value = true; error.value = '';
  try {
    applyDetail(await api.createInventoryTask({ name: name.value.trim(), location: location.value }), true);
    name.value = ''; emit('success', '盘点任务创建成功');
  } catch (err) { report(err, '盘点任务创建失败'); }
  finally { busy.value = false; }
}
async function scan() {
  if (busy.value || !detail.value || detail.value.task.status !== 'in_progress' || !props.canWrite || !scanText.value.trim()) return;
  const assetCode = getAssetCodeFromScan(scanText.value);
  if (!assetCode) { error.value = '无法识别资产编码，请输入完整二维码原文或资产编码'; return; }
  busy.value = true; error.value = '';
  try {
    const result = await api.recordInventoryScan(detail.value.task.id, { assetCode, actualLocation: actualLocation.value.trim() || undefined, note: note.value });
    applyDetail(result); scanText.value = ''; note.value = '';
    emit('success', result.replayed ? `${assetCode} 已登记，无需重复操作` : `已登记 ${assetCode} · ${labels[result.record.result]}`);
  } catch (err) { report(err, '盘点登记失败'); }
  finally { busy.value = false; }
}
function requestComplete() { if (!busy.value && props.canWrite && detail.value?.task.status === 'in_progress') confirmation.value?.showModal(); }
async function complete() {
  if (busy.value || !detail.value || !props.canWrite) return;
  confirmation.value?.close(); busy.value = true; error.value = '';
  try { applyDetail(await api.completeInventoryTask(detail.value.task.id)); resultFilter.value = ''; page.value = 1; emit('success', '盘点任务已结束'); }
  catch (err) { report(err, '结束盘点失败'); }
  finally { busy.value = false; }
}
onMounted(loadTasks);
</script>

<template>
  <div class="inventory-workspace" :aria-busy="busy">
    <details class="panel inventory-guide"><summary>盘点怎么做？查看操作步骤</summary><ol><li><strong>按位置建任务：</strong>例如“12楼季度盘点”，系统保存创建时的应盘清单。</li><li><strong>到现场扫码：</strong>使用安卓 App 扫描原有二维码，也可输入资产编码。填写当前实际楼层，再逐件登记；重复扫码不会重复计数。</li><li><strong>核对差异：</strong>搜索明细，检查待盘和位置差异。范围外资产会提示，可查询台账后另建任务。</li><li><strong>确认后结束：</strong>剩余待盘项记为“未盘到”，不代表已丢失。任务结束后不能继续登记。</li><li><strong>导出并处理：</strong>下载全部明细与差异清单；需要修改归属或位置时，核实后提交交接或调拨审批。</li></ol><p>当前为在线盘点，手机需要连接系统服务。建议首次先按一个楼层试盘，再扩大范围。</p></details>
    <div v-if="error" class="scan-error inventory-error" role="alert">{{ error }} <button class="text-button" :disabled="busy" @click="loadTasks">重新加载</button></div>
    <div class="inventory-layout">
      <article class="panel inventory-create">
        <div class="panel-header"><div><span class="eyebrow">盘点任务</span><h2>{{ canWrite ? '创建盘点' : '查看盘点' }}</h2></div><span class="panel-count">{{ activeTasks.length }} 个进行中</span></div>
        <form v-if="canWrite" class="event-form" @submit.prevent="createTask">
          <label class="field"><span>任务名称</span><input v-model="name" placeholder="例如：12楼季度盘点" maxlength="100" required :disabled="busy" /></label>
          <label class="field"><span>位置范围</span><select v-model="location" :disabled="busy"><option value="">全部位置</option><option v-for="item in locations" :key="item.name" :value="item.name">{{ item.name }}（{{ item.count }}）</option></select></label>
          <button class="primary-button full-button" :disabled="busy || !name.trim()">创建盘点任务</button>
        </form>
        <p v-else class="muted">只读账号可查看进度和盘点结果。</p>
        <div class="section-title task-list-heading"><h3>任务列表</h3><button class="text-button" :disabled="busy" @click="loadTasks">刷新</button></div>
        <div class="inventory-task-list"><button v-for="task in tasks" :key="task.id" class="inventory-task" :disabled="busy" :class="{ active: detail?.task.id === task.id }" :aria-pressed="detail?.task.id === task.id" @click="openTask(task)"><strong>{{ task.name }}</strong><span>{{ task.status === 'completed' ? '已结束' : '进行中' }} · {{ task.checked_count }}/{{ task.total_count }} 已盘</span><em v-if="task.difference_count">差异 {{ task.difference_count }}</em></button><div v-if="!tasks.length" class="empty-history">{{ busy ? '正在加载任务…' : '还没有盘点任务' }}</div></div>
      </article>
      <article v-if="detail" class="panel inventory-detail">
        <div class="panel-header"><div><span class="eyebrow">盘点明细</span><h2>{{ detail.task.name }}</h2><p class="inventory-scope">{{ detail.task.location_filter || '全部位置' }} · 创建人 {{ detail.task.operator_name || '未记录' }}</p></div><span class="status-pill" :class="detail.task.status === 'completed' ? 'status-报废' : 'status-在用'">{{ detail.task.status === 'completed' ? '已结束' : '进行中' }}</span></div>
        <div class="inventory-summary"><div><strong>{{ detail.task.total_count }}</strong><span>应盘资产</span></div><div><strong>{{ detail.task.checked_count }}</strong><span>实际已盘</span></div><div><strong>{{ pendingCount }}</strong><span>待盘资产</span></div><div><strong>{{ detail.task.difference_count }}</strong><span>差异资产</span></div></div>
        <div class="inventory-progress"><div><span>实盘进度</span><strong>{{ progress }}%</strong></div><progress :value="detail.task.checked_count" :max="detail.task.total_count" aria-label="实盘进度"></progress></div>
        <template v-if="canWrite && detail.task.status === 'in_progress'">
          <form class="inventory-scan-form" @submit.prevent="scan">
            <label class="field"><span>二维码原文或资产编码</span><input v-model="scanText" placeholder="例如：CSJ-0431" autocomplete="off" :disabled="busy" required /></label>
            <label class="field"><span>实盘位置</span><input v-model="actualLocation" placeholder="留空时按台账位置登记" :disabled="busy" /></label>
            <label class="field field-wide"><span>盘点备注（可选）</span><input v-model="note" placeholder="例如：资产在另一楼层，待核实" :disabled="busy" maxlength="500" /></label>
            <div class="form-actions field-wide"><button class="primary-button" :disabled="busy || !scanText.trim()">{{ busy ? '处理中…' : '登记盘点' }}</button><button class="secondary-button" type="button" :disabled="busy" @click="requestComplete">结束盘点</button></div>
          </form>
          <QrScanner :disabled="busy" @scan="(value) => { scanText = value; scan(); }" />
          <p class="inventory-help">位置差异会记入盘点结果；需要调整台账时，请核实后提交调拨或交接审批。</p>
        </template>
        <p v-if="detail.task.status === 'completed'" class="inventory-help">任务已结束。“未盘到”计入差异，不计入实际已盘。</p>
        <div class="export-actions"><ExportButton :path="`/api/exports/inventory/${detail.task.id}`" :disabled="busy" label="导出盘点结果" description="导出该任务概况、全部明细和差异清单，不受下方明细筛选影响" @success="emit('success', $event)" @error="(message) => { error = message; }" /><span class="export-note">包含任务概况、全部明细、差异清单</span></div><div class="inventory-records">
          <div class="section-title"><h3>盘点明细</h3><span>{{ filtered.length }} / {{ detail.records.length }} 项</span></div>
          <div class="inventory-filters"><label class="field"><span>搜索明细</span><input v-model="search" type="search" placeholder="资产编码或名称" /></label><label class="field"><span>盘点结果</span><select v-model="resultFilter"><option value="">全部结果</option><option v-for="(label, value) in labels" :key="value" :value="value">{{ label }}</option></select></label></div>
          <div v-for="record in records" :key="record.id" class="inventory-record"><div><strong>{{ record.asset_code }} · {{ record.asset_name || '未命名资产' }}</strong><span>应在 {{ record.expected_location || '未设置' }} · {{ record.expected_status }}</span><span v-if="record.checked_at">实盘 {{ record.actual_location || '未设置' }} · {{ record.operator_name || '未记录操作人' }} · {{ formatSystemTime(record.checked_at) }}</span><span v-else-if="record.result === 'missing'">结束时未盘到 · {{ record.operator_name || '未记录操作人' }}</span><span v-if="record.note">备注：{{ record.note }}</span></div><span class="inventory-result" :class="`result-${record.result}`">{{ labels[record.result] }}</span></div>
          <div v-if="!records.length" class="empty-history">没有符合条件的明细</div>
          <div class="table-footer inventory-pager"><span>每页 {{ pageSize }} 项</span><div class="pager"><button :disabled="page <= 1" @click="page--">上一页</button><span>{{ page }} / {{ pages }}</span><button :disabled="page >= pages" @click="page++">下一页</button></div></div>
        </div>
      </article>
      <article v-else class="panel placeholder-panel"><span class="eyebrow">盘点明细</span><h2>{{ busy ? '正在加载盘点…' : '选择或创建盘点任务' }}</h2><p>按位置生成任务后，扫码或输入资产编码逐项登记。</p></article>
    </div>
    <dialog ref="confirmation" class="inventory-confirm" aria-labelledby="complete-title" aria-describedby="complete-description">
      <span class="eyebrow">结束盘点</span><h2 id="complete-title">确认结束「{{ detail?.task.name }}」？</h2>
      <p id="complete-description">{{ pendingCount ? `还有 ${pendingCount} 项待盘资产，结束后将全部记为“未盘到”。` : '全部资产已盘，结束后将保留本次盘点结果。' }}结束后无法继续登记。</p>
      <div class="form-actions"><button class="secondary-button" autofocus @click="confirmation?.close()">继续盘点</button><button class="primary-button" :disabled="busy" @click="complete">确认结束</button></div>
    </dialog>
  </div>
</template>

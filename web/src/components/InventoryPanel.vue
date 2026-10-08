<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { api } from '../api';
import { getAssetCodeFromScan } from '../qrLookup';
import type { InventoryTask, InventoryTaskDetail } from '../types';

const props = defineProps<{ locations: Array<{ name: string; count: number }>; canWrite: boolean }>();
const emit = defineEmits<{ error: [message: string]; success: [message: string] }>();
const tasks = ref<InventoryTask[]>([]);
const detail = ref<InventoryTaskDetail | null>(null);
const name = ref('');
const location = ref('');
const scanText = ref('');
const actualLocation = ref('');
const busy = ref(false);
const error = ref('');
const activeTasks = computed(() => tasks.value.filter((task) => task.status === 'in_progress'));
const differences = computed(() => detail.value?.records.filter((record) => record.result === 'different' || record.result === 'missing') ?? []);

async function loadTasks() {
  try {
    tasks.value = (await api.listInventoryTasks()).tasks;
    if (!detail.value && activeTasks.value[0]) await openTask(activeTasks.value[0]);
  } catch (err) { error.value = err instanceof Error ? err.message : '盘点任务加载失败'; }
}
async function openTask(task: InventoryTask) {
  try { detail.value = await api.getInventoryTask(task.id); actualLocation.value = task.location_filter; error.value = ''; }
  catch (err) { error.value = err instanceof Error ? err.message : '盘点任务详情加载失败'; }
}
async function createTask() {
  if (!props.canWrite || !name.value.trim()) return;
  busy.value = true;
  try { detail.value = await api.createInventoryTask({ name: name.value.trim(), location: location.value, operatorName: '当前用户' }); name.value = ''; await loadTasks(); emit('success', '盘点任务创建成功'); }
  catch (err) { error.value = err instanceof Error ? err.message : '盘点任务创建失败'; emit('error', error.value); }
  finally { busy.value = false; }
}
async function scan() {
  if (!detail.value || !props.canWrite || !scanText.value.trim()) return;
  const assetCode = getAssetCodeFromScan(scanText.value);
  if (!assetCode) { error.value = '无法识别资产编码，请输入完整二维码原文或资产编码'; return; }
  busy.value = true;
  try { detail.value = await api.recordInventoryScan(detail.value.task.id, { assetCode, actualLocation: actualLocation.value || undefined, operatorName: '当前用户' }); scanText.value = ''; error.value = ''; emit('success', `已登记 ${assetCode}`); }
  catch (err) { error.value = err instanceof Error ? err.message : '盘点登记失败'; emit('error', error.value); }
  finally { busy.value = false; }
}
async function complete() {
  if (!detail.value || !props.canWrite) return;
  busy.value = true;
  try { detail.value = await api.completeInventoryTask(detail.value.task.id, '当前用户'); await loadTasks(); emit('success', '盘点任务已结束'); }
  catch (err) { error.value = err instanceof Error ? err.message : '结束盘点失败'; emit('error', error.value); }
  finally { busy.value = false; }
}
onMounted(loadTasks);
</script>

<template>
  <div class="inventory-layout">
    <article class="panel inventory-create">
      <div class="panel-header"><div><span class="eyebrow">盘点任务</span><h2>创建盘点</h2></div><span class="panel-count">{{ activeTasks.length }} 个进行中</span></div>
      <form class="event-form" @submit.prevent="createTask">
        <label class="field"><span>任务名称</span><input v-model="name" placeholder="例如：12楼季度盘点" required /></label>
        <label class="field"><span>位置范围</span><select v-model="location"><option value="">全部位置</option><option v-for="item in locations" :key="item.name" :value="item.name">{{ item.name }}（{{ item.count }}）</option></select></label>
        <button class="primary-button full-button" :disabled="!canWrite || busy">{{ busy ? '处理中…' : '创建盘点任务' }}</button>
      </form>
      <div class="inventory-task-list"><button v-for="task in tasks" :key="task.id" class="inventory-task" :class="{ active: detail?.task.id === task.id }" @click="openTask(task)"><strong>{{ task.name }}</strong><span>{{ task.status === 'completed' ? '已结束' : '进行中' }} · {{ task.checked_count }}/{{ task.total_count }} 已盘</span><em v-if="task.difference_count">差异 {{ task.difference_count }}</em></button><div v-if="!tasks.length" class="empty-history">还没有盘点任务</div></div>
    </article>

    <article class="panel inventory-detail" v-if="detail">
      <div class="panel-header"><div><span class="eyebrow">盘点明细</span><h2>{{ detail.task.name }}</h2></div><span class="status-pill" :class="detail.task.status === 'completed' ? 'status-报废' : 'status-在用'">{{ detail.task.status === 'completed' ? '已结束' : '进行中' }}</span></div>
      <div class="inventory-summary"><div><strong>{{ detail.task.total_count }}</strong><span>应盘</span></div><div><strong>{{ detail.task.checked_count }}</strong><span>已盘</span></div><div><strong>{{ detail.task.difference_count }}</strong><span>差异</span></div></div>
      <form v-if="detail.task.status === 'in_progress'" class="inventory-scan-form" @submit.prevent="scan"><input v-model="scanText" placeholder="扫描二维码或输入资产编码" /><input v-model="actualLocation" placeholder="实盘位置（可选）" /><button class="primary-button" :disabled="busy || !canWrite">登记盘点</button><button class="secondary-button" type="button" :disabled="busy || !canWrite" @click="complete">结束盘点</button></form>
      <div v-if="error" class="scan-error">{{ error }}</div>
      <div class="inventory-records"><div class="section-title"><h3>盘点明细</h3><span>{{ detail.records.length }} 项</span></div><div v-for="record in detail.records" :key="record.id" class="inventory-record"><div><strong>{{ record.asset_code }} · {{ record.asset_name || '未命名资产' }}</strong><span>应在 {{ record.expected_location || '未设置' }} · {{ record.expected_status }}</span></div><span class="inventory-result" :class="`result-${record.result}`">{{ record.result === 'pending' ? '待盘' : record.result === 'found' ? '正常' : record.result === 'different' ? '位置差异' : '缺失' }}</span></div></div>
      <div v-if="differences.length" class="inventory-difference"><strong>差异清单</strong><span v-for="record in differences" :key="record.id">{{ record.asset_code }}：{{ record.result === 'missing' ? '未找到' : `实盘位置 ${record.actual_location || '未填写'}` }}</span></div>
    </article>
    <article v-else class="panel placeholder-panel"><span class="eyebrow">盘点明细</span><h2>选择或创建盘点任务</h2><p>按位置生成任务后，使用二维码原文或资产编码逐项登记。</p></article>
  </div>
</template>

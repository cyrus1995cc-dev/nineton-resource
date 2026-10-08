<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { Asset } from '../types';

type EventPayload = { type: string; toAssignee?: string; toDepartment?: string; toLocation?: string; note?: string };

const props = defineProps<{ asset: Asset | null; busy?: boolean }>();
const emit = defineEmits<{ submit: [payload: EventPayload] }>();

const action = ref('');
const assignee = ref('');
const department = ref('');
const location = ref('');
const note = ref('');

const actionOptions = computed(() => {
  if (!props.asset) return [];
  if (props.asset.status === '空闲') return ['领用', '借出', '维修', '报废'];
  if (props.asset.status === '在用') return ['归还', '调拨', '维修', '报废'];
  if (props.asset.status === '借出') return ['借出归还', '调拨', '维修', '报废'];
  if (props.asset.status === '维修') return ['维修', '调拨', '报废'];
  return [];
});

watch(() => props.asset, (asset) => {
  action.value = actionOptions.value[0] ?? '';
  assignee.value = asset?.assignee ?? '';
  department.value = asset?.department ?? '';
  location.value = asset?.location ?? '';
  note.value = '';
}, { immediate: true });

function submit() {
  if (!action.value || !props.asset) return;
  emit('submit', {
    type: action.value,
    ...(action.value === '领用' || action.value === '借出' ? { toAssignee: assignee.value, toDepartment: department.value } : {}),
    ...(action.value === '调拨' ? { toLocation: location.value } : {}),
    note: note.value,
  });
}
</script>

<template>
  <article class="panel flow-action-panel">
    <div v-if="asset">
      <div class="panel-header"><div><span class="eyebrow">资产操作</span><h2>{{ asset.asset_code }}</h2></div><span class="status-pill" :class="`status-${asset.status}`">{{ asset.status }}</span></div>
      <div class="flow-asset-summary"><strong>{{ asset.name || '未命名资产' }}</strong><span>{{ asset.location || '未设置位置' }} · {{ asset.assignee || '暂无使用人' }}</span></div>
      <div v-if="!actionOptions.length" class="empty-history">当前状态暂无可用操作</div>
      <form v-else class="event-form" @submit.prevent="submit">
        <label class="field"><span>选择操作</span><select v-model="action"><option v-for="item in actionOptions" :key="item" :value="item">{{ item }}</option></select></label>
        <template v-if="action === '领用' || action === '借出'"><label class="field"><span>{{ action === '借出' ? '借用人' : '使用人' }}</span><input v-model="assignee" required /></label><label class="field"><span>所属部门</span><input v-model="department" /></label></template>
        <label v-if="action === '调拨'" class="field"><span>调拨到位置</span><input v-model="location" required /></label>
        <label class="field"><span>备注</span><textarea v-model="note" rows="3" placeholder="可填写原因、归还时间或维修说明"></textarea></label>
        <button class="primary-button full-button" type="submit" :disabled="busy">{{ busy ? '提交中…' : `确认${action}` }}</button>
      </form>
    </div>
    <div v-else class="flow-empty"><span class="scan-icon">↔</span><h2>选择一项资产</h2><p>从左侧资产列表选择资产后，在这里发起领用、借出、归还、调拨或维修操作。</p></div>
  </article>
</template>

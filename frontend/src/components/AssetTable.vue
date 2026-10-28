<script setup lang="ts">
import { computed, ref } from 'vue';
import type { Asset, AuthUser } from '../types';
import { allowedAssetFlows, assetColumns, assetDepartmentLabel, pageNumbers, type AssetColumn } from '../assetWorkspace';
import type { FlowType } from '../../shared/flows';
import AppIcon from './AppIcon.vue';

const props = defineProps<{ items: Asset[]; total: number; page: number; pageSize: number; loading: boolean; columns: AssetColumn[]; density: string; selectedCodes: string[]; user: AuthUser; sort: string; direction: string }>();
const emit = defineEmits<{ select: [asset: Asset]; page: [page: number]; pageSize: [size: number]; sort: [column: string]; toggle: [asset: Asset]; togglePage: []; flow: [assets: Asset[], type: FlowType] }>();
const visible = computed(() => assetColumns.filter(c => props.columns.includes(c.key)));
const canSelect = computed(() => props.user.role !== 'viewer');
const allSelected = computed(() => !!props.items.length && props.items.every(a => props.selectedCodes.includes(a.asset_code)));
const someSelected = computed(() => props.items.some(a => props.selectedCodes.includes(a.asset_code)) && !allSelected.value);
const pages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));
const pageInput = ref('');
function jump() { const n = Number(pageInput.value); if (Number.isInteger(n) && n >= 1 && n <= pages.value) emit('page', n); pageInput.value = ''; }
function actions(asset: Asset) { const priority: FlowType[] = ['领用', '归还', '借出归还', '交接', '借出', '维修', '调拨', '报废', '报失']; const allowed = allowedAssetFlows([asset], props.user); return priority.filter(type => allowed.includes(type)).slice(0, 2); }
function value(asset: Asset, key: AssetColumn) { return (key === 'department' ? assetDepartmentLabel(asset) : asset[key]) || '—'; }
const labels: Partial<Record<FlowType, string>> = { 领用: '领用', 归还: '退库', 借出归还: '借出归还', 维修: '报修' };
</script>

<template>
  <div class="asset-table-region" :aria-busy="loading">
    <div class="asset-table-scroll" tabindex="0" aria-label="资产清单，可横向滚动查看更多列">
      <table class="workspace-table" :class="density" :style="{ minWidth: `${visible.reduce((sum, c) => sum + c.width, 152 + (canSelect ? 44 : 0))}px` }">
        <colgroup><col v-if="canSelect" style="width:44px" /><col v-for="col in visible" :key="col.key" :style="{ width: `${col.width}px` }" /><col style="width:152px" /></colgroup>
        <thead><tr>
          <th v-if="canSelect" class="selection-column"><label class="checkbox-hit"><input type="checkbox" aria-label="选择本页全部资产" :checked="allSelected" :indeterminate="someSelected" :disabled="loading || !items.length" @change="emit('togglePage')" /></label></th>
          <th v-for="col in visible" :key="col.key" :class="{ 'asset-code-column': col.key === 'asset_code', 'has-selection': canSelect }" scope="col" :aria-sort="sort === col.key ? (direction === 'asc' ? 'ascending' : 'descending') : 'none'"><button v-if="'sortable' in col && col.sortable" class="column-sort" :disabled="loading" @click="emit('sort', col.key)">{{ col.label }}<span class="sort-arrows" :class="{ active: sort === col.key }">{{ sort === col.key ? direction === 'asc' ? '↑' : '↓' : '↕' }}</span></button><span v-else>{{ col.label }}</span></th>
          <th class="asset-actions-column" scope="col">操作</th>
        </tr></thead>
        <tbody v-if="loading" class="table-skeleton"><tr v-for="n in 7" :key="n"><td :colspan="visible.length + (canSelect ? 2 : 1)"><span></span></td></tr></tbody>
        <tbody v-else-if="!items.length"><tr><td class="workspace-empty" :colspan="visible.length + (canSelect ? 2 : 1)"><AppIcon name="search" :size="32" /><strong>没有找到符合条件的资产</strong><span>可以调整关键词或清除筛选条件。</span></td></tr></tbody>
        <tbody v-else><tr v-for="asset in items" :key="asset.asset_code" :class="{ 'is-selected': selectedCodes.includes(asset.asset_code) }" @click="emit('select', asset)">
          <td v-if="canSelect" class="selection-column" @click.stop><label class="checkbox-hit"><input type="checkbox" :aria-label="`选择资产 ${asset.asset_code}`" :checked="selectedCodes.includes(asset.asset_code)" @change="emit('toggle', asset)" /></label></td>
          <td v-for="col in visible" :key="col.key" :class="{ 'asset-code-column': col.key === 'asset_code', 'has-selection': canSelect }" :title="String(value(asset, col.key))"><button v-if="col.key === 'asset_code'" class="asset-code-link" @click.stop="emit('select', asset)">{{ asset.asset_code }}</button><span v-else-if="col.key === 'status'" class="asset-status" :class="`asset-status-${asset.status}`"><i></i>{{ asset.status }}</span><span v-else>{{ value(asset, col.key) }}</span></td>
          <td class="asset-actions-column" @click.stop><div class="asset-row-actions"><template v-if="actions(asset).length"><button v-for="type in actions(asset)" :key="type" :aria-label="`${labels[type] || type} ${asset.asset_code}`" @click="emit('flow', [asset], type)">{{ labels[type] || type }}</button></template><button v-else @click="emit('select', asset)">详情</button></div></td>
        </tr></tbody>
      </table>
    </div>
    <div class="asset-mobile-list"><div v-if="loading" class="workspace-empty" role="status">正在加载资产…</div><div v-else-if="!items.length" class="workspace-empty"><AppIcon name="search" :size="32" /><strong>未找到资产</strong><span>请调整搜索条件。</span></div><article v-for="asset in loading ? [] : items" :key="asset.asset_code" class="asset-mobile-card" :class="{ 'is-selected': selectedCodes.includes(asset.asset_code) }"><header><label v-if="canSelect" class="checkbox-hit"><input type="checkbox" :aria-label="`选择资产 ${asset.asset_code}`" :checked="selectedCodes.includes(asset.asset_code)" @change="emit('toggle', asset)" /></label><button class="asset-code-link" @click="emit('select', asset)">{{ asset.asset_code }}</button><span class="asset-status" :class="`asset-status-${asset.status}`"><i></i>{{ asset.status }}</span></header><button class="asset-mobile-main" @click="emit('select', asset)"><strong>{{ asset.name || '未命名资产' }}</strong><span>{{ asset.model || asset.category_path || '未设置型号' }}</span></button><dl><div><dt>公司</dt><dd>{{ asset.company || '—' }}</dd></div><div><dt>使用人</dt><dd>{{ asset.assignee || '未分配' }}<span v-if="assetDepartmentLabel(asset)"> · {{ assetDepartmentLabel(asset) }}</span></dd></div><div><dt>位置</dt><dd>{{ asset.location || '—' }}</dd></div></dl><footer><button class="secondary-button" @click="emit('select', asset)">查看详情</button><button v-for="type in actions(asset)" :key="type" class="secondary-button" @click="emit('flow', [asset], type)">{{ labels[type] || type }}</button></footer></article></div>
    <div class="workspace-pagination"><span class="pagination-count">共 <strong>{{ total.toLocaleString('zh-CN') }}</strong> 项<span v-if="total">，第 {{ (page - 1) * pageSize + 1 }}–{{ Math.min(page * pageSize, total) }} 项</span></span><div class="pagination-controls"><label class="page-size-label"><span class="sr-only">每页条数</span><select :value="pageSize" :disabled="loading" @change="emit('pageSize', Number(($event.target as HTMLSelectElement).value))"><option :value="20">20 条 / 页</option><option :value="50">50 条 / 页</option><option :value="100">100 条 / 页</option></select></label><button :disabled="loading || page <= 1" aria-label="上一页" @click="emit('page', page - 1)"><AppIcon class="previous-icon" name="chevron" :size="14" /></button><template v-for="(n, index) in pageNumbers(page, pages)" :key="`${index}-${n}`"><span v-if="n === '…'" class="pagination-ellipsis">…</span><button v-else class="page-number" :class="{ active: page === n }" :disabled="loading" :aria-label="`第 ${n} 页`" :aria-current="page === n ? 'page' : undefined" @click="emit('page', n)">{{ n }}</button></template><button :disabled="loading || page >= pages" aria-label="下一页" @click="emit('page', page + 1)"><AppIcon name="chevron" :size="14" /></button><form class="page-jump" @submit.prevent="jump"><label>前往 <input v-model="pageInput" inputmode="numeric" type="number" min="1" :max="pages" aria-label="跳转页码" :placeholder="String(page)" :disabled="loading" /> 页</label><button class="sr-only" :disabled="loading">跳转</button></form></div></div>
  </div>
</template>

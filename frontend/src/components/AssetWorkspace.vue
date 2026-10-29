<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { api } from '../api';
import type { Asset, AuthUser } from '../types';
import { assetStatuses } from '../../shared/assets';
import { allowedAssetFlows, assetColumns, assetQuery, blankAssetFilters, normalizeColumns, type AssetColumn, type AssetFilters, type AssetOptions } from '../assetWorkspace';
import type { FlowType } from '../../shared/flows';
import AssetTable from './AssetTable.vue';
import AppIcon from './AppIcon.vue';
import ExportButton from './ExportButton.vue';

const props = defineProps<{ user: AuthUser; scope: string; reload: number; importBusy: boolean }>();
const emit = defineEmits<{ select: [asset: Asset]; flow: [assets: Asset[], type: FlowType]; import: []; create: []; success: [message: string] }>();
const items = ref<Asset[]>([]); const total = ref(0); const page = ref(1); const pageSize = ref(50);
const loading = ref(true); const error = ref(''); const selectionMessage = ref('');
const options = ref<AssetOptions>({ total: 0, statuses: [], locations: [], companies: [], departments: [], categories: [] });
const filters = reactive(blankAssetFilters()); const applied = ref<AssetFilters>(blankAssetFilters());
const advanced = ref(false); const sort = ref('updated_at_system'); const direction = ref('desc');
const columns = ref<AssetColumn[]>(normalizeColumns(null)); const density = ref('compact');
const columnDialog = ref<HTMLDialogElement | null>(null); const chosen = ref(new Map<string, Asset>());
const preferencesKey = `asset-table-preferences-v1:${props.user.id}`;
const searchFields = [{ value: 'all', label: '全部字段' }, { value: 'asset_code', label: '资产编码' }, { value: 'name', label: '资产名称' }, { value: 'serial_number', label: '设备序列号' }, { value: 'assignee', label: '使用人' }, { value: 'department', label: '使用部门' }, { value: 'company', label: '所属公司' }, { value: 'model', label: '型号' }];
const filterLabels: Record<string, string> = { status: '状态', category: '分类', company: '公司', location: '位置', department: '部门', search: '关键词' };
const chips = computed(() => Object.entries(applied.value).filter(([key, value]) => value && key !== 'searchField').map(([key, value]) => ({ key: key as keyof AssetFilters, label: filterLabels[key], value })));
const activeFilters = computed(() => chips.value.length);
const selectedAssets = computed(() => [...chosen.value.values()]);
const selectedCodes = computed(() => [...chosen.value.keys()]);
const batchTypes = computed(() => allowedAssetFlows(selectedAssets.value, props.user));
const canWrite = computed(() => ['admin', 'asset_manager'].includes(props.user.role));
const canRequest = computed(() => props.user.role !== 'viewer');
const exportPath = computed(() => `/api/exports/assets?${assetQuery(applied.value, props.scope, sort.value, direction.value)}`);
const selectedExportPath = computed(() => { const q = assetQuery(blankAssetFilters(), props.scope, sort.value, direction.value); for (const code of selectedCodes.value) q.append('code', code); return `/api/exports/assets?${q}`; });
let sequence = 0; let disposed = false;

try { const saved = JSON.parse(localStorage.getItem(preferencesKey) || 'null'); if (saved) { columns.value = normalizeColumns(saved.columns); density.value = saved.density === 'comfortable' ? 'comfortable' : 'compact'; if ([20, 50, 100].includes(saved.pageSize)) pageSize.value = saved.pageSize; } } catch { /* Defaults remain usable when storage is unavailable. */ }
function persist() { try { localStorage.setItem(preferencesKey, JSON.stringify({ columns: columns.value, density: density.value, pageSize: pageSize.value })); } catch { /* Preference persistence is optional. */ } }
watch([columns, density, pageSize], persist, { deep: true });
async function load() {
  const current = ++sequence; loading.value = true; error.value = ''; selectionMessage.value = '';
  try {
    const result = await api.listAssets({ ...applied.value, scope: props.scope, sort: sort.value, direction: direction.value, page: page.value, pageSize: pageSize.value });
    if (disposed || current !== sequence) return;
    if (page.value > 1 && (page.value - 1) * pageSize.value >= result.total) { page.value = Math.max(1, Math.ceil(result.total / pageSize.value)); await load(); return; }
    items.value = result.items; total.value = result.total;
    for (const asset of result.items) if (chosen.value.has(asset.asset_code)) chosen.value.set(asset.asset_code, asset);
  } catch (e) { if (!disposed && current === sequence) { items.value = []; total.value = 0; error.value = e instanceof Error ? e.message : '资产加载失败'; } }
  finally { if (!disposed && current === sequence) loading.value = false; }
}
async function loadOptions() { try { const result = await api.assetOptions(props.scope); if (!disposed) options.value = result; } catch (e) { if (!disposed) error.value = e instanceof Error ? e.message : '筛选选项加载失败'; } }
async function refresh() { chosen.value.clear(); await Promise.all([load(), loadOptions()]); }
function apply() { applied.value = { ...filters, search: filters.search.trim() }; page.value = 1; chosen.value.clear(); void load(); }
function clearFilters() { Object.assign(filters, blankAssetFilters()); apply(); }
function removeFilter(key: keyof AssetFilters) { filters[key] = ''; applied.value = { ...applied.value, [key]: '' }; page.value = 1; chosen.value.clear(); void load(); }
function statusTab(status: string) { filters.status = status; apply(); }
function sortColumn(column: string) { direction.value = sort.value === column && direction.value === 'asc' ? 'desc' : 'asc'; sort.value = column; page.value = 1; void load(); }
function toggle(asset: Asset) {
  selectionMessage.value = '';
  if (chosen.value.has(asset.asset_code)) chosen.value.delete(asset.asset_code);
  else if (chosen.value.size >= 100) selectionMessage.value = '每次最多选择 100 项资产，请先处理当前选择。';
  else chosen.value.set(asset.asset_code, asset);
}
function togglePage() {
  const all = items.value.every(a => chosen.value.has(a.asset_code));
  for (const asset of items.value) { if (all) chosen.value.delete(asset.asset_code); else if (!chosen.value.has(asset.asset_code)) toggle(asset); }
}
function startFlow(assets: Asset[], type: FlowType) { if (!loading.value && !error.value && allowedAssetFlows(assets, props.user).includes(type)) emit('flow', assets, type); }
function changePage(next: number) { page.value = next; void load(); }
function changePageSize(size: number) { pageSize.value = size; page.value = 1; void load(); }
function toggleColumn(key: AssetColumn) { if (key === 'asset_code') return; columns.value = columns.value.includes(key) ? columns.value.filter(k => k !== key) : [...columns.value, key]; }
async function showColumns() { await nextTick(); columnDialog.value?.showModal(); }
watch(() => props.reload, refresh);
onMounted(refresh);
onBeforeUnmount(() => { disposed = true; sequence++; });
</script>

<template>
  <div class="asset-workspace">
    <p v-if="['employee', 'approver'].includes(user.role)" class="asset-scope-notice" role="status">{{ user.employee_id ? '仅显示本人领用的资产。需要领用新资产时，请联系资产管理员。' : '账号尚未关联员工身份，请联系管理员关联后查看本人资产。' }}</p>
    <div class="workspace-mobile-status"><span>{{ scope === 'mine' ? '我的资产' : '全部资产' }} <strong>{{ options.total.toLocaleString('zh-CN') }}</strong></span><label><span class="sr-only">按资产状态筛选</span><select :value="applied.status" @change="statusTab(($event.target as HTMLSelectElement).value)"><option value="">全部状态</option><option v-for="status in assetStatuses" :key="status" :value="status">{{ status }}（{{ options.statuses.find(s => s.name === status)?.count ?? 0 }}）</option></select></label></div>
    <section class="workspace-status-tabs" aria-label="按资产状态筛选"><button :class="{ active: !applied.status }" @click="statusTab('')"><span>全部资产</span><strong>{{ options.total.toLocaleString('zh-CN') }}</strong></button><button v-for="status in assetStatuses" :key="status" :class="{ active: applied.status === status }" @click="statusTab(status)"><span>{{ status }}</span><strong>{{ options.statuses.find(s => s.name === status)?.count ?? 0 }}</strong></button></section>
    <section class="panel workspace-panel">
      <div class="workspace-toolbar">
        <form class="workspace-search" @submit.prevent="apply"><label class="sr-only" for="asset-search-field">搜索范围</label><select id="asset-search-field" v-model="filters.searchField"><option v-for="field in searchFields" :key="field.value" :value="field.value">{{ field.label }}</option></select><label class="sr-only" for="asset-keyword">搜索资产</label><input id="asset-keyword" v-model="filters.search" type="search" maxlength="200" placeholder="输入编码、名称、使用人等关键词" /><button type="submit" :disabled="loading" aria-label="搜索资产"><AppIcon name="search" /><span>搜索</span></button></form>
        <button class="workspace-filter-toggle" :class="{ active: advanced || activeFilters }" :aria-expanded="advanced" aria-controls="asset-filter-fields" @click="advanced = !advanced"><AppIcon name="filter" :size="16" />筛选<span v-if="activeFilters" class="filter-count">{{ activeFilters }}</span><AppIcon name="chevron" :size="12" :class="{ rotated: advanced }" /></button>
        <div class="workspace-main-actions"><ExportButton :path="exportPath" label="导出资产" :disabled="loading || !!error" @success="emit('success', $event)" @error="error = $event" /><button v-if="canWrite" class="primary-button workspace-icon-button create-asset-button" @click="emit('create')"><AppIcon name="plus" :size="17" />新增资产</button><button v-if="canWrite" class="secondary-button workspace-icon-button import-asset-button" :disabled="importBusy" @click="emit('import')"><AppIcon name="upload" :size="16" />{{ importBusy ? '处理中…' : '导入资产' }}</button></div>
      </div>
      <form v-if="advanced" id="asset-filter-fields" class="workspace-filter-fields" @submit.prevent="apply"><label class="field"><span>所属公司</span><select v-model="filters.company"><option value="">全部公司</option><option v-for="item in options.companies.filter(v => v.name)" :key="item.name" :value="item.name">{{ item.name }}（{{ item.count }}）</option></select></label><label class="field"><span>资产分类</span><select v-model="filters.category"><option value="">全部分类</option><option v-for="item in options.categories.filter(v => v.name)" :key="item.name" :value="item.name">{{ item.name }}（{{ item.count }}）</option></select></label><label class="field"><span>所在位置</span><select v-model="filters.location"><option value="">全部位置</option><option v-for="item in options.locations.filter(v => v.name)" :key="item.name" :value="item.name">{{ item.name }}（{{ item.count }}）</option></select></label><label class="field"><span>使用部门</span><select v-model="filters.department"><option value="">全部部门</option><option v-for="item in options.departments.filter(v => v.name)" :key="item.name" :value="item.name">{{ item.name }}（{{ item.count }}）</option></select></label><div class="workspace-filter-actions"><button type="button" class="secondary-button" :disabled="loading" @click="clearFilters">重置</button><button class="primary-button" :disabled="loading">应用筛选</button></div></form>
      <div v-if="chips.length" class="workspace-filter-chips"><span>当前筛选</span><button v-for="chip in chips" :key="chip.key" :title="chip.value" :disabled="loading" :aria-label="`移除${chip.label}筛选`" @click="removeFilter(chip.key)"><span>{{ chip.label }}：{{ chip.value }}</span><AppIcon name="close" :size="12" /></button><button class="clear-all-filters" :disabled="loading" @click="clearFilters">清空筛选</button></div>
      <p v-if="scope === 'mine' && !user.employee_id" class="directory-notice">当前账号尚未关联员工身份。使用已关联的员工账号登录后，可查看本人资产。</p>
      <div v-if="error" class="workspace-error" role="alert">{{ error }}<button @click="refresh">重新加载</button></div>
      <div class="workspace-table-toolbar"><div class="workspace-list-summary"><strong>{{ scope === 'mine' ? '我的资产' : '资产台账' }}</strong><span>{{ total.toLocaleString('zh-CN') }} 项资产</span><span v-if="!loading" class="workspace-live-label"><i></i>已更新</span></div><div class="workspace-view-actions"><span class="workspace-selection-hint" v-if="canRequest && !selectedAssets.length">勾选资产可批量操作</span><label class="density-picker"><span class="sr-only">表格密度</span><select v-model="density"><option value="compact">紧凑</option><option value="comfortable">舒适</option></select></label><button class="workspace-columns-button" @click="showColumns"><AppIcon name="columns" :size="16" />显示列</button><button class="workspace-refresh-button" aria-label="刷新资产清单" title="刷新资产清单" :disabled="loading" @click="refresh"><AppIcon name="refresh" :size="16" /></button></div></div>
      <div v-if="selectedAssets.length" class="workspace-batch-toolbar"><span>已选 <strong>{{ selectedAssets.length }}</strong> 项<span class="batch-selection-detail">（可跨页选择，上限 100 项）</span></span><div class="batch-actions"><button v-for="type in batchTypes" :key="type" :disabled="loading || !!error" @click="startFlow(selectedAssets, type)">批量{{ type === '归还' ? '退库' : type === '维修' ? '报修' : type }}</button><ExportButton :path="selectedExportPath" label="导出所选" :disabled="loading || !!error" description="仅导出已勾选的资产" @success="emit('success', $event)" @error="error = $event" /><button class="clear-selection" @click="chosen.clear()">清空选择</button></div><p v-if="!batchTypes.length">所选资产没有可共同执行的流转类型，可调整选择后操作。</p></div>
      <p v-if="selectionMessage" class="workspace-selection-message" role="status">{{ selectionMessage }}</p>
      <AssetTable :items="items" :total="total" :page="page" :page-size="pageSize" :loading="loading" :columns="columns" :density="density" :selected-codes="selectedCodes" :user="user" :sort="sort" :direction="direction" @select="emit('select', $event)" @flow="startFlow" @page="changePage" @page-size="changePageSize" @sort="sortColumn" @toggle="toggle" @toggle-page="togglePage" />
    </section>
    <p class="workspace-footnote"><AppIcon name="check" :size="14" />流转操作提交审批后生效，操作记录自动保留。导出范围与当前筛选一致。</p>
    <dialog ref="columnDialog" class="workspace-column-dialog" aria-labelledby="column-dialog-title"><form @submit.prevent="columnDialog?.close()"><div class="column-dialog-heading"><div><span class="eyebrow">个人显示偏好</span><h2 id="column-dialog-title">自定义显示列</h2></div><button class="workspace-refresh-button" type="button" aria-label="关闭列设置" @click="columnDialog?.close()"><AppIcon name="close" /></button></div><p>选择需要在电脑表格中查看的信息，设置仅保存在当前浏览器。</p><div class="workspace-column-choices"><label v-for="col in assetColumns" :key="col.key"><input type="checkbox" :checked="columns.includes(col.key)" :disabled="col.key === 'asset_code'" @change="toggleColumn(col.key)" /><span>{{ col.label }}</span><small v-if="col.key === 'asset_code'">固定</small></label></div><div class="company-form-actions form-actions"><button class="secondary-button" type="button" @click="columns = normalizeColumns(null)">恢复默认</button><button class="primary-button">完成</button></div></form></dialog>
  </div>
</template>

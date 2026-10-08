<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { api } from './api';
import type { Asset, AuthUser, ImportResponse, ManagedUser, Stats } from './types';
import SidebarNav from './components/SidebarNav.vue';
import MetricCard from './components/MetricCard.vue';
import AssetTable from './components/AssetTable.vue';
import AssetDetailDrawer from './components/AssetDetailDrawer.vue';
import AssetEventPanel from './components/AssetEventPanel.vue';
import QrLookup from './components/QrLookup.vue';
import LoginView from './components/LoginView.vue';
import UserSettingsPanel from './components/UserSettingsPanel.vue';
import InventoryPanel from './components/InventoryPanel.vue';

const activeView = ref('dashboard');
const stats = ref<Stats>({ total: 0, status: {}, categories: [], locations: [], companies: [], amounts: { taxed: 0, untaxed: 0, tax: 0 } });
const assets = ref<Asset[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 20;
const search = ref('');
const statusFilter = ref('');
const locationFilter = ref('');
const selected = ref<Asset | null>(null);
const flowAsset = ref<Asset | null>(null);
const loading = ref(false);
const error = ref('');
const importInput = ref<HTMLInputElement | null>(null);
const importPreview = ref<ImportResponse | null>(null);
const importBusy = ref(false);
const eventBusy = ref(false);
const editBusy = ref(false);
const toast = ref('');
const currentUser = ref<AuthUser | null>(null);
const authReady = ref(false);
const users = ref<ManagedUser[]>([]);
const userBusy = ref(false);

const inUse = computed(() => stats.value.status['在用'] ?? 0);
const idle = computed(() => stats.value.status['空闲'] ?? 0);
const canWrite = computed(() => currentUser.value?.role === 'admin' || currentUser.value?.role === 'asset_manager');
const canManageUsers = computed(() => currentUser.value?.role === 'admin');
const activeTitle = computed(() => ({ dashboard: '经营分析看板', assets: '资产清单', scan: '扫码查询', events: '资产流转', inventory: '资产盘点', settings: '系统设置' }[activeView.value] ?? '资产管理系统'));

async function loadStats() { stats.value = await api.getStats(); }
async function loadAssets() {
  loading.value = true;
  try {
    const result = await api.listAssets({ search: search.value, status: statusFilter.value, location: locationFilter.value, page: page.value, pageSize });
    assets.value = result.items; total.value = result.total;
  } catch (err) { error.value = err instanceof Error ? err.message : '资产列表加载失败'; }
  finally { loading.value = false; }
}
async function refresh() { error.value = ''; try { await Promise.all([loadStats(), loadAssets()]); } catch (err) { error.value = err instanceof Error ? err.message : '数据加载失败'; } }
async function loadUsers() {
  if (!canManageUsers.value) { users.value = []; return; }
  try { users.value = (await api.listUsers()).users; }
  catch (err) { error.value = err instanceof Error ? err.message : '用户列表加载失败'; }
}
async function bootstrapAuth() {
  try { currentUser.value = await api.me(); await refresh(); }
  catch { localStorage.removeItem('asset_token'); }
  finally { authReady.value = true; }
}
async function onLoggedIn(user: AuthUser) { currentUser.value = user; await refresh(); }
async function logout() { await api.logout(); currentUser.value = null; selected.value = null; flowAsset.value = null; }
async function createUser(payload: { username: string; password: string; display_name: string; role: ManagedUser['role'] }) {
  userBusy.value = true;
  try { await api.createUser(payload); await loadUsers(); toast.value = '用户创建成功'; window.setTimeout(() => { toast.value = ''; }, 2400); }
  catch (err) { error.value = err instanceof Error ? err.message : '用户创建失败'; }
  finally { userBusy.value = false; }
}
function showToast(message: string) { toast.value = message; globalThis.setTimeout(() => { toast.value = ''; }, 2400); }
async function selectAsset(asset: Asset) { try { selected.value = await api.getAsset(asset.asset_code); } catch (err) { error.value = err instanceof Error ? err.message : '资产详情加载失败'; } }
async function lookupAsset(payload: { raw: string; assetCode: string }) {
  try {
    await api.saveQr(payload.assetCode, payload.raw);
    await selectAsset({ asset_code: payload.assetCode } as Asset);
  } catch (err) { error.value = err instanceof Error ? err.message : '二维码保存失败'; }
}
async function submitEvent(payload: Record<string, unknown>) {
  if (!selected.value) return;
  eventBusy.value = true;
  try {
    const result = await api.createEvent(selected.value.asset_code, { ...payload, operatorName: '当前管理员', requestId: crypto.randomUUID() });
    selected.value = result.asset; toast.value = result.replayed ? '操作已处理' : '资产操作成功'; await refresh();
    window.setTimeout(() => { toast.value = ''; }, 2400);
  } catch (err) { error.value = err instanceof Error ? err.message : '资产操作失败'; }
  finally { eventBusy.value = false; }
}
async function selectFlowAsset(asset: Asset) {
  try { flowAsset.value = await api.getAsset(asset.asset_code); }
  catch (err) { error.value = err instanceof Error ? err.message : '资产详情加载失败'; }
}
async function submitFlowEvent(payload: Record<string, unknown>) {
  if (!flowAsset.value) return;
  eventBusy.value = true;
  try {
    const result = await api.createEvent(flowAsset.value.asset_code, { ...payload, operatorName: '当前管理员', requestId: crypto.randomUUID() });
    flowAsset.value = result.asset;
    toast.value = result.replayed ? '操作已处理' : '资产操作成功';
    await refresh();
    window.setTimeout(() => { toast.value = ''; }, 2400);
  } catch (err) { error.value = err instanceof Error ? err.message : '资产操作失败'; }
  finally { eventBusy.value = false; }
}
async function updateAsset(payload: Record<string, unknown>) {
  if (!selected.value) return;
  editBusy.value = true;
  try {
    const result = await api.updateAsset(selected.value.asset_code, payload as any);
    selected.value = result.asset;
    toast.value = '资产资料保存成功';
    await refresh();
    window.setTimeout(() => { toast.value = ''; }, 2400);
  } catch (err) { error.value = err instanceof Error ? err.message : '资产资料保存失败'; }
  finally { editBusy.value = false; }
}
async function onImportFile(event: Event) {
  const input = event.target as HTMLInputElement; const file = input.files?.[0]; if (!file) return;
  importBusy.value = true;
  try { importPreview.value = await api.previewImport(file); } catch (err) { error.value = err instanceof Error ? err.message : '导入预览失败'; }
  finally { importBusy.value = false; input.value = ''; }
}
async function confirmImport() {
  if (!importPreview.value) return;
  importBusy.value = true;
  try { const result = await api.confirmImport(importPreview.value); importPreview.value = null; toast.value = `导入完成：新增 ${result.newRows} 条，更新 ${result.updatedRows} 条`; await refresh(); window.setTimeout(() => { toast.value = ''; }, 3200); }
  catch (err) { error.value = err instanceof Error ? err.message : '导入失败'; }
  finally { importBusy.value = false; }
}
function navigate(view: string) { activeView.value = view; if (view === 'events') { selected.value = null; flowAsset.value = null; } if (view === 'assets') { flowAsset.value = null; } if (view === 'settings') void loadUsers(); if (view === 'assets' || view === 'events') void loadAssets(); }
function applyFilters() { page.value = 1; void loadAssets(); }
onMounted(bootstrapAuth);
</script>

<template>
  <LoginView v-if="authReady && !currentUser" @logged-in="onLoggedIn" />
  <div v-else-if="authReady" class="app-shell">
    <SidebarNav :active="activeView" @navigate="navigate" />
    <main class="main-shell">
      <header class="topbar"><div class="tenant"><span class="tenant-badge">资产管理</span><strong>重庆市九吨科技有限公司</strong><span class="chevron">⌄</span></div><div class="top-actions"><span>{{ currentUser?.display_name }} · {{ currentUser?.role === 'viewer' ? '只读' : '可操作' }}</span><button class="avatar" :title="'退出登录'" @click="logout">{{ currentUser?.display_name?.slice(0, 1) }}</button></div></header>
      <div class="page-heading"><div><span class="eyebrow">资产管理平台</span><h1>{{ activeTitle }}</h1></div><div class="heading-actions"><input ref="importInput" type="file" accept=".xlsx,.xls" class="hidden-input" @change="onImportFile" /><button v-if="canWrite" class="secondary-button" :disabled="importBusy" @click="importInput?.click()">{{ importBusy ? '处理中…' : '导入 Excel' }}</button><button class="primary-button" @click="activeView = 'scan'">扫码查询</button></div></div>
      <div v-if="error" class="error-banner">{{ error }} <button @click="error = ''">×</button></div><div v-if="toast" class="toast">{{ toast }}</div>

      <section v-if="activeView === 'dashboard'" class="content-area">
        <div class="metrics-grid"><MetricCard label="资产总数" :value="stats.total" tone="blue" hint="当前资产台账" /><MetricCard label="在用资产" :value="inUse" tone="green" :hint="stats.total ? `${Math.round(inUse / stats.total * 100)}% 使用中` : '暂无数据'" /><MetricCard label="空闲资产" :value="idle" tone="orange" :hint="stats.total ? `${Math.round(idle / stats.total * 100)}% 可分配` : '暂无数据'" /><MetricCard label="资产原值" :value="`¥ ${stats.amounts.taxed.toLocaleString('zh-CN')}`" tone="purple" hint="含税金额合计" /></div>
        <div class="dashboard-grid"><article class="panel wide-panel"><div class="panel-header"><div><span class="eyebrow">状态分布</span><h2>资产使用情况</h2></div><button class="text-button" @click="navigate('assets')">查看清单 →</button></div><div class="status-visual"><div class="donut" :style="{ '--ratio': `${stats.total ? inUse / stats.total * 100 : 0}%` }"><div><strong>{{ stats.total }}</strong><span>项资产</span></div></div><div class="legend"><div><i class="legend-dot green"></i><span>在用</span><strong>{{ inUse }}</strong></div><div><i class="legend-dot orange"></i><span>空闲</span><strong>{{ idle }}</strong></div><div v-for="(count, name) in stats.status" v-if="name !== '在用' && name !== '空闲'" :key="name"><i class="legend-dot gray"></i><span>{{ name }}</span><strong>{{ count }}</strong></div></div></div></article><article class="panel"><div class="panel-header"><div><span class="eyebrow">分类统计</span><h2>资产分类</h2></div><span class="panel-count">{{ stats.categories.length }} 类</span></div><div class="bar-list"><div v-for="item in stats.categories.slice(0, 6)" :key="item.name" class="bar-row"><div class="bar-label"><span>{{ item.name }}</span><strong>{{ item.count }}</strong></div><div class="bar-track"><i :style="{ width: `${Math.max(4, item.count / Math.max(stats.categories[0]?.count ?? 1, 1) * 100)}%` }"></i></div></div></div></article></div>
        <div class="dashboard-grid three"><article class="panel"><div class="panel-header"><div><span class="eyebrow">位置统计</span><h2>楼层分布</h2></div></div><div class="simple-list"><div v-for="item in stats.locations.slice(0, 6)" :key="item.name"><span>{{ item.name }}</span><strong>{{ item.count }}</strong></div></div></article><article class="panel"><div class="panel-header"><div><span class="eyebrow">公司统计</span><h2>所属公司</h2></div></div><div class="simple-list"><div v-for="item in stats.companies.slice(0, 6)" :key="item.name"><span class="truncate">{{ item.name }}</span><strong>{{ item.count }}</strong></div></div></article><article class="panel quick-panel"><span class="eyebrow">快捷操作</span><h2>开始管理资产</h2><button class="quick-action" @click="navigate('assets')">查看资产清单 <span>→</span></button><button class="quick-action" @click="navigate('scan')">扫描二维码查询 <span>⌕</span></button><button class="quick-action" @click="importInput?.click()">批量导入 Excel <span>↑</span></button></article></div>
      </section>

      <section v-else-if="activeView === 'assets'" class="content-area"><div class="panel list-panel"><div class="filter-row"><div class="search-box"><span>⌕</span><input v-model="search" placeholder="搜索资产编码、名称、型号或序列号" @keyup.enter="applyFilters" /><button @click="applyFilters">搜索</button></div><select v-model="statusFilter" @change="applyFilters"><option value="">全部状态</option><option value="在用">在用</option><option value="空闲">空闲</option><option value="借出">借出</option><option value="维修">维修</option></select><select v-model="locationFilter" @change="applyFilters"><option value="">全部位置</option><option v-for="item in stats.locations" :key="item.name" :value="item.name">{{ item.name }}</option></select><button class="secondary-button" @click="refresh">刷新</button></div><div class="list-summary"><span>资产清单</span><strong>{{ total }}</strong><span>项</span></div><AssetTable :items="assets" :total="total" :page="page" :page-size="pageSize" :loading="loading" @select="selectAsset" @page="(next) => { page = next; loadAssets(); }" /></div></section>

      <section v-else-if="activeView === 'events'" class="content-area"><div class="flow-grid"><div class="panel list-panel"><div class="filter-row"><div class="search-box"><span>⌕</span><input v-model="search" placeholder="搜索要操作的资产" @keyup.enter="applyFilters" /><button @click="applyFilters">搜索</button></div><select v-model="statusFilter" @change="applyFilters"><option value="">全部状态</option><option value="在用">在用</option><option value="空闲">空闲</option><option value="借出">借出</option><option value="维修">维修</option></select><button class="secondary-button" @click="refresh">刷新</button></div><div class="list-summary"><span>可操作资产</span><strong>{{ total }}</strong><span>项</span></div><AssetTable :items="assets" :total="total" :page="page" :page-size="pageSize" :loading="loading" @select="selectFlowAsset" @page="(next) => { page = next; loadAssets(); }" /></div><AssetEventPanel :asset="flowAsset" :busy="eventBusy" :can-write="canWrite" @submit="submitFlowEvent" /></div></section>

      <section v-else-if="activeView === 'scan'" class="content-area scan-view"><QrLookup @lookup="lookupAsset" /></section>
      <section v-else-if="activeView === 'inventory'" class="content-area"><InventoryPanel :locations="stats.locations" :can-write="canWrite" @error="(message) => { error = message; }" @success="showToast" /></section>
      <section v-else-if="activeView === 'settings'" class="content-area"><UserSettingsPanel :users="users" :can-write="canManageUsers" :busy="userBusy" @create="createUser" /></section>
      <section v-else class="content-area"><div class="panel placeholder-panel"><span class="eyebrow">{{ activeTitle }}</span><h2>这个模块将在下一阶段接入</h2><p>一期核心功能已经包含资产台账、二维码解析、Excel 导入、看板统计和流转记录。</p></div></section>
    </main>
    <AssetDetailDrawer :asset="selected" :busy="eventBusy" :edit-busy="editBusy" :can-write="canWrite" @close="selected = null" @event="submitEvent" @update="updateAsset" />
    <div v-if="importPreview" class="modal-backdrop"><div class="import-modal"><span class="eyebrow">导入预览</span><h2>{{ importPreview.fileName }}</h2><p>确认后将按资产编码更新台账，空值不会覆盖已有信息。</p><div class="import-stats"><div><strong>{{ importPreview.preview.newRows }}</strong><span>新增</span></div><div><strong>{{ importPreview.preview.updatedRows }}</strong><span>更新</span></div><div><strong>{{ importPreview.preview.unchangedRows }}</strong><span>无变化</span></div><div class="danger"><strong>{{ importPreview.preview.invalidRows }}</strong><span>异常</span></div></div><div class="form-actions"><button class="secondary-button" @click="importPreview = null">取消</button><button class="primary-button" :disabled="importBusy" @click="confirmImport">{{ importBusy ? '导入中…' : '确认导入' }}</button></div></div></div>
  </div>
</template>

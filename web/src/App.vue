<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { api, SESSION_EXPIRED_EVENT } from './api';
import { ApiError } from './apiClient';
import { parseQr } from '../../shared/qr';
import type { Asset, AuthUser, ImportResponse, ManagedUser, Stats } from './types';
import SidebarNav from './components/SidebarNav.vue';
import MetricCard from './components/MetricCard.vue';
import AssetTable from './components/AssetTable.vue';
import AssetDetailDrawer from './components/AssetDetailDrawer.vue';
import FlowPanel from './components/FlowPanel.vue';
import type { FlowType } from '../../shared/flows';
import QrLookup from './components/QrLookup.vue';
import LoginView from './components/LoginView.vue';
import UserSettingsPanel from './components/UserSettingsPanel.vue';
import InventoryPanel from './components/InventoryPanel.vue';
import DirectoryPanel from './components/DirectoryPanel.vue';
import { roleLabels } from './directory';

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
const flowDraft = ref<{ asset: Asset; type: FlowType; nonce: number } | null>(null);
const flowId = ref<number | null>(Number(new URLSearchParams(window.location.search).get('flow')) || null);
const loading = ref(false);
const error = ref('');
const importInput = ref<HTMLInputElement | null>(null);
const importPreview = ref<ImportResponse | null>(null);
const importDialog = ref<HTMLDialogElement | null>(null);
const importBusy = ref(false);
const editBusy = ref(false);
const toast = ref('');
const currentUser = ref<AuthUser | null>(null);
const authReady = ref(false);
const users = ref<ManagedUser[]>([]);
const userBusy = ref(false);
const userCreated = ref(0);
const authFailure = ref('');
let toastTimer: ReturnType<typeof setTimeout> | undefined;
let listRequest = 0;

const inUse = computed(() => stats.value.status['在用'] ?? 0);
const idle = computed(() => stats.value.status['空闲'] ?? 0);
const otherStatuses = computed(() => Object.entries(stats.value.status).filter(([name]) => name !== '在用' && name !== '空闲'));
const donutBackground = computed(() => {
  const ratio = stats.value.total ? inUse.value / stats.value.total * 100 : 0;
  const idleEnd = stats.value.total ? (inUse.value + idle.value) / stats.value.total * 100 : 0;
  return `conic-gradient(#4dcc9f 0% ${ratio}%, #ffb62d ${ratio}% ${idleEnd}%, #c3ccd6 ${idleEnd}% 100%)`;
});
const canWrite = computed(() => currentUser.value?.role === 'admin' || currentUser.value?.role === 'asset_manager');
const employeeOnly = computed(() => ['employee', 'approver'].includes(currentUser.value?.role ?? ''));
const canRequest = computed(() => !!currentUser.value && currentUser.value.role !== 'viewer');
const canManageUsers = computed(() => currentUser.value?.role === 'admin');
const activeTitle = computed(() => ({ dashboard: '经营分析看板', 'my-assets': '我的资产', assets: employeeOnly.value ? '可领用资产' : '资产清单', scan: '扫码查询', events: '资产流转', inventory: '资产盘点', settings: '系统设置' }[activeView.value] ?? '资产管理系统'));
watch(importPreview, async (preview) => { if (preview) { await nextTick(); if (importPreview.value && !importDialog.value?.open) importDialog.value?.showModal(); } });
function cancelImport(event: Event) { if (importBusy.value) event.preventDefault(); else importPreview.value = null; }

async function loadStats() { stats.value = await api.getStats(); }
async function loadAssets() {
  const requestId = ++listRequest;
  loading.value = true;
  try {
    const result = await api.listAssets({ search: search.value, status: statusFilter.value, location: locationFilter.value, page: page.value, pageSize, scope: activeView.value === 'my-assets' ? 'mine' : employeeOnly.value ? 'available' : '' });
    if (requestId === listRequest) { assets.value = result.items; total.value = result.total; }
  } catch (err) { if (requestId === listRequest) error.value = err instanceof Error ? err.message : '资产列表加载失败'; }
  finally { if (requestId === listRequest) loading.value = false; }
}
async function refresh() { error.value = ''; try { await Promise.all([loadStats(), loadAssets()]); } catch (err) { error.value = err instanceof Error ? err.message : '数据加载失败'; } }
async function loadUsers() {
  if (!canManageUsers.value) { users.value = []; return; }
  try { users.value = (await api.listUsers()).users; }
  catch (err) { error.value = err instanceof Error ? err.message : '用户列表加载失败'; }
}
async function bootstrapAuth() {
  authReady.value = false; authFailure.value = '';
  try { if (localStorage.getItem('asset_token')) { currentUser.value = await api.me(); await refresh(); if (flowId.value) activeView.value = 'events'; } }
  catch (err) { if (!(err instanceof ApiError && err.status === 401)) authFailure.value = err instanceof Error ? err.message : '登录状态检查失败'; }
  finally { authReady.value = true; }
}
async function onLoggedIn(user: AuthUser) { currentUser.value = user; await refresh(); if (flowId.value) activeView.value = 'events'; }
function resetSession() {
  listRequest++; currentUser.value = null; selected.value = null; flowDraft.value = null;
  flowId.value = Number(new URLSearchParams(window.location.search).get('flow')) || null;
  assets.value = []; total.value = 0; stats.value = { total: 0, status: {}, categories: [], locations: [], companies: [], amounts: { taxed: 0, untaxed: 0, tax: 0 } };
  activeView.value = 'dashboard'; search.value = ''; statusFilter.value = ''; locationFilter.value = ''; page.value = 1;
  importPreview.value = null; users.value = []; error.value = ''; toast.value = ''; authFailure.value = '';
  if (toastTimer) clearTimeout(toastTimer);
}
async function logout() { await api.logout(); resetSession(); }
function returnToLogin() { localStorage.removeItem('asset_token'); resetSession(); }
async function createUser(payload: { username: string; password: string; display_name: string; role: ManagedUser['role'] }) {
  userBusy.value = true;
  try { await api.createUser(payload); userCreated.value++; await loadUsers(); showToast('用户创建成功'); }
  catch (err) { error.value = err instanceof Error ? err.message : '用户创建失败'; }
  finally { userBusy.value = false; }
}
function showToast(message: string) { if (toastTimer) clearTimeout(toastTimer); toast.value = message; toastTimer = setTimeout(() => { toast.value = ''; }, 3200); }
async function selectAsset(asset: Asset) { try { selected.value = await api.getAsset(asset.asset_code); } catch (err) { error.value = err instanceof Error ? err.message : '资产详情加载失败'; } }
async function lookupAsset(payload: { raw: string; assetCode: string }) {
  try {
    selected.value = await api.getAsset(payload.assetCode);
    if (canWrite.value && parseQr(payload.raw).format !== 'asset_code') {
      const qr = await api.saveQr(payload.assetCode, payload.raw);
      if (selected.value?.asset_code === payload.assetCode) selected.value = { ...selected.value, qr_raw: qr.qr_raw, qr_format: qr.qr_format };
    }
  } catch (err) { error.value = err instanceof Error ? err.message : '资产查询失败'; }
}
function startFlow(payload: { type: FlowType }) {
  if (!selected.value) return;
  flowDraft.value = { asset: selected.value, type: payload.type, nonce: Date.now() };
  selected.value = null; flowId.value = null; activeView.value = 'events';
}
function openFlow(id: number) { flowDraft.value = null; selected.value = null; flowId.value = id; activeView.value = 'events'; }
async function updateAsset(payload: Record<string, unknown>) {
  if (!selected.value) return;
  editBusy.value = true;
  try {
    const result = await api.updateAsset(selected.value.asset_code, payload as any);
    selected.value = result.asset;
    showToast('资产资料保存成功');
    await refresh();
  } catch (err) { error.value = err instanceof Error ? err.message : '资产资料保存失败'; }
  finally { editBusy.value = false; }
}
async function onImportFile(event: Event) {
  const input = event.target as HTMLInputElement; const file = input.files?.[0]; if (!file) return;
  importBusy.value = true; error.value = '';
  try { importPreview.value = await api.previewImport(file); } catch (err) { error.value = err instanceof Error ? err.message : '导入预览失败'; }
  finally { importBusy.value = false; input.value = ''; }
}
async function confirmImport() {
  if (!importPreview.value) return;
  importBusy.value = true;
  try { const result = await api.confirmImport(importPreview.value); importPreview.value = null; showToast(`导入完成：新增 ${result.newRows} 条，更新 ${result.updatedRows} 条`); await refresh(); }
  catch (err) { error.value = err instanceof Error ? err.message : '导入失败'; }
  finally { importBusy.value = false; }
}
function navigate(view: string) {
  activeView.value = view;
  const url = new URL(window.location.href); url.searchParams.delete('flow'); window.history.replaceState(null, '', url);
  flowId.value = null; flowDraft.value = null;
  if (view === 'events') selected.value = null;
  if (view === 'settings') void loadUsers();
  if (view === 'assets' || view === 'my-assets') { page.value = 1; void loadAssets(); }
}
function applyFilters() { page.value = 1; void loadAssets(); }
onMounted(() => { window.addEventListener(SESSION_EXPIRED_EVENT, resetSession); void bootstrapAuth(); });
onBeforeUnmount(() => { window.removeEventListener(SESSION_EXPIRED_EVENT, resetSession); if (toastTimer) clearTimeout(toastTimer); });
</script>

<template>
  <div v-if="!authReady || authFailure" class="login-shell"><div class="login-card"><h1>{{ authFailure ? '连接暂时不可用' : '正在加载…' }}</h1><p class="login-intro" role="status">{{ authFailure || '正在检查登录状态' }}</p><div v-if="authFailure" class="form-actions"><button class="primary-button" @click="bootstrapAuth">重新连接</button><button class="secondary-button" @click="returnToLogin">返回登录</button></div></div></div>
  <LoginView v-else-if="!currentUser" @logged-in="onLoggedIn" />
  <div v-else-if="authReady" class="app-shell">
    <SidebarNav :active="activeView" :employee-only="employeeOnly" :can-manage="canManageUsers" @navigate="navigate" />
    <main class="main-shell">
      <header class="topbar"><div class="tenant"><span class="tenant-badge">资产管理</span><strong>重庆市九吨科技有限公司</strong><span class="chevron">⌄</span></div><div class="top-actions"><span>{{ currentUser?.display_name }} · {{ currentUser ? roleLabels[currentUser.role] : '' }}</span><button class="avatar" :title="'退出登录'" @click="logout">{{ currentUser?.display_name?.slice(0, 1) }}</button></div></header>
      <div class="page-heading"><div><span class="eyebrow">资产管理平台</span><h1>{{ activeTitle }}</h1></div><div class="heading-actions"><input ref="importInput" type="file" accept=".xlsx,.xls" class="hidden-input" @change="onImportFile" /><button v-if="canWrite" class="secondary-button" :disabled="importBusy" @click="importInput?.click()">{{ importBusy ? '处理中…' : '导入 Excel' }}</button><button class="primary-button" @click="navigate('scan')">扫码查询</button></div></div>
      <div v-if="error" class="error-banner" role="alert">{{ error }} <button aria-label="关闭提示" @click="error = ''">×</button></div><div v-if="toast" class="toast" role="status">{{ toast }}</div>

      <section v-if="activeView === 'dashboard'" class="content-area">
        <div class="metrics-grid"><MetricCard label="资产总数" :value="stats.total" tone="blue" hint="当前资产台账" /><MetricCard label="在用资产" :value="inUse" tone="green" :hint="stats.total ? `${Math.round(inUse / stats.total * 100)}% 使用中` : '暂无数据'" /><MetricCard label="空闲资产" :value="idle" tone="orange" :hint="stats.total ? `${Math.round(idle / stats.total * 100)}% 可分配` : '暂无数据'" /><MetricCard label="资产原值" :value="`¥ ${stats.amounts.taxed.toLocaleString('zh-CN')}`" tone="purple" hint="含税金额合计" /></div>
        <div class="dashboard-grid"><article class="panel wide-panel"><div class="panel-header"><div><span class="eyebrow">状态分布</span><h2>资产使用情况</h2></div><button class="text-button" @click="navigate('assets')">查看清单 →</button></div><div class="status-visual"><div class="donut" :style="{ background: donutBackground }"><div><strong>{{ stats.total }}</strong><span>项资产</span></div></div><div class="legend"><div><i class="legend-dot green"></i><span>在用</span><strong>{{ inUse }}</strong></div><div><i class="legend-dot orange"></i><span>空闲</span><strong>{{ idle }}</strong></div><div v-for="[name, count] in otherStatuses" :key="name"><i class="legend-dot gray"></i><span>{{ name }}</span><strong>{{ count }}</strong></div></div></div></article><article class="panel"><div class="panel-header"><div><span class="eyebrow">分类统计</span><h2>资产分类</h2></div><span class="panel-count">{{ stats.categories.length }} 类</span></div><div class="bar-list"><div v-for="item in stats.categories.slice(0, 6)" :key="item.name" class="bar-row"><div class="bar-label"><span>{{ item.name }}</span><strong>{{ item.count }}</strong></div><div class="bar-track"><i :style="{ width: `${Math.max(4, item.count / Math.max(stats.categories[0]?.count ?? 1, 1) * 100)}%` }"></i></div></div></div></article></div>
        <div class="dashboard-grid three"><article class="panel"><div class="panel-header"><div><span class="eyebrow">位置统计</span><h2>楼层分布</h2></div></div><div class="simple-list"><div v-for="item in stats.locations.slice(0, 6)" :key="item.name"><span>{{ item.name }}</span><strong>{{ item.count }}</strong></div></div></article><article class="panel"><div class="panel-header"><div><span class="eyebrow">公司统计</span><h2>所属公司</h2></div></div><div class="simple-list"><div v-for="item in stats.companies.slice(0, 6)" :key="item.name"><span class="truncate">{{ item.name }}</span><strong>{{ item.count }}</strong></div></div></article><article class="panel quick-panel"><span class="eyebrow">快捷操作</span><h2>开始管理资产</h2><button class="quick-action" @click="navigate('assets')">查看资产清单 <span>→</span></button><button class="quick-action" @click="navigate('scan')">扫描二维码查询 <span>⌕</span></button><button v-if="canWrite" class="quick-action" :disabled="importBusy" @click="importInput?.click()">批量导入 Excel <span>↑</span></button></article></div>
      </section>

      <section v-else-if="['assets', 'my-assets'].includes(activeView)" class="content-area"><div class="panel list-panel"><div class="filter-row"><div class="search-box"><span>⌕</span><input v-model="search" placeholder="搜索资产编码、名称、型号或序列号" @keyup.enter="applyFilters" /><button @click="applyFilters">搜索</button></div><select v-model="statusFilter" @change="applyFilters"><option value="">全部状态</option><option value="在用">在用</option><option value="空闲">空闲</option><option value="借出">借出</option><option value="维修">维修</option></select><select v-model="locationFilter" @change="applyFilters"><option value="">全部位置</option><option v-for="item in stats.locations" :key="item.name" :value="item.name">{{ item.name }}</option></select><button class="secondary-button" @click="refresh">刷新</button></div><p v-if="activeView === 'my-assets' && !currentUser.employee_id" class="directory-notice">当前账号尚未关联员工身份。通讯录同步后，使用对应员工账号登录即可查看本人资产。</p><div class="list-summary"><span>{{ activeView === 'my-assets' ? '我的资产' : employeeOnly ? '可领用资产' : '资产清单' }}</span><strong>{{ total }}</strong><span>项</span></div><AssetTable :items="assets" :total="total" :page="page" :page-size="pageSize" :loading="loading" @select="selectAsset" @page="(next) => { page = next; loadAssets(); }" /></div></section>

      <section v-else-if="activeView === 'events'" class="content-area"><FlowPanel :user="currentUser" :can-write="canRequest" :draft="flowDraft" :initial-id="flowId" @changed="refresh" @success="showToast" @settings="navigate('settings')" /></section>

      <section v-else-if="activeView === 'scan'" class="content-area scan-view"><QrLookup @lookup="lookupAsset" /></section>
      <section v-else-if="activeView === 'inventory'" class="content-area"><InventoryPanel :locations="stats.locations" :can-write="canWrite" @error="(message) => { error = message; }" @success="showToast" /></section>
      <section v-else-if="activeView === 'settings'" class="content-area"><DirectoryPanel @changed="loadUsers(); refresh()" /><UserSettingsPanel :current-id="currentUser.id" :users="users" :can-write="canManageUsers" :busy="userBusy" :created="userCreated" @create="createUser" /></section>
      <section v-else class="content-area"><div class="panel placeholder-panel"><span class="eyebrow">{{ activeTitle }}</span><h2>这个模块将在下一阶段接入</h2><p>一期核心功能已经包含资产台账、二维码解析、Excel 导入、看板统计和流转记录。</p></div></section>
    </main>
    <AssetDetailDrawer :asset="selected" :employee-only="employeeOnly" :can-apply="canRequest && (!employeeOnly || selected?.assignee_employee_id === currentUser.employee_id || selected?.status === '空闲')" :edit-busy="editBusy" :can-write="canWrite" @close="selected = null" @event="startFlow" @flow="openFlow" @update="updateAsset" />
    <dialog v-if="importPreview" ref="importDialog" class="import-modal" aria-labelledby="import-title" @cancel="cancelImport"><span class="eyebrow">导入预览</span><h2 id="import-title">{{ importPreview.fileName }}</h2><p>确认后按资产编码更新资料；已有资产的状态和归属变化请走审批，相关行将跳过。空值保留原值，明确的 0 更新金额。预览 30 分钟内有效。</p><div class="import-stats"><div><strong>{{ importPreview.preview.newRows }}</strong><span>新增</span></div><div><strong>{{ importPreview.preview.updatedRows }}</strong><span>更新</span></div><div><strong>{{ importPreview.preview.unchangedRows }}</strong><span>无变化</span></div><div class="danger"><strong>{{ importPreview.preview.invalidRows }}</strong><span>异常</span></div></div><p v-if="error" class="scan-error" role="alert">{{ error }}</p><div v-if="importPreview.errors?.length" class="import-errors"><strong>异常行（将跳过）</strong><p v-for="(item, index) in importPreview.errors" :key="index">第 {{ item.row }} 行 · {{ item.assetCode || '无资产编码' }}：{{ item.reason }}</p></div><details v-if="importPreview.changes?.length" class="import-differences"><summary>查看 {{ importPreview.changes.length }} 项资产的资料差异</summary><div v-for="change in importPreview.changes" :key="change.assetCode"><strong>{{ change.assetCode }}</strong><p v-for="field in change.fields" :key="field.field">{{ field.field }}：{{ field.before || '空' }} → {{ field.after }}</p></div></details><div class="form-actions"><button class="secondary-button" :disabled="importBusy" autofocus @click="importPreview = null">取消</button><button class="primary-button" :disabled="importBusy || !(importPreview.preview.newRows + importPreview.preview.updatedRows + importPreview.preview.unchangedRows)" @click="confirmImport">{{ importBusy ? '导入中…' : '确认导入' }}</button></div></dialog>
  </div>
</template>

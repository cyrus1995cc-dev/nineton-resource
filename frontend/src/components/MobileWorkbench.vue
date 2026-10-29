<script setup lang="ts">
import { computed } from 'vue';
import type { AuthUser, Stats } from '../types';
import { roleLabels } from '../directory';
import AppIcon from './AppIcon.vue';
const props = defineProps<{ user: AuthUser; stats: Stats; profile?: boolean }>();
const emit = defineEmits<{ navigate: [view: string]; create: []; logout: [] }>();
const admin = computed(() => props.user.role === 'admin');
const limited = computed(() => ['employee','approver'].includes(props.user.role));
const writer = computed(() => ['admin','asset_manager'].includes(props.user.role));
const sections = computed(() => [
  { title: '资产服务', items: [ { name: '我的资产', icon: 'box', view: 'my-assets', tone: 'teal' }, ...(!limited.value ? [{ name: '资产清单', icon: 'list', view: 'assets', tone: 'blue' }] : []), { name: '扫码查询', icon: 'scan', view: 'scan', tone: 'violet' }, ...(writer.value ? [{ name: '新增资产', icon: 'plus', view: 'create', tone: 'orange' }] : []), { name: '申请与审批', icon: 'flow', view: 'events', tone: 'blue' }, ...(!['employee','approver'].includes(props.user.role) ? [{ name: '资产盘点', icon: 'check', view: 'inventory', tone: 'teal' }] : []) ] },
  ...(admin.value ? [{ title: '基础管理', items: [ { name: '组织与人员', icon: 'users', view: 'directory', tone: 'blue' }, { name: '公司管理', icon: 'building', view: 'companies', tone: 'violet' }, { name: '资产分类', icon: 'grid', view: 'categories', tone: 'orange' }, { name: '编码规则', icon: 'code', view: 'codes', tone: 'teal' }, { name: '资产卡片', icon: 'card', view: 'cards', tone: 'blue' }, { name: '账号权限', icon: 'settings', view: 'settings', tone: 'violet' } ] }] : [])
]);
function act(view: string) { if (view === 'create') emit('create'); else emit('navigate', view); }
</script>
<template>
 <div :class="['mobile-workbench', { 'profile-workbench': profile }]">
  <header class="mobile-welcome"><div><span class="mobile-brand-name">九吨资产</span><h1>{{ profile ? user.display_name : `Hi，${user.display_name}` }}</h1><span class="mobile-role"><i></i>{{ roleLabels[user.role] }}</span></div><img src="/brand/jiudun-mascot.png" alt="九吨资产" width="70" height="70" /></header>
  <template v-if="!profile"><div class="mobile-stat-strip"><button v-for="item in [{label:limited ? '我的资产' : '资产总数',value:stats.total},{label:'在用资产',value:stats.status['在用'] || 0},{label:limited ? '其他状态' : '空闲资产',value:limited ? stats.total - (stats.status['在用'] || 0) : stats.status['空闲'] || 0}]" :key="item.label" @click="emit('navigate','assets')"><strong>{{ item.value.toLocaleString('zh-CN') }}</strong><span>{{ item.label }}</span></button></div><button class="mobile-scan-banner" @click="emit('navigate','scan')"><span class="mobile-scan-mark"><AppIcon name="scan" :size="30" /></span><span><strong>扫一扫，找到这件资产</strong><small>支持现有资产标签和资产编码</small></span><AppIcon name="chevron" :size="18" /></button></template>
  <div v-if="profile" class="mobile-profile-info"><div><span>当前账号</span><strong>{{ user.username }}</strong></div><div><span>所在部门</span><strong>{{ user.department || '尚未关联通讯录部门' }}</strong></div><div><span>权限</span><strong>{{ roleLabels[user.role] }}</strong></div></div>
  <section v-for="section in (profile ? sections.filter(s => s.title === '基础管理') : sections)" :key="section.title" class="mobile-function-card"><h2>{{ section.title }}</h2><div class="mobile-function-grid"><button v-for="item in section.items" :key="item.view" @click="act(item.view)"><span :class="['mobile-function-icon',item.tone]"><AppIcon :name="item.icon" :size="25" /></span><span>{{ item.name }}</span></button></div></section>
  <button v-if="profile" class="mobile-logout" @click="emit('logout')">退出当前账号</button>
  <p class="mobile-workbench-note">让每一件资产，清晰有序</p>
 </div>
</template>

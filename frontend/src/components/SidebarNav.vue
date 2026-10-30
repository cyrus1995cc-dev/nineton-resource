<script setup lang="ts">
import AppIcon from './AppIcon.vue';
import BrandLogo from './BrandLogo.vue';
defineProps<{ active: string; employeeOnly?: boolean; canManage?: boolean; canRequest?:boolean }>();
const emit = defineEmits<{ navigate: [value: string] }>();
const items = [
  { key: 'dashboard', icon: 'home', label: '首页', group: '' },
  { key: 'my-assets', icon: 'box', label: '我的资产', group: '' },
  { key: 'assets', icon: 'list', label: '资产清单', group: '资产管理' },
  { key: 'scan', icon: 'scan', label: '扫码查询', group: '资产管理' },
  { key: 'events', icon: 'flow', label: '申请与审批', group: '资产管理' },
  { key: 'requisition', icon: 'plus', label: '申请领用', group: '资产管理' },
  { key: 'inventory', icon: 'check', label: '资产盘点', group: '资产管理' },
  { key: 'companies', icon: 'building', label: '公司管理', group: '基础设置' },
  { key: 'directory', icon: 'users', label: '组织与人员', group: '基础设置' },
  { key: 'categories', icon: 'grid', label: '分类与卡片', group: '基础设置' },
  { key: 'labels', icon: 'printer', label: '标签样式', group: '基础设置' },
  { key: 'settings', icon: 'settings', label: '系统设置', group: '基础设置' },
];
</script>

<template>
  <aside class="sidebar">
    <div class="brand"><BrandLogo /></div>
    <nav>
      <template v-for="group in ['', '资产管理', '基础设置']" :key="group"><span v-if="group && (group !== '基础设置' || canManage)" class="nav-group-label">{{ group }}</span><button v-for="item in items.filter((i) => i.group === group && (i.key !== 'requisition' || canRequest) && !(employeeOnly && ['assets', 'inventory', 'settings', 'companies', 'directory', 'categories'].includes(i.key)) && (i.group !== '基础设置' || canManage))" :key="item.key" class="nav-item" :class="{ active: active === item.key || item.key === 'categories' && ['cards','codes'].includes(active) }" :aria-current="active === item.key ? 'page' : undefined" @click="emit('navigate', item.key)"><AppIcon :name="item.icon" /><span>{{ item.label }}</span></button></template>
    </nav>
    <div class="sidebar-footer"><span>管理范围</span><strong>{{ employeeOnly ? '本人领用的资产' : '全部资产' }}</strong></div>
  </aside>
</template>

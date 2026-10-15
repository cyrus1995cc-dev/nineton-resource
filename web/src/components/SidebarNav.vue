<script setup lang="ts">
defineProps<{ active: string; employeeOnly?: boolean; canManage?: boolean }>();
const emit = defineEmits<{ navigate: [value: string] }>();
const items = [
  { key: 'dashboard', icon: '▣', label: '首页' },
  { key: 'my-assets', icon: '▦', label: '我的资产' },
  { key: 'assets', icon: '▦', label: '资产清单' },
  { key: 'scan', icon: '⌕', label: '扫码查询' },
  { key: 'events', icon: '↔', label: '资产流转' },
  { key: 'inventory', icon: '✓', label: '资产盘点' },
  { key: 'settings', icon: '⚙', label: '系统设置' },
];
</script>

<template>
  <aside class="sidebar">
    <div class="brand"><span class="brand-mark">◒</span><span>自有资产</span></div>
    <nav>
      <button v-for="item in items.filter((i) => !(employeeOnly && ['inventory', 'settings'].includes(i.key)) && (i.key !== 'settings' || canManage))" :key="item.key" class="nav-item" :class="{ active: active === item.key }" @click="emit('navigate', item.key)">
        <span class="nav-icon">{{ item.icon }}</span><span>{{ item.key === 'assets' && employeeOnly ? '可领用资产' : item.label }}</span>
      </button>
    </nav>
    <div class="sidebar-footer"><span>管理范围</span><strong>{{ employeeOnly ? '本人及审批相关资产' : '全部资产' }}</strong></div>
  </aside>
</template>

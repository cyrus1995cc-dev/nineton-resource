import { createApp } from 'vue';
import App from './App.vue';
import './styles.css';
import './organization.css';
import './assetWorkspace.css';
import './brand.css';
import './assetScan.css';
import './catalogue.css';
import './mobile.css';
import './employeeMappings.css';
import './assetLabels.css';
import './requisitions.css';
import { labelContentCSS } from './assetLabels';
const labelStyle = document.createElement('style'); labelStyle.textContent = labelContentCSS; document.head.appendChild(labelStyle);

createApp(App).mount('#app');

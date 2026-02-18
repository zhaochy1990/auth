import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

import zhCommon from './locales/zh-CN/common.json';
import zhLogin from './locales/zh-CN/login.json';
import zhDashboard from './locales/zh-CN/dashboard.json';
import zhApplications from './locales/zh-CN/applications.json';
import zhUsers from './locales/zh-CN/users.json';

import enCommon from './locales/en-US/common.json';
import enLogin from './locales/en-US/login.json';
import enDashboard from './locales/en-US/dashboard.json';
import enApplications from './locales/en-US/applications.json';
import enUsers from './locales/en-US/users.json';

const savedLang = localStorage.getItem('lang') || 'zh-CN';

i18n.use(initReactI18next).init({
  resources: {
    'zh-CN': {
      common: zhCommon,
      login: zhLogin,
      dashboard: zhDashboard,
      applications: zhApplications,
      users: zhUsers,
    },
    'en-US': {
      common: enCommon,
      login: enLogin,
      dashboard: enDashboard,
      applications: enApplications,
      users: enUsers,
    },
  },
  lng: savedLang,
  fallbackLng: 'en-US',
  defaultNS: 'common',
  interpolation: { escapeValue: false },
});

export default i18n;

<script setup lang="ts">
import { computed, onMounted, onUnmounted } from "vue";
import SidebarNav from "./components/SidebarNav.vue";
import TopBar from "./components/TopBar.vue";
import ToastHost from "./components/ToastHost.vue";
import OnboardingWizard from "./components/OnboardingWizard.vue";
import RestoreModal from "./components/RestoreModal.vue";
import ConfirmModal from "./components/ConfirmModal.vue";
import HelpModal from "./components/HelpModal.vue";
import DashboardView from "./views/DashboardView.vue";
import HistoryView from "./views/HistoryView.vue";
import SettingsView from "./views/SettingsView.vue";
import { dispose, dismissUpdate, go, init, openReleasePage, retryLoad, store } from "./store";

const showOnboarding = computed(() => !!store.boot && !store.boot.config.onboardingComplete);
const showSplash = computed(() => !store.ready && !store.boot);
const bootFailed = computed(() => store.ready && !store.boot);

onMounted(init);
onUnmounted(dispose);
</script>

<template>
  <svg class="svg-defs" aria-hidden="true">
    <symbol id="i-whale" viewBox="0 0 27 24">
      <path fill="currentColor" stroke="none" d="M26.5542 4.34393C26.2719 4.20592 26.1506 4.46928 25.9856 4.60268C25.9292 4.64581 25.8815 4.70216 25.8338 4.75391C25.4215 5.19438 24.9396 5.48361 24.3105 5.44911C23.3905 5.39736 22.605 5.68659 21.9104 6.39041C21.7626 5.52271 21.2721 5.00462 20.5258 4.67226C20.1353 4.49976 19.7403 4.32668 19.4666 3.95119C19.2757 3.68381 19.2234 3.38595 19.1279 3.09211C19.0669 2.91501 19.0066 2.73388 18.8024 2.7034C18.5811 2.6689 18.4942 2.85463 18.4074 3.00989C18.0601 3.6447 17.9255 4.34393 17.9388 5.05235C17.9692 6.64572 18.642 7.91478 19.9789 8.81756C20.1307 8.92106 20.1698 9.02457 20.1221 9.1758C20.0307 9.48688 19.9226 9.78876 19.8271 10.0998C19.7662 10.2982 19.6753 10.3419 19.4626 10.2551C18.7288 9.94862 18.0952 9.49493 17.5351 8.94694C16.5846 8.02749 15.7249 7.01258 14.6531 6.21791C14.4013 6.03218 14.1494 5.85967 13.8889 5.69522C12.7952 4.63316 14.0321 3.76086 14.3185 3.65736C14.618 3.54925 14.4225 3.17779 13.4548 3.18239C12.487 3.18642 11.6015 3.51073 10.4727 3.94256C10.3077 4.00754 10.1341 4.05469 9.95637 4.09379C8.93227 3.89944 7.86849 3.85631 6.75755 3.98167C4.66564 4.21455 2.99464 5.20358 1.7664 6.89183C0.290908 8.92106 -0.0564026 11.2269 0.368535 13.6316C0.815324 16.1663 2.10911 18.2645 4.09695 19.905C6.15838 21.6059 8.53263 22.4397 11.2415 22.2799C12.8867 22.185 14.7181 21.9648 16.7841 20.2161C17.3051 20.4755 17.8519 20.579 18.7587 20.6566C19.4574 20.7216 20.1302 20.6221 20.6511 20.514C21.4671 20.3415 21.4107 19.5859 21.1157 19.4473C18.7242 18.3335 19.2492 18.7866 18.772 18.4198C19.987 16.9822 21.8431 14.4269 22.4158 10.9474C22.4722 10.5633 22.5441 10.0222 22.5355 9.71114C22.5309 9.52138 22.5746 9.44778 22.7913 9.42593C23.3905 9.35693 23.9718 9.19305 24.506 8.89921C26.0557 8.05279 26.6808 6.6624 26.828 4.996C26.8498 4.74126 26.8234 4.47791 26.5542 4.34393ZM13.0511 19.3438C10.7332 17.5216 9.60906 16.9219 9.14502 16.9477C8.71089 16.9736 8.78909 17.4704 8.88454 17.7942C8.98459 18.1139 9.11455 18.3341 9.29683 18.6147C9.42276 18.8004 9.50959 19.0764 9.1709 19.284C8.42453 19.7458 7.12671 19.1288 7.06576 19.0983C5.55519 18.2087 4.29245 17.0346 3.40233 15.4285C2.54268 13.8829 2.04356 12.2245 1.96133 10.4546C1.93948 10.0274 2.06541 9.87617 2.49092 9.79854C3.05099 9.69504 3.62831 9.67319 4.1878 9.75541C6.55342 10.101 8.56713 11.1585 10.2554 12.8341C11.2191 13.788 11.9482 14.9283 12.6992 16.0421C13.4979 17.2249 14.357 18.3519 15.4512 19.276C15.8377 19.5997 16.1459 19.8458 16.4408 20.0275C15.5513 20.127 14.0666 20.1483 13.0511 19.345ZM14.162 12.1981C14.162 12.0083 14.3139 11.8571 14.5048 11.8571C14.5479 11.8571 14.587 11.8657 14.6221 11.8784C14.6698 11.8956 14.7135 11.9215 14.748 11.9606C14.8089 12.021 14.8434 12.1072 14.8434 12.1981C14.8434 12.3878 14.6916 12.5391 14.5007 12.5391C14.3098 12.5391 14.162 12.3878 14.162 12.1981ZM17.6127 13.968C17.3913 14.0588 17.17 14.1365 16.9572 14.1451C16.6271 14.1623 16.2672 14.0284 16.0717 13.8645C15.7681 13.6098 15.5507 13.4671 15.4599 13.0227C15.4208 12.8329 15.4426 12.5391 15.4771 12.3706C15.5553 12.0078 15.4685 11.7749 15.2126 11.5633C15.0045 11.3908 14.7394 11.343 14.4484 11.343C14.3397 11.343 14.2403 11.2953 14.1661 11.2568C14.0447 11.1964 13.9447 11.0452 14.0401 10.8594C14.0706 10.7991 14.2184 10.6524 14.2529 10.6266C14.6479 10.4017 15.1034 10.4753 15.5248 10.6438C15.9153 10.8037 16.2108 11.0969 16.6358 11.5115C17.0699 12.0124 17.1481 12.1504 17.3954 12.5264C17.5909 12.8203 17.7686 13.1221 17.8905 13.4677C17.9641 13.6834 17.8686 13.8599 17.6127 13.968Z"></path>
    </symbol>
    <symbol id="i-grid" viewBox="0 0 24 24">
      <rect x="3" y="3" width="7" height="7" rx="1.5"></rect>
      <rect x="14" y="3" width="7" height="7" rx="1.5"></rect>
      <rect x="3" y="14" width="7" height="7" rx="1.5"></rect>
      <rect x="14" y="14" width="7" height="7" rx="1.5"></rect>
    </symbol>
    <symbol id="i-history" viewBox="0 0 24 24">
      <path d="M3.5 12a8.5 8.5 0 1 0 2.5-6"></path>
      <path d="M3.5 4.5V12H11"></path>
      <path d="M12 7.5V12l3 1.8"></path>
    </symbol>
    <symbol id="i-archive" viewBox="0 0 24 24">
      <rect x="3" y="4" width="18" height="4" rx="1"></rect>
      <path d="M5 8v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8"></path>
      <path d="M10 12h4"></path>
    </symbol>
    <symbol id="i-clock" viewBox="0 0 24 24">
      <circle cx="12" cy="12" r="9"></circle>
      <path d="M12 7v5l3 2"></path>
    </symbol>
    <symbol id="i-database" viewBox="0 0 24 24">
      <ellipse cx="12" cy="6" rx="8" ry="3"></ellipse>
      <path d="M4 6v6c0 1.7 3.6 3 8 3s8-1.3 8-3V6"></path>
      <path d="M4 12v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"></path>
    </symbol>
    <symbol id="i-check-circle" viewBox="0 0 24 24">
      <circle cx="12" cy="12" r="9"></circle>
      <path d="m8.5 12 2.5 2.5 4.5-5"></path>
    </symbol>
    <symbol id="i-lock" viewBox="0 0 24 24">
      <rect x="5" y="10" width="14" height="10" rx="2"></rect>
      <path d="M8 10V7a4 4 0 0 1 8 0v3"></path>
    </symbol>
    <symbol id="i-chevron-right" viewBox="0 0 24 24">
      <path d="m9 5 7 7-7 7"></path>
    </symbol>
    <symbol id="i-plus" viewBox="0 0 24 24">
      <path d="M12 5v14M5 12h14"></path>
    </symbol>
    <symbol id="i-file" viewBox="0 0 24 24">
      <path d="M6 3h8l4 4v14H6V3z"></path>
      <path d="M14 3v4h4"></path>
      <path d="M9 12h6M9 16h6"></path>
    </symbol>
    <symbol id="i-export" viewBox="0 0 24 24">
      <path d="M12 3v12"></path>
      <path d="m8 11 4 4 4-4"></path>
      <path d="M5 20h14"></path>
    </symbol>
    <symbol id="i-monitor" viewBox="0 0 24 24">
      <rect x="3" y="4" width="18" height="12" rx="2"></rect>
      <path d="M8 20h8M12 16v4"></path>
    </symbol>
    <symbol id="i-minus" viewBox="0 0 24 24">
      <circle cx="12" cy="12" r="9"></circle>
      <path d="M8 12h8"></path>
    </symbol>
    <symbol id="i-github" viewBox="0 0 24 24">
      <path fill="currentColor" stroke="none" d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"></path>
    </symbol>
    <symbol id="i-settings" viewBox="0 0 24 24">
      <path d="M4 6h16M4 12h16M4 18h16"></path>
      <circle cx="8" cy="6" r="2" fill="currentColor" stroke="none"></circle>
      <circle cx="16" cy="12" r="2" fill="currentColor" stroke="none"></circle>
      <circle cx="10" cy="18" r="2" fill="currentColor" stroke="none"></circle>
    </symbol>
    <symbol id="i-help" viewBox="0 0 24 24">
      <circle cx="12" cy="12" r="9"></circle>
      <path d="M9.6 9a2.5 2.5 0 1 1 4.2 1.9c-.9.7-1.8 1.2-1.8 2.6"></path>
      <path d="M12 17.4v.1"></path>
    </symbol>
    <symbol id="i-shield" viewBox="0 0 24 24">
      <path d="M12 3.5 19 6v5.4c0 4.4-2.8 7.7-7 9.1-4.2-1.4-7-4.7-7-9.1V6l7-2.5Z"></path>
      <path d="m9 12 2 2 4-4"></path>
    </symbol>
    <symbol id="i-folder" viewBox="0 0 24 24">
      <path d="M3.5 7.5a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2v-10Z"></path>
    </symbol>
    <symbol id="i-refresh" viewBox="0 0 24 24">
      <path d="M20 11a8 8 0 0 0-14.7-4.6L4 8"></path>
      <path d="M4 4v4h4"></path>
      <path d="M4 13a8 8 0 0 0 14.7 4.6L20 16"></path>
      <path d="M20 20v-4h-4"></path>
    </symbol>
    <symbol id="i-key" viewBox="0 0 24 24">
      <circle cx="8" cy="15" r="4"></circle>
      <path d="m11 12 8-8"></path>
      <path d="m16 7 3 3"></path>
      <path d="m14 9 2 2"></path>
    </symbol>
    <symbol id="i-trash" viewBox="0 0 24 24">
      <path d="M4 7h16"></path>
      <path d="M9 7V5h6v2"></path>
      <path d="m6 7 1 13h10l1-13"></path>
      <path d="M10 11v5M14 11v5"></path>
    </symbol>
    <symbol id="i-external" viewBox="0 0 24 24">
      <path d="M14 4h6v6"></path>
      <path d="m20 4-9 9"></path>
      <path d="M19 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h5"></path>
    </symbol>
    <symbol id="i-check" viewBox="0 0 24 24">
      <path d="m5 12.5 4.5 4.5L19 7.5"></path>
    </symbol>
    <symbol id="i-alert" viewBox="0 0 24 24">
      <path d="M12 3 2.8 20h18.4L12 3Z"></path>
      <path d="M12 9v5"></path>
      <path d="M12 17.5v.1"></path>
    </symbol>
    <symbol id="i-play" viewBox="0 0 24 24">
      <path d="M8 5.5v13l10-6.5-10-6.5Z"></path>
    </symbol>
    <symbol id="i-restore" viewBox="0 0 24 24">
      <path d="M3 12a9 9 0 1 0 3-6.7"></path>
      <path d="M3 4v5h5"></path>
      <path d="M12 8v4l2.5 1.5"></path>
    </symbol>
    <symbol id="i-x" viewBox="0 0 24 24">
      <path d="m6 6 12 12M18 6 6 18"></path>
    </symbol>
  </svg>

  <div v-if="showSplash || bootFailed" class="splash">
    <div class="splash-mark"><svg class="whale-mark" aria-hidden="true"><use href="#i-whale"></use></svg></div>
    <template v-if="bootFailed">
      <h1>无法加载应用状态</h1>
      <p>{{ store.bootError || "后端没有返回状态。" }}</p>
      <button class="btn btn-primary" @click="retryLoad()">重试</button>
    </template>
    <template v-else>
      <h1>DSH Backup</h1>
      <p>正在准备本地备份环境…</p>
      <span class="spinner splash-spinner"></span>
    </template>
  </div>

  <div v-else class="desktop">
    <div class="app-shell">
      <SidebarNav />
      <main class="main">
        <TopBar @help="store.modals.help = true" />
        <div v-if="store.update?.available && !store.updateDismissed" class="shell-notice update-notice" role="status">
          <span class="update-copy"><svg class="icon"><use href="#i-export"></use></svg>发现新版本 v{{ store.update.latestVersion }}<span class="update-current">当前 v{{ store.update.currentVersion }}</span></span>
          <span class="update-actions"><button class="text-button" @click="openReleasePage()">查看发行版</button><button class="icon-btn" type="button" aria-label="关闭更新提示" @click="dismissUpdate"><svg class="icon"><use href="#i-x"></use></svg></button></span>
        </div>
        <div v-if="store.settingsReminder && store.settingsDirty && store.page !== 'settings'" class="shell-notice" role="status"><span>{{ store.settingsReminder }}</span><button class="text-button" @click="go('settings')">返回设置</button></div>
        <div v-if="store.bootError" class="shell-notice error" role="alert"><span>状态刷新失败，当前显示上次读取的数据：{{ store.bootError }}</span><button class="text-button" :disabled="store.refreshing" @click="retryLoad()">重试刷新</button></div>
        <div class="content" id="content">
          <DashboardView />
          <HistoryView />
          <SettingsView />
        </div>
      </main>
    </div>
  </div>

  <OnboardingWizard v-if="showOnboarding" />
  <RestoreModal v-if="store.modals.restore" />
  <ConfirmModal v-if="store.modals.confirm" />
  <HelpModal v-if="store.modals.help" />
  <ToastHost />
</template>

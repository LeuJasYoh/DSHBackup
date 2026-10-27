<script setup lang="ts">
import { computed, onMounted, onUnmounted } from "vue";
import SidebarNav from "./components/SidebarNav.vue";
import TopBar from "./components/TopBar.vue";
import ToastHost from "./components/ToastHost.vue";
import OnboardingWizard from "./components/OnboardingWizard.vue";
import RestoreModal from "./components/RestoreModal.vue";
import GitHubModal from "./components/GitHubModal.vue";
import ConfirmModal from "./components/ConfirmModal.vue";
import HelpModal from "./components/HelpModal.vue";
import DashboardView from "./views/DashboardView.vue";
import HistoryView from "./views/HistoryView.vue";
import SettingsView from "./views/SettingsView.vue";
import { closeConfirm, closeGitHubModal, closeModals, dispose, init, retryLoad, store } from "./store";

const showOnboarding = computed(() => !!store.boot && !store.boot.config.onboardingComplete);
const showSplash = computed(() => !store.ready && !store.boot);
const bootFailed = computed(() => store.ready && !store.boot);

function onKeydown(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  if (showOnboarding.value) return;
  if (store.modals.confirm) {
    closeConfirm();
    return;
  }
  if (store.modals.github) {
    closeGitHubModal();
    return;
  }
  if (store.modals.restore || store.modals.help) {
    closeModals();
  }
}

onMounted(async () => {
  window.addEventListener("keydown", onKeydown);
  await init();
});

onUnmounted(() => {
  window.removeEventListener("keydown", onKeydown);
  dispose();
});
</script>

<template>
  <svg class="svg-defs" aria-hidden="true">
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
    <symbol id="i-cloud" viewBox="0 0 24 24">
      <path d="M6.7 18.2h11a3.8 3.8 0 0 0 .4-7.5A6 6 0 0 0 6.4 9.3a4.5 4.5 0 0 0 .3 8.9Z"></path>
      <path d="M9 13.5h6"></path>
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
    <symbol id="i-x" viewBox="0 0 24 24">
      <path d="m6 6 12 12M18 6 6 18"></path>
    </symbol>
  </svg>

  <div v-if="showSplash || bootFailed" class="splash">
    <div class="splash-mark"><img src="/icon.svg" alt="" /></div>
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
  <GitHubModal v-if="store.modals.github" />
  <ConfirmModal v-if="store.modals.confirm" />
  <HelpModal v-if="store.modals.help" />
  <ToastHost />
</template>

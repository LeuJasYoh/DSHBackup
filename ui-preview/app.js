(() => {
  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));

  const compressionLabels = {
    fast: "快速压缩",
    balanced: "均衡压缩",
    max: "极限压缩"
  };

  const compressionHints = {
    fast: "当前：快速 · 备份更快，归档体积更大。",
    balanced: "当前：均衡 · 速度与体积平衡，推荐。",
    max: "当前：极限 · 体积最小，压缩时间和 CPU 占用更高。"
  };

  const state = {
    page: "dashboard",
    settingsTab: "backup",
    mode: "local",
    cloudConnected: false,
    compression: "balanced",
    localEncryption: false,
    localDir: "D:\\DSHBackup\\backups",
    onboardingStep: 1,
    wizardMode: "local",
    wizardDir: "D:\\DSHBackup\\backups",
    historyFilter: "all",
    history: [
      {
        id: "snapshot-20261026-1842",
        date: "2026-10-26 18:42",
        source: "local",
        size: "64.3 MB",
        encrypted: false,
        status: "success"
      },
      {
        id: "snapshot-20261025-1615",
        date: "2026-10-25 16:15",
        source: "local",
        size: "63.8 MB",
        encrypted: false,
        status: "success"
      },
      {
        id: "snapshot-20261024-2107",
        date: "2026-10-24 21:07",
        source: "local",
        size: "62.9 MB",
        encrypted: false,
        status: "success"
      }
    ],
    backupTimer: null,
    backupProgress: 0,
    pendingMode: null,
    pendingAfterOnboarding: false,
    githubContext: null,
    toastTimer: null
  };

  const pageMeta = {
    dashboard: {
      title: "概览",
      subtitle: "DSH 核心数据备份"
    },
    history: {
      title: "备份历史",
      subtitle: "本地与云端快照"
    },
    settings: {
      title: "设置",
      subtitle: "备份方式与云端连接"
    }
  };

  function modeLabel(mode) {
    if (mode === "hybrid") return "本地 + GitHub";
    if (mode === "cloud") return "仅 GitHub";
    return "仅本地";
  }

  function modeContext(mode) {
    if (mode === "hybrid") return "本地 + 云端 · 云端归档始终加密";
    if (mode === "cloud") return "仅云端 · 建议同时保留本地副本";
    return "本地模式 · 不发起网络请求";
  }

  function sourceHtml(source) {
    if (source === "both") {
      return '<span class="table-source"><span class="badge local">本地</span><span class="badge cloud">云端</span></span>';
    }
    if (source === "cloud") {
      return '<span class="badge cloud">云端</span>';
    }
    return '<span class="badge local">本地</span>';
  }

  function encryptionHtml(encrypted) {
    return encrypted
      ? '<span class="badge success">已加密</span>'
      : '<span class="badge muted">未加密</span>';
  }

  function historyRow(item, withVerify = false) {
    const verify = withVerify
      ? `<button class="table-action" data-action="verify" data-id="${item.id}">验证</button>`
      : "";
    return `
      <tr>
        <td><strong>${item.date}</strong></td>
        <td>${sourceHtml(item.source)}</td>
        <td>${item.size}</td>
        <td>${encryptionHtml(item.encrypted)}</td>
        <td><span class="badge success">成功</span></td>
        <td class="align-right">
          <span class="table-actions">
            <button class="table-action" data-action="restore" data-id="${item.id}">恢复</button>
            ${verify}
          </span>
        </td>
      </tr>
    `;
  }

  function renderRecent() {
    $("#recentBody").innerHTML = state.history.slice(0, 3).map((item) => historyRow(item)).join("");
  }

  function filteredHistory() {
    if (state.historyFilter === "local") {
      return state.history.filter((item) => item.source === "local" || item.source === "both");
    }
    if (state.historyFilter === "cloud") {
      return state.history.filter((item) => item.source === "cloud" || item.source === "both");
    }
    return state.history;
  }

  function renderHistory() {
    const rows = filteredHistory();
    $("#historyBody").innerHTML = rows.map((item) => historyRow(item, true)).join("");
    $("#historyCount").textContent = `共 ${rows.length} 份备份`;
  }

  function renderRestoreOptions() {
    const select = $("#restoreSnapshot");
    select.innerHTML = state.history
      .map((item) => {
        const source = item.source === "both" ? "本地 + 云端" : item.source === "cloud" ? "云端" : "本地";
        return `<option value="${item.id}">${item.date} · ${source} · ${item.size}</option>`;
      })
      .join("");
  }

  function updateModeUI() {
    const label = modeLabel(state.mode);
    $("#topModeText").textContent = label;
    $("#sidebarMode").textContent = label;
    $("#topMode").classList.toggle("cloud", state.mode !== "local");
    $("#dashboardContext").textContent = modeContext(state.mode);

    $$('input[name="settingsMode"]').forEach((input) => {
      const selected = input.value === state.mode;
      input.checked = selected;
      const option = input.closest(".mode-option");
      if (option) option.classList.toggle("selected", selected);
    });

    updateCloudUI();
  }

  function updateCloudUI() {
    const disconnected = $("#cloudDisconnected");
    const connected = $("#cloudConnected");
    if (disconnected) disconnected.hidden = state.cloudConnected;
    if (connected) connected.hidden = !state.cloudConnected;

    const tokenBadges = $$('[data-panel="security"] .static-value .badge');
    if (tokenBadges[1]) {
      tokenBadges[1].textContent = state.cloudConnected ? "已连接" : "未连接";
      tokenBadges[1].className = state.cloudConnected ? "badge success" : "badge muted";
    }
  }

  function showPage(page) {
    if (!pageMeta[page]) return;
    state.page = page;
    $$(".nav-item").forEach((item) => item.classList.toggle("active", item.dataset.page === page));
    $$(".page").forEach((item) => item.classList.toggle("active", item.id === `page-${page}`));
    $("#pageTitle").textContent = pageMeta[page].title;
    $("#pageSubtitle").textContent = pageMeta[page].subtitle;
    $("#content").scrollTop = 0;
    if (page === "history") renderHistory();
    if (page === "settings") activateSettingsTab(state.settingsTab);
  }

  function activateSettingsTab(tab) {
    state.settingsTab = tab;
    $$(".settings-tab").forEach((item) => item.classList.toggle("active", item.dataset.tab === tab));
    $$(".settings-panel").forEach((item) => item.classList.toggle("active", item.dataset.panel === tab));
    if (tab === "cloud") updateCloudUI();
  }

  function openSettingsTab(tab) {
    showPage("settings");
    activateSettingsTab(tab);
  }

  function showToast(message) {
    const toast = $("#toast");
    clearTimeout(state.toastTimer);
    toast.textContent = message;
    toast.classList.add("show");
    state.toastTimer = setTimeout(() => toast.classList.remove("show"), 2600);
  }

  function closeOverlay(id) {
    const overlay = document.getElementById(id);
    if (overlay) overlay.classList.remove("open");
  }

  function closeAllOverlays() {
    $$(".overlay").forEach((overlay) => overlay.classList.remove("open"));
  }

  /* Navigation */
  $$(".nav-item").forEach((item) => {
    item.addEventListener("click", () => showPage(item.dataset.page));
  });

  $("#helpButton").addEventListener("click", () => showToast("帮助文档将在正式版中提供。"));

  /* Dashboard actions */
  $("#backupButton").addEventListener("click", startBackup);
  $("#emptyBackupButton").addEventListener("click", startBackup);
  $("#restoreButton").addEventListener("click", () => openRestoreModal(state.history[0]?.id));
  $("#viewAllButton").addEventListener("click", () => showPage("history"));
  $("#manageRetentionButton").addEventListener("click", () => openSettingsTab("backup"));
  $("#openFolderButton").addEventListener("click", () => showToast(`打开本地备份目录：${state.localDir}`));
  $("#verifyButton").addEventListener("click", () => showToast("正在验证最近一次备份的完整性（预览）。"));
  $("#recheckButton").addEventListener("click", () => {
    $("#runningAlert").hidden = true;
    showToast("未检测到正在运行的 DSH。");
  });

  /* History */
  $("#historyFilter").addEventListener("click", (event) => {
    const button = event.target.closest("button[data-filter]");
    if (!button) return;
    state.historyFilter = button.dataset.filter;
    $$("#historyFilter button").forEach((item) => item.classList.toggle("active", item === button));
    renderHistory();
  });

  $("#refreshHistoryButton").addEventListener("click", () => {
    renderHistory();
    showToast("备份历史已刷新（预览）。");
  });

  document.addEventListener("click", (event) => {
    const actionButton = event.target.closest("[data-action]");
    if (!actionButton) return;
    const { action, id } = actionButton.dataset;
    if (action === "restore") openRestoreModal(id);
    if (action === "verify") showToast("完整性校验通过（预览）。");
  });

  /* Settings mode */
  $$('input[name="settingsMode"]').forEach((input) => {
    input.addEventListener("change", () => {
      const nextMode = input.value;
      if (nextMode !== "local" && !state.cloudConnected) {
        state.pendingMode = nextMode;
        state.githubContext = "settings";
        openGithubModal();
        return;
      }
      state.mode = nextMode;
      updateModeUI();
      if (nextMode === "cloud") {
        showToast("仅 GitHub 模式没有本地副本，请谨慎使用。");
      }
    });
  });

  /* Settings tabs */
  $$(".settings-tab").forEach((tab) => {
    tab.addEventListener("click", () => activateSettingsTab(tab.dataset.tab));
  });

  /* Compression */
  function setCompression(value, root = document) {
    state.compression = value;
    $$("[data-compression]", root).forEach((button) => {
      button.classList.toggle("active", button.dataset.compression === value);
    });
    const hint = $("#compressionHint");
    if (hint) hint.textContent = compressionHints[value];
  }

  $("#compressionSegment").addEventListener("click", (event) => {
    const button = event.target.closest("[data-compression]");
    if (!button) return;
    setCompression(button.dataset.compression);
  });

  /* Encryption sync */
  function setLocalEncryption(value) {
    state.localEncryption = value;
    const securityToggle = $("#securityEncryptionToggle");
    if (securityToggle) securityToggle.checked = value;
    const status = $("#localEncryptionStatus");
    if (status) {
      status.textContent = value ? "开启" : "关闭";
      status.className = value ? "badge success" : "badge muted";
    }
    const keyStatus = $("#localKeyStatus");
    if (keyStatus) {
      keyStatus.textContent = value ? "已生成" : "未生成";
      keyStatus.className = value ? "badge success" : "badge muted";
    }
  }

  $("#securityEncryptionToggle").addEventListener("change", (event) => {
    setLocalEncryption(event.target.checked);
  });

  $("#openKeyFolder").addEventListener("click", () => {
    showToast("打开本地密钥目录：应用目录\\data\\keys（预览）。");
  });

  $("#openSecuritySettings").addEventListener("click", () => {
    activateSettingsTab("security");
  });

  /* Save buttons */
  $$(".settings-save").forEach((button) => {
    button.addEventListener("click", () => {
      if (button.dataset.save === "backup") {
        state.localDir = $("#localDirInput").value || state.localDir;
      }
      showToast("设置已保存（预览）。");
    });
  });

  /* Proxy test */
  $("#testProxyButton").addEventListener("click", () => {
    const status = $("#proxyStatus");
    status.textContent = "正在测试代理连接...";
    setTimeout(() => {
      status.textContent = "代理连接正常（预览）；只影响 GitHub 云端功能。";
      showToast("代理连接正常（预览）。");
    }, 650);
  });

  /* Restore modal */
  function openRestoreModal(id) {
    closeAllOverlays();
    renderRestoreOptions();
    if (id) $("#restoreSnapshot").value = id;
    $("#restoreModal").classList.add("open");
  }

  $("#confirmRestoreButton").addEventListener("click", () => {
    closeOverlay("restoreModal");
    showToast("恢复完成（预览）。正式版会先创建恢复前快照。");
  });

  document.addEventListener("click", (event) => {
    const closer = event.target.closest("[data-close]");
    if (!closer) return;
    const id = closer.dataset.close;
    if (id === "githubModal") {
      cancelGithub();
    } else {
      closeOverlay(id);
    }
  });

  $$(".overlay.modal").forEach((overlay) => {
    overlay.addEventListener("click", (event) => {
      if (event.target !== overlay) return;
      if (overlay.id === "githubModal") cancelGithub();
      else overlay.classList.remove("open");
    });
  });

  document.addEventListener("click", (event) => {
    const info = event.target.closest(".info-icon");
    if (!info) return;
    event.preventDefault();
    event.stopPropagation();
    info.focus();
  });

  document.addEventListener("click", (event) => {
    const toastButton = event.target.closest("[data-toast]");
    if (toastButton) showToast(toastButton.dataset.toast);
  });

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    if ($("#githubModal").classList.contains("open")) {
      cancelGithub();
      return;
    }
    const openModal = $(".overlay.modal.open");
    if (openModal) openModal.classList.remove("open");
  });

  /* GitHub modal */
  function openGithubModal() {
    $("#githubModal").classList.add("open");
  }

  function cancelGithub() {
    closeOverlay("githubModal");
    if (state.pendingMode) {
      state.pendingMode = null;
      updateModeUI();
    }
    state.pendingAfterOnboarding = false;
    state.githubContext = null;
  }

  $("#connectGithubButton").addEventListener("click", () => {
    state.pendingMode = state.mode === "local" ? "hybrid" : state.mode;
    state.githubContext = "cloudTab";
    openGithubModal();
  });

  $("#disconnectButton").addEventListener("click", () => {
    state.cloudConnected = false;
    state.mode = "local";
    updateModeUI();
    showToast("已断开 GitHub 连接。");
  });

  $("#copyCodeButton").addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText("7GQ4-2K9M");
      showToast("授权码已复制。");
    } catch {
      showToast("授权码：7GQ4-2K9M");
    }
  });

  $("#openGithubButton").addEventListener("click", () => {
    window.open("https://github.com/login/device", "_blank", "noopener");
  });

  $("#githubProxyButton").addEventListener("click", () => {
    cancelGithub();
    openSettingsTab("network");
    showToast("配置代理后，再从“云端”重新连接 GitHub。");
  });

  $("#simulateGithubButton").addEventListener("click", () => {
    const afterOnboarding = state.pendingAfterOnboarding;
    const targetMode = state.pendingMode;
    const context = state.githubContext;

    state.cloudConnected = true;
    state.mode = targetMode || (state.mode === "local" ? "hybrid" : state.mode);
    state.pendingMode = null;
    state.pendingAfterOnboarding = false;
    state.githubContext = null;
    closeOverlay("githubModal");
    updateModeUI();
    showToast("GitHub 已连接（预览）。");

    if (afterOnboarding) {
      finishOnboarding();
    } else if (context === "wizard") {
      renderWizard();
    } else {
      openSettingsTab("cloud");
    }
  });

  /* Onboarding */
  function openOnboarding() {
    state.onboardingStep = 1;
    state.wizardMode = state.mode;
    state.wizardDir = state.localDir;
    renderWizard();
    $("#onboarding").classList.add("open");
  }

  function wizardModeOptions() {
    return [
      {
        value: "local",
        title: "仅本地备份",
        desc: "完全离线，不需要 GitHub；之后可以随时添加云端。",
        badge: "推荐",
        badgeClass: ""
      },
      {
        value: "hybrid",
        title: "本地 + GitHub",
        desc: "本地保留一份，再加密上传到你的私有仓库。",
        badge: "双副本",
        badgeClass: ""
      },
      {
        value: "cloud",
        title: "仅 GitHub",
        desc: "没有本地副本，断网或账号异常时无法恢复。",
        badge: "不推荐",
        badgeClass: "muted"
      }
    ];
  }

  function wizardBodyHtml() {
    if (state.onboardingStep === 1) {
      return `
        <h2>欢迎使用 DSH Backup</h2>
        <p class="wizard-lead">只备份 DSH 的核心数据，不读取项目代码。先选择备份方式，后续可以在设置中修改。</p>
        <div class="choice-list">
          ${wizardModeOptions().map((option) => `
            <label class="choice-card ${state.wizardMode === option.value ? "selected" : ""}" data-wizard-mode="${option.value}">
              <input type="radio" name="wizardMode" value="${option.value}" ${state.wizardMode === option.value ? "checked" : ""}>
              <div class="choice-title">${option.title}</div>
              <span class="choice-badge ${option.badgeClass}">${option.badge}</span>
              <div class="choice-desc">${option.desc}</div>
            </label>
          `).join("")}
        </div>
      `;
    }

    if (state.onboardingStep === 2) {
      const cloudRow = state.wizardMode === "local" ? "" : `
        <div class="wizard-connect-row">
          <span>${state.cloudConnected ? "GitHub 已连接，完成设置后可以上传。" : "云端模式需要先连接 GitHub。"}</span>
          ${state.cloudConnected
            ? '<span class="badge success">已连接</span>'
            : '<button class="btn btn-secondary btn-small" id="wizardConnectButton">连接 GitHub</button>'}
        </div>
      `;
      return `
        <h2>设置本地备份</h2>
        <p class="wizard-lead">这些选项稍后都可以在设置中修改。默认不加密，适合只在本机使用的备份。</p>
        <div class="wizard-fields">
          <div class="wizard-field">
            <label for="wizardDirInput">本地备份目录</label>
            <div class="path-input">
              <input class="text-input mono" id="wizardDirInput" value="${state.wizardDir}">
              <button class="btn btn-secondary btn-small" data-toast="目录选择器将在正式版中打开（预览）。">浏览</button>
            </div>
            <p class="hint">便携版默认在应用目录下；删除应用目录前请先移走备份。</p>
          </div>
          <div class="wizard-field">
            <span class="wizard-label">压缩等级</span>
            <div class="segmented" id="wizardCompression">
              <button data-compression="fast" class="${state.compression === "fast" ? "active" : ""}">快速</button>
              <button data-compression="balanced" class="${state.compression === "balanced" ? "active" : ""}">均衡</button>
              <button data-compression="max" class="${state.compression === "max" ? "active" : ""}">极限</button>
            </div>
            <p class="hint" id="wizardCompressionHint">${compressionHints[state.compression]}</p>
          </div>
          <div class="wizard-field">
            <label class="switch-row">
              <span class="switch-copy">加密本地备份 <span class="info-icon" tabindex="0" data-tip="开启后在应用目录生成密钥对；归档密码由公钥封装，恢复时用私钥解开。私钥丢失后无法恢复。">i</span></span>
              <span class="switch">
                <input type="checkbox" id="wizardEncryptionToggle" ${state.localEncryption ? "checked" : ""}>
                <span class="switch-track"></span>
              </span>
            </label>
            <p class="hint">默认关闭；如果备份会放 U 盘或网盘，建议开启。</p>
          </div>
          ${cloudRow}
        </div>
      `;
    }

    const encryptionText = state.localEncryption ? "开启" : "关闭";
    const cloudText = state.wizardMode === "local"
      ? "不连接"
      : state.cloudConnected
        ? "GitHub 已连接"
        : "完成时引导连接";
    return `
      <h2>准备就绪</h2>
      <p class="wizard-lead">确认下面的设置，进入主界面后就可以创建第一份备份。</p>
      <div class="wizard-summary">
        <div class="summary-row"><span>备份方式</span><strong>${modeLabel(state.wizardMode)}</strong></div>
        <div class="summary-row"><span>本地目录</span><strong class="mono">${state.wizardDir}</strong></div>
        <div class="summary-row"><span>压缩等级</span><strong>${compressionLabels[state.compression]}</strong></div>
        <div class="summary-row"><span>本地加密</span><strong>${encryptionText}</strong></div>
        <div class="summary-row"><span>云端</span><strong>${cloudText}</strong></div>
      </div>
      <div class="summary-note">
        <span class="dot"></span>
        <span>${state.wizardMode === "local"
          ? "仅本地模式不会发起网络请求，也不会要求 GitHub 登录。"
          : "云端归档始终加密；本地是否加密按你的选择执行。"}</span>
      </div>
    `;
  }

  function bindWizardFields() {
    const dirInput = $("#wizardDirInput");
    if (dirInput) {
      dirInput.addEventListener("input", () => {
        state.wizardDir = dirInput.value;
      });
    }

    const compression = $("#wizardCompression");
    if (compression) {
      compression.addEventListener("click", (event) => {
        const button = event.target.closest("[data-compression]");
        if (!button) return;
        state.compression = button.dataset.compression;
        $$("[data-compression]", compression).forEach((item) => {
          item.classList.toggle("active", item === button);
        });
        const hint = $("#wizardCompressionHint");
        if (hint) hint.textContent = compressionHints[state.compression];
      });
    }

    const encryptionToggle = $("#wizardEncryptionToggle");
    if (encryptionToggle) {
      encryptionToggle.addEventListener("change", () => {
        state.localEncryption = encryptionToggle.checked;
        setLocalEncryption(encryptionToggle.checked);
      });
    }

    const connect = $("#wizardConnectButton");
    if (connect) {
      connect.addEventListener("click", () => {
        state.pendingMode = state.wizardMode;
        state.githubContext = "wizard";
        openGithubModal();
      });
    }
  }

  function renderWizard() {
    $("#wizardStepCount").textContent = `${state.onboardingStep} / 3`;
    $("#wizardProgressBar").style.width = `${(state.onboardingStep / 3) * 100}%`;
    $("#wizardBody").innerHTML = wizardBodyHtml();
    $("#wizardBack").style.visibility = state.onboardingStep === 1 ? "hidden" : "visible";
    $("#wizardNext").textContent = state.onboardingStep === 3 ? "完成设置" : "下一步";
    $("#wizardSkip").textContent = state.onboardingStep === 1 ? "稍后设置" : "使用默认值";
    bindWizardFields();
  }

  $("#wizardBody").addEventListener("change", (event) => {
    const input = event.target.closest('input[name="wizardMode"]');
    if (!input) return;
    state.wizardMode = input.value;
    $$("[data-wizard-mode]").forEach((card) => {
      card.classList.toggle("selected", card.dataset.wizardMode === state.wizardMode);
    });
  });

  $("#wizardBack").addEventListener("click", () => {
    if (state.onboardingStep <= 1) return;
    state.onboardingStep -= 1;
    renderWizard();
  });

  $("#wizardSkip").addEventListener("click", () => {
    state.wizardMode = "local";
    state.localEncryption = false;
    finishOnboarding();
  });

  $("#wizardNext").addEventListener("click", () => {
    if (state.onboardingStep < 3) {
      state.onboardingStep += 1;
      renderWizard();
      return;
    }

    if (state.wizardMode !== "local" && !state.cloudConnected) {
      state.pendingMode = state.wizardMode;
      state.pendingAfterOnboarding = true;
      state.githubContext = "wizard";
      openGithubModal();
      return;
    }

    finishOnboarding();
  });

  function finishOnboarding() {
    state.mode = state.wizardMode === "local"
      ? "local"
      : state.cloudConnected
        ? state.wizardMode
        : "local";
    state.localDir = state.wizardDir || state.localDir;
    $("#localDirInput").value = state.localDir;
    setCompression(state.compression);
    setLocalEncryption(state.localEncryption);
    closeOverlay("onboarding");
    updateModeUI();
    showPage("dashboard");
    showToast(`已启用：${modeLabel(state.mode)}`);
  }

  /* Backup simulation */
  function stepsForBackup() {
    const cloud = state.cloudConnected && state.mode !== "local";
    const encrypt = state.localEncryption || cloud;
    if (cloud) {
      return [
        { label: "检查文件", at: 0 },
        { label: "7z 压缩", at: 15 },
        { label: "加密", at: 58 },
        { label: "上传云端", at: 74 },
        { label: "校验", at: 92 }
      ];
    }
    if (encrypt) {
      return [
        { label: "检查文件", at: 0 },
        { label: "7z 压缩", at: 15 },
        { label: "加密", at: 58 },
        { label: "写入本地", at: 78 },
        { label: "校验", at: 92 }
      ];
    }
    return [
      { label: "检查文件", at: 0 },
      { label: "7z 压缩", at: 15 },
      { label: "写入本地", at: 72 },
      { label: "校验", at: 92 }
    ];
  }

  function progressPhase(progress) {
    const cloud = state.cloudConnected && state.mode !== "local";
    const encrypt = state.localEncryption || cloud;
    if (progress < 15) return "检查文件";
    if (progress < 58) return "7z 压缩";
    if (encrypt && progress < 78) return "加密归档";
    if (progress < 92) return cloud ? "上传云端" : "写入本地";
    return "校验备份";
  }

  function renderProgressSteps(progress) {
    const steps = stepsForBackup();
    let current = 0;
    steps.forEach((step, index) => {
      if (progress >= step.at) current = index;
    });
    const html = steps.map((step, index) => {
      const done = progress >= (steps[index + 1]?.at ?? 101);
      const isCurrent = index === current && !done;
      const cls = done ? "done" : isCurrent ? "current" : "";
      const mark = done ? "✓" : isCurrent ? "·" : "";
      return `<span class="progress-step ${cls}"><span class="step-dot">${mark}</span>${step.label}</span>`;
    }).join("");
    $("#progressSteps").innerHTML = html;
  }

  function updateProgress(progress) {
    const p = Math.min(100, Math.round(progress));
    $("#progressPercent").textContent = `${p}%`;
    $("#progressBar").style.width = `${p}%`;
    $("#progressPhase").textContent = progressPhase(p);
    $("#progressBytes").textContent = `${(100.5 * p / 100).toFixed(1)} MB / 100.5 MB`;
    const remain = Math.max(1, Math.ceil((100 - p) / 4));
    $("#progressEta").textContent = p >= 100 ? "完成" : `预计剩余 ${remain} 秒`;
    renderProgressSteps(p);
  }

  function formatNow() {
    const now = new Date();
    const pad = (value) => String(value).padStart(2, "0");
    return {
      full: `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}`,
      file: `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}`
    };
  }

  function finishBackup() {
    const cloud = state.cloudConnected && state.mode !== "local";
    const encrypted = state.localEncryption || cloud;
    const now = formatNow();
    const source = cloud ? (state.mode === "cloud" ? "cloud" : "both") : "local";
    const size = "64.4 MB";
    const path = `${state.localDir}\\dsh-backup-${now.file}.7z${encrypted ? ".age" : ""}`;

    $("#lastBackupTitle").textContent = now.full;
    $("#lastBackupSize").textContent = size;
    $("#lastBackupCompression").textContent = compressionLabels[state.compression];
    $("#lastBackupEncryption").textContent = encrypted ? "已加密" : "未加密";
    $("#lastBackupEncryption").className = encrypted ? "badge success" : "badge muted";
    $("#lastBackupPath").textContent = path;

    state.history.unshift({
      id: `snapshot-${Date.now()}`,
      date: now.full,
      source,
      size,
      encrypted,
      status: "success"
    });
    renderRecent();
    if (state.page === "history") renderHistory();
    renderRestoreOptions();
    showToast(cloud ? "备份完成，已保存本地并上传云端（预览）。" : "备份完成并已校验（预览）。");
  }

  function startBackup() {
    closeAllOverlays();
    showPage("dashboard");
    $("#lastBackupCard").hidden = true;
    $("#emptyState").hidden = true;
    $("#progressCard").hidden = false;
    state.backupProgress = 0;
    updateProgress(0);
    clearInterval(state.backupTimer);
    state.backupTimer = setInterval(() => {
      let next = state.backupProgress;
      if (next < 18) next += 3.5;
      else if (next < 58) next += 5.5;
      else if (next < 88) next += 4;
      else next += 2.2;
      state.backupProgress = Math.min(100, next);
      updateProgress(state.backupProgress);
      if (state.backupProgress >= 100) {
        clearInterval(state.backupTimer);
        state.backupTimer = null;
        setTimeout(() => {
          $("#progressCard").hidden = true;
          $("#lastBackupCard").hidden = false;
          finishBackup();
        }, 260);
      }
    }, 120);
  }

  $("#cancelBackupButton").addEventListener("click", () => {
    clearInterval(state.backupTimer);
    state.backupTimer = null;
    $("#progressCard").hidden = true;
    $("#lastBackupCard").hidden = false;
    showToast("已取消备份（预览）。");
  });

  /* Preview navigation */
  const previewActions = {
    onboarding: () => {
      closeAllOverlays();
      openOnboarding();
    },
    dashboard: () => {
      closeAllOverlays();
      showPage("dashboard");
    },
    progress: () => {
      startBackup();
    },
    settings: () => {
      closeAllOverlays();
      openSettingsTab("backup");
    },
    network: () => {
      closeAllOverlays();
      openSettingsTab("network");
    },
    security: () => {
      closeAllOverlays();
      openSettingsTab("security");
    },
    restore: () => {
      closeAllOverlays();
      showPage("dashboard");
      openRestoreModal(state.history[0]?.id);
    },
    github: () => {
      closeAllOverlays();
      showPage("dashboard");
      state.pendingMode = state.mode === "local" ? "hybrid" : state.mode;
      state.githubContext = "preview";
      openGithubModal();
    }
  };

  $$(".preview-bar button[data-preview]").forEach((button) => {
    button.addEventListener("click", () => {
      $$(".preview-bar button").forEach((item) => item.classList.toggle("active", item === button));
      previewActions[button.dataset.preview]?.();
    });
  });

  /* Init */
  renderRecent();
  renderHistory();
  renderRestoreOptions();
  updateModeUI();
  setCompression("balanced");
  setLocalEncryption(false);
  const initialPreview = new URLSearchParams(window.location.search).get("preview");
  if (initialPreview && previewActions[initialPreview]) {
    previewActions[initialPreview]();
  } else {
    openOnboarding();
  }
})();

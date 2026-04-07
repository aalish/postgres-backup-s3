(function () {
  const body = document.body;
  const page = body?.dataset?.page || "";
  const csrfToken = document.querySelector('meta[name="csrf-token"]')?.getAttribute("content") || "";

  function $(selector) {
    return document.querySelector(selector);
  }

  function createStatusBadge(status) {
    const label = String(status || "unknown").replaceAll("_", " ");
    return `<span class="status-badge status-${escapeHTML(status)}">${escapeHTML(label)}</span>`;
  }

  function escapeHTML(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;")
      .replaceAll("'", "&#39;");
  }

  function formatBytes(bytes) {
    const value = Number(bytes || 0);
    if (!Number.isFinite(value) || value <= 0) {
      return "0 B";
    }
    const units = ["B", "KB", "MB", "GB", "TB"];
    let size = value;
    let unit = 0;
    while (size >= 1024 && unit < units.length - 1) {
      size /= 1024;
      unit += 1;
    }
    return `${size.toFixed(size >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
  }

  function formatTime(value) {
    if (!value) {
      return "—";
    }
    const date = new Date(value);
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(date);
  }

  function showMessage(element, type, text) {
    if (!element) return;
    element.className = `inline-message ${type}`;
    element.textContent = text;
    element.classList.remove("hidden");
  }

  function clearMessage(element) {
    if (!element) return;
    element.classList.add("hidden");
    element.textContent = "";
  }

  async function api(path, options = {}) {
    const headers = new Headers(options.headers || {});
    if (options.body && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    if (options.method && options.method !== "GET" && csrfToken) {
      headers.set("X-CSRF-Token", csrfToken);
    }

    const response = await fetch(path, {
      credentials: "same-origin",
      ...options,
      headers,
    });

    let payload = null;
    const contentType = response.headers.get("content-type") || "";
    if (contentType.includes("application/json")) {
      payload = await response.json();
    }

    if (!response.ok) {
      const message = payload?.error || payload?.message || "Request failed.";
      throw new Error(message);
    }

    return payload;
  }

  function renderBackupsRows(target, items) {
    if (!target) return;
    if (!items.length) {
      target.innerHTML = `<tr><td colspan="5" class="muted">No backups matched the current filters.</td></tr>`;
      return;
    }
    target.innerHTML = items.map((item) => `
      <tr>
        <td>${escapeHTML(item.name)}</td>
        <td>${escapeHTML(formatBytes(item.size))}</td>
        <td>${escapeHTML(formatTime(item.createdAt))}</td>
        <td class="mono">${escapeHTML(item.s3Path)}</td>
        <td>${createStatusBadge(item.status)}</td>
      </tr>
    `).join("");
  }

  function renderBackupRuns(target, items) {
    if (!target) return;
    if (!items.length) {
      target.innerHTML = `<tr><td colspan="5" class="muted">No backup runs have been recorded yet.</td></tr>`;
      return;
    }
    target.innerHTML = items.map((item) => `
      <tr>
        <td>${escapeHTML(formatTime(item.startedAt))}</td>
        <td>${escapeHTML(item.triggeredBy)}</td>
        <td>${escapeHTML(item.triggerSource)}</td>
        <td>${createStatusBadge(item.status)}</td>
        <td class="mono">${escapeHTML(item.s3URI || "—")}</td>
      </tr>
    `).join("");
  }

  function renderRetentionRuns(target, items) {
    if (!target) return;
    if (!items.length) {
      target.innerHTML = `<tr><td colspan="5" class="muted">No retention runs have been recorded yet.</td></tr>`;
      return;
    }
    target.innerHTML = items.map((item) => `
      <tr>
        <td>${escapeHTML(formatTime(item.startedAt))}</td>
        <td>${escapeHTML(item.trigger)}</td>
        <td>${createStatusBadge(item.status)}</td>
        <td>${escapeHTML(String(item.evaluated ?? 0))}</td>
        <td>${escapeHTML(String(item.deleted ?? 0))}</td>
      </tr>
    `).join("");
  }

  function renderAuditEvents(target, items) {
    if (!target) return;
    if (!items.length) {
      target.innerHTML = `<tr><td colspan="4" class="muted">No audit events have been recorded yet.</td></tr>`;
      return;
    }
    target.innerHTML = items.map((item) => `
      <tr>
        <td>${escapeHTML(formatTime(item.createdAt))}</td>
        <td>${escapeHTML(item.type)}</td>
        <td>${escapeHTML(item.actor || "system")}</td>
        <td>${escapeHTML(item.message)}</td>
      </tr>
    `).join("");
  }

  function renderPagination(target, page, totalPages, onChange) {
    if (!target) return;
    if (totalPages <= 1) {
      target.innerHTML = `<span class="muted">Page ${page} of ${Math.max(totalPages, 1)}</span>`;
      return;
    }
    target.innerHTML = `
      <span class="muted">Page ${page} of ${totalPages}</span>
      <div class="pagination">
        <button type="button" class="button button-secondary" ${page <= 1 ? "disabled" : ""} data-page="${page - 1}">Previous</button>
        <button type="button" class="button button-secondary" ${page >= totalPages ? "disabled" : ""} data-page="${page + 1}">Next</button>
      </div>
    `;
    target.querySelectorAll("[data-page]").forEach((button) => {
      button.addEventListener("click", () => onChange(Number(button.dataset.page)));
    });
  }

  async function triggerBackup(messageEl, refresh) {
    showMessage(messageEl, "info", "Starting backup…");
    try {
      await api("/api/backups/trigger", { method: "POST" });
      showMessage(messageEl, "success", "Backup started. Status will update automatically.");
      await refresh();
    } catch (error) {
      showMessage(messageEl, "error", error.message);
    }
  }

  async function loadDashboard() {
    const stats = $("#dashboard-stats");
    const currentJobPanel = $("#current-job-panel");
    const backupsTable = $("#dashboard-backups");
    const messageEl = $("#backup-action-message");
    const triggerButton = document.querySelector('[data-action="trigger-backup"]');

    const refresh = async () => {
      const overview = await api("/api/overview");
      stats.innerHTML = `
        <div class="stat-card">
          <h3>Total Backups</h3>
          <strong>${escapeHTML(String(overview.backupCount || 0))}</strong>
          <p>Objects currently found in S3 under the active prefix.</p>
        </div>
        <div class="stat-card">
          <h3>Latest Backup</h3>
          <strong>${escapeHTML(overview.latestRun?.status || "none")}</strong>
          <p>${escapeHTML(overview.latestRun?.finishedAt ? formatTime(overview.latestRun.finishedAt) : "No completed run yet.")}</p>
        </div>
        <div class="stat-card">
          <h3>Last Retention Run</h3>
          <strong>${escapeHTML(overview.latestRetention?.status || "none")}</strong>
          <p>${escapeHTML(overview.latestRetention?.startedAt ? formatTime(overview.latestRetention.startedAt) : "Retention has not run yet.")}</p>
        </div>
      `;

      if (overview.currentJob) {
        currentJobPanel.innerHTML = `
          <div>
            <strong>Run ${escapeHTML(overview.currentJob.id)}</strong>
            <p class="muted">Triggered by ${escapeHTML(overview.currentJob.triggeredBy)} via ${escapeHTML(overview.currentJob.triggerSource)}.</p>
            <div>${createStatusBadge(overview.currentJob.status)}</div>
          </div>
        `;
      } else if (overview.latestRun) {
        currentJobPanel.innerHTML = `
          <div>
            <strong>Latest run finished ${escapeHTML(formatTime(overview.latestRun.finishedAt || overview.latestRun.startedAt))}</strong>
            <p class="muted">${escapeHTML(overview.latestRun.s3URI || overview.latestRun.error || "No current backup is running.")}</p>
            <div>${createStatusBadge(overview.latestRun.status)}</div>
          </div>
        `;
      } else {
        currentJobPanel.innerHTML = `No backup job is currently running.`;
      }

      renderBackupsRows(backupsTable, overview.recentBackups || []);
      return overview;
    };

    if (triggerButton) {
      triggerButton.addEventListener("click", () => triggerBackup(messageEl, refresh));
    }

    let overview = await refresh();
    if (overview.currentJob) {
      const timer = setInterval(async () => {
        overview = await refresh();
        if (!overview.currentJob) {
          clearInterval(timer);
        }
      }, 4000);
    }
  }

  async function loadBackupsPage() {
    const backupsTable = $("#backups-table");
    const runsTable = $("#backup-runs-table");
    const pagination = $("#backups-pagination");
    const form = $("#backup-filters");
    const messageEl = $("#backup-action-message");
    const triggerButton = document.querySelector('[data-action="trigger-backup"]');
    let currentPage = 1;

    const loadRows = async () => {
      const formData = new FormData(form);
      const params = new URLSearchParams();
      params.set("page", String(currentPage));
      params.set("pageSize", "20");
      for (const [key, value] of formData.entries()) {
        if (value) params.set(key, value);
      }
      const backups = await api(`/api/backups?${params.toString()}`);
      renderBackupsRows(backupsTable, backups.items || []);
      renderPagination(pagination, backups.page, backups.totalPages, (pageNumber) => {
        currentPage = pageNumber;
        loadRows().catch(showPageError);
      });
    };

    const loadRuns = async () => {
      const runs = await api("/api/backup-runs?page=1&pageSize=10");
      renderBackupRuns(runsTable, runs.items || []);
    };

    const refreshAll = async () => {
      await Promise.all([loadRows(), loadRuns()]);
    };

    function showPageError(error) {
      showMessage(messageEl, "error", error.message);
    }

    form?.addEventListener("submit", (event) => {
      event.preventDefault();
      currentPage = 1;
      clearMessage(messageEl);
      refreshAll().catch(showPageError);
    });

    if (triggerButton) {
      triggerButton.addEventListener("click", () => triggerBackup(messageEl, refreshAll));
    }

    await refreshAll();
  }

  async function loadSettingsPage() {
    const form = $("#settings-form");
    const messageEl = $("#settings-message");
    if (!form) return;

    const settings = await api("/api/settings");
    form.retentionDays.value = settings.retentionDays;
    form.backupSchedule.value = settings.backupSchedule;
    form.s3Bucket.value = settings.s3Bucket;
    form.s3Prefix.value = settings.s3Prefix || "";
    form.backupOutputDir.value = settings.backupOutputDir;
    form.backupFilenamePrefix.value = settings.backupFilenamePrefix;
    form.backupCompression.value = settings.backupCompression;
    form.notificationEnabled.checked = Boolean(settings.notification?.enabled);
    form.webhookURL.value = settings.notification?.webhookURL || "";
    form.webhookTimeout.value = settings.notification?.webhookTimeout || "";

    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      showMessage(messageEl, "info", "Saving changes…");
      const payload = {
        retentionDays: Number(form.retentionDays.value),
        backupSchedule: form.backupSchedule.value,
        s3Bucket: form.s3Bucket.value,
        s3Prefix: form.s3Prefix.value,
        backupOutputDir: form.backupOutputDir.value,
        backupFilenamePrefix: form.backupFilenamePrefix.value,
        backupCompression: Number(form.backupCompression.value),
        notification: {
          enabled: Boolean(form.notificationEnabled.checked),
          webhookURL: form.webhookURL.value,
          webhookTimeout: form.webhookTimeout.value,
        },
      };

      try {
        await api("/api/settings", {
          method: "POST",
          body: JSON.stringify(payload),
        });
        showMessage(messageEl, "success", "Settings saved successfully.");
      } catch (error) {
        showMessage(messageEl, "error", error.message);
      }
    });
  }

  async function loadLogsPage() {
    const retentionTable = $("#retention-runs-table");
    const auditTable = $("#audit-events-table");

    const [retention, audit] = await Promise.all([
      api("/api/retention-runs?page=1&pageSize=10"),
      api("/api/audit-events?page=1&pageSize=20"),
    ]);

    renderRetentionRuns(retentionTable, retention.items || []);
    renderAuditEvents(auditTable, audit.items || []);
  }

  async function init() {
    try {
      if (page === "dashboard") {
        await loadDashboard();
      } else if (page === "backups") {
        await loadBackupsPage();
      } else if (page === "settings") {
        await loadSettingsPage();
      } else if (page === "logs") {
        await loadLogsPage();
      }
    } catch (error) {
      const messageEl = $("#backup-action-message") || $("#settings-message");
      showMessage(messageEl, "error", error.message);
    }
  }

  init();
})();

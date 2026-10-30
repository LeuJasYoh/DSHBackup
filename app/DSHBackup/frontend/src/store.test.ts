import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Bootstrap, Snapshot } from "./lib/types";

const mocks = vi.hoisted(() => ({
  moveToTrash: vi.fn(),
  getBootstrap: vi.fn(),
  listTrash: vi.fn(),
}));

vi.mock("./lib/bridge", () => ({
  api: {
    moveToTrash: mocks.moveToTrash,
    getBootstrap: mocks.getBootstrap,
    listTrash: mocks.listTrash,
  },
  isPreviewMode: false,
  copyToClipboard: vi.fn(async () => true),
}));

import { deleteSnapshot, snapshots, store, trashBytes, trashCount } from "./store";

function makeSnapshot(id: string, size = 1024): Snapshot {
  return {
    id,
    createdAt: "2026-10-30T10:00:00Z",
    kind: "manual",
    size,
    sha256: "a".repeat(64),
    encrypted: false,
    desktopIncluded: false,
    fileCount: 10,
    totalBytes: 2048,
    status: "ready",
    localPath: `D:\\Backups\\dsh-backup-${id}.7z`,
    manifestPath: `D:\\Backups\\dsh-backup-${id}.manifest.json`,
  };
}

function makeBoot(): Bootstrap {
  return {
    config: {
      schemaVersion: 1, theme: "system", updateCheck: false, onboardingComplete: true,
      backupDir: "D:\\Backups", includeDesktop: false, encryptionLocal: false,
      compression: { level: "store", dictionaryMb: 0, threads: 0, extraArgs: [] },
      schedule: { kind: "manual", time: "03:00", weekday: 1 },
      retention: { local: 7, trashDays: 7 },
      preRestoreSnapshot: true, verifyAfterBackup: true, closeBehavior: "ask", lastScheduledAt: "",
    },
    app: {
      version: "test", buildTime: "", platform: "windows/amd64", writable: true, portable: true,
      trayAvailable: true, warnings: [],
      sevenZip: { path: "", version: "", ok: true }, sidecar: { path: "", version: "", ok: true },
    },
    paths: { appDir: "", dataDir: "", configDir: "", logsDir: "", keysDir: "", backupDir: "D:\\Backups", defaultBackupDir: "D:\\Backups" },
    dsh: { running: false, evidence: [], sources: [], checkedAt: "" },
    localSnapshots: [makeSnapshot("20261030-100000", 1024), makeSnapshot("20261030-090000", 2048)],
    key: { exists: false },
    disk: { path: "D:\\Backups", free: 0, total: 0 },
    schedule: { kind: "manual", time: "03:00", weekday: 1, nextRun: "", lastRun: "" },
    warnings: [],
    logsPath: "",
    backupRootIdentity: "volume:root",
    trash: { items: [], count: 0, bytes: 0, root: "D:\\Backups\\.dshbackup-trash" },
  };
}

beforeEach(() => {
  store.boot = makeBoot();
  store.toast = { text: "", type: "info", action: null };
  mocks.moveToTrash.mockReset();
  mocks.getBootstrap.mockReset();
  mocks.listTrash.mockReset();
});

describe("deleting a backup", () => {
  it("removes the row and shows it in the trash without refetching the bootstrap", async () => {
    mocks.moveToTrash.mockResolvedValueOnce(undefined);
    const target = snapshots.value[0];

    await deleteSnapshot(target);

    expect(mocks.moveToTrash).toHaveBeenCalledWith({
      snapshotId: "20261030-100000",
      expectedRootIdentity: "volume:root",
      expectedBackupDir: "D:\\Backups",
      expectedSha256: "a".repeat(64),
    });
    expect(snapshots.value.map((item) => item.id)).toEqual(["20261030-090000"]);
    expect(trashCount.value).toBe(1);
    expect(trashBytes.value).toBe(1024);
    expect(store.boot?.trash.items[0]).toMatchObject({ snapshotId: "20261030-100000", reason: "manual" });
    // This is the regression guard for "the whole page refreshes after a delete".
    expect(mocks.getBootstrap).not.toHaveBeenCalled();
    expect(store.toast.type).toBe("info");
  });

  it("keeps the archive in the list when the move fails", async () => {
    mocks.moveToTrash.mockRejectedValueOnce(new Error("文件正被占用"));

    await deleteSnapshot(snapshots.value[0]);

    expect(snapshots.value).toHaveLength(2);
    expect(trashCount.value).toBe(0);
    expect(mocks.getBootstrap).not.toHaveBeenCalled();
    expect(store.toast.type).toBe("error");
    expect(store.toast.text).toContain("文件正被占用");
  });

  it("refuses to delete when the backup root identity is unknown", async () => {
    store.boot!.backupRootIdentity = "";

    await deleteSnapshot(snapshots.value[0]);

    expect(mocks.moveToTrash).not.toHaveBeenCalled();
    expect(snapshots.value).toHaveLength(2);
    expect(store.toast.type).toBe("error");
  });
});

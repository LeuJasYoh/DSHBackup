import { describe, expect, it } from "vitest";
import { compressionLabel, compressionLevels, extraArgsError, validateConfig } from "./format";
import type { Config } from "./types";

function baseConfig(): Config {
  return {
    schemaVersion: 1,
    theme: "system",
    updateCheck: true,
    onboardingComplete: true,
    backupDir: "D:\\Backups",
    includeDesktop: true,
    encryptionLocal: false,
    compression: { level: "store", dictionaryMb: 0, threads: 0, extraArgs: [] },
    schedule: { kind: "manual", time: "03:00", weekday: 1 },
    retention: { local: 7, trashDays: 7 },
    preRestoreSnapshot: true,
    verifyAfterBackup: true,
    closeBehavior: "ask",
    lastScheduledAt: "",
  };
}

describe("compression presets", () => {
  it("offers exactly the six levels with store first", () => {
    expect(compressionLevels).toEqual(["store", "fast", "quick", "balanced", "high", "max"]);
  });

  it("labels store as a non-compressing level", () => {
    expect(compressionLabel("store")).toBe("仅存储");
    expect(compressionLabel("max")).toBe("极限压缩");
    expect(compressionLabel("unknown")).toBe("未记录");
  });
});

describe("advanced 7z arguments", () => {
  it("rejects -mx because the preset owns the level", () => {
    expect(extraArgsError("-mx=0")).not.toBe("");
    expect(extraArgsError("-mx=9")).not.toBe("");
  });

  it("still accepts the whitelisted tuning flags", () => {
    expect(extraArgsError("-md=64m -ms=on -mmt=4")).toBe("");
  });
});

describe("config validation", () => {
  it("accepts the default trash retention", () => {
    expect(validateConfig(baseConfig(), "")).toBe("");
  });

  it("rejects out-of-range trash retention", () => {
    const low = baseConfig();
    low.retention.trashDays = 0;
    expect(validateConfig(low, "")).toContain("回收站保留天数");
    const high = baseConfig();
    high.retention.trashDays = 366;
    expect(validateConfig(high, "")).toContain("回收站保留天数");
  });

  it("rejects an unknown close behaviour", () => {
    const config = baseConfig();
    (config as unknown as { closeBehavior: string }).closeBehavior = "minimize";
    expect(validateConfig(config, "")).toContain("关闭行为");
  });
});
